//go:build linux

package assistance

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Runs inside the combined image with AI_RUNTIME_SMOKE=true. No model endpoint
// or clinical text is used; it verifies the real shipped binary and supervisor.
func TestManagedSepiLensRuntime(t *testing.T) {
	if os.Getenv("AI_RUNTIME_SMOKE") != "true" {
		t.Skip("container runtime smoke check")
	}
	testCipher(t)
	t.Setenv("AI_ASSISTANCE_TOKEN", "runtime-test-token")
	t.Setenv("AI_ASSISTANCE_TOKEN_FILE", "")
	t.Setenv("AI_ASSISTANCE_MANAGED_PROCESS", "true")
	t.Setenv("AI_ASSISTANCE_BINARY", "/app/sepilens/sepilens")
	dir := t.TempDir()
	t.Setenv("AI_ASSISTANCE_DATA_DIR", dir)
	client := Client{"http://127.0.0.1:18081", "runtime-test-token", &http.Client{Timeout: time.Second}}
	ready := func() {
		t.Helper()
		for i := 0; i < 60; i++ {
			if _, e := client.Call(context.Background(), "GET", "/readyz", "", nil, nil); e == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("SepiLens never became ready")
	}
	ctx, cancel := context.WithCancel(context.Background())
	wait := StartProcess(ctx)
	defer func() { cancel(); wait() }()
	ready()
	release, e := lockDirectory(filepath.Join(dir, ".octopus.lock"))
	if e == nil {
		release()
		t.Fatal("second writer acquired directory")
	}
	var caps map[string]any
	if _, e = client.Call(ctx, "GET", "/v1/capabilities", "", nil, &caps); e != nil {
		t.Fatal(e)
	}
	if caps["hpo_candidate_extraction_available"] != true {
		t.Fatal("shipped HPO index unavailable")
	}
	for _, entry := range caps["engines"].([]any) {
		engine := entry.(map[string]any)
		if engine["enabled"] == true {
			t.Fatal("medical engine gate bypassed")
		}
	}
	if caps["version"] != "0.2.0-rc2" {
		t.Fatal("unexpected release API version")
	}
	// Synthetic experimental scoring never contacts a model provider.
	method := json.RawMessage(`{"upstream_commit":"ef66faff51a265fef7b5c4e6439905f3aa540c46","moi":"AR","gene_disease_validity":"STRONG","population":{"faf":"0.000001","daft":"0.0001"},"variant_impact":{"workflow":"nonsense","prediction_outcome":"NMD_NO_RESCUE","initial_points":"6","gencc_mechanism":"ESTABLISHED","exon_relevance":"ALL","informative":[{"id":"iv-1","classification":"PATHOGENIC"}]}}`)
	path, body, err := BuildPayload(Input{Feature: "svcv4", Mode: "experimental", DraftAssessment: method, Reference: "GRCh38", Variant: map[string]any{"chromosome": "1", "position": float64(100), "ref": "A", "alt": "G", "gene": "EXAMPLE"}, ClinicalContext: Context{DiseaseID: "MONDO:EXAMPLE", Inheritance: "AR", SampleType: "blood", VariantOrigin: "germline_confirmed"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	delete(body["options"].(map[string]any), "provider_profile_id")
	var admission map[string]any
	status, err := client.Call(ctx, "POST", path, "runtime-svc-experiment", body, &admission)
	if err != nil || status != 202 {
		t.Fatal(status, err)
	}
	var final map[string]any
	for i := 0; i < 60; i++ {
		status, err = client.Call(ctx, "GET", "/v1/jobs/"+text(admission["job_id"])+"/result", "", nil, &final)
		if err != nil {
			t.Fatal(err)
		}
		if status == 200 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	result, bill, err := NormalizeResult("svcv4", "", final)
	if err != nil || bill || result.ExperimentalResult == nil || final["settlement_eligible"] != false {
		t.Fatal("experimental contract invalid", err)
	}
	if _, err = client.Call(ctx, "DELETE", "/v1/jobs/"+text(admission["job_id"]), "", nil, nil); err != nil {
		t.Fatal("upstream job deletion failed", err)
	}
	findChild := func() int {
		entries, _ := os.ReadDir("/proc")
		for _, entry := range entries {
			pid, e := strconv.Atoi(entry.Name())
			if e != nil {
				continue
			}
			raw, _ := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
			if strings.HasPrefix(string(raw), "/app/sepilens/sepilens\x00serve") {
				return pid
			}
		}
		return 0
	}
	first := findChild()
	if first == 0 {
		t.Fatal("child process missing")
	}
	if e = syscall.Kill(first, syscall.SIGTERM); e != nil {
		t.Fatal(e)
	}
	restarted := false
	for i := 0; i < 60; i++ {
		pid := findChild()
		if pid > 0 && pid != first {
			restarted = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !restarted {
		t.Fatal("supervisor did not restart child")
	}
	ready()
	cancel()
	wait()
	if _, e = os.Stat(filepath.Join(dir, "sepilens.db")); e != nil {
		t.Fatal("SQLite not persisted")
	}
	if _, e = client.Call(context.Background(), "GET", "/readyz", "", nil, nil); e == nil {
		t.Fatal("child still accepting requests after shutdown")
	}
	// Reuse the same persisted SQLite and master key after a clean shutdown.
	ctx2, cancel2 := context.WithCancel(context.Background())
	wait2 := StartProcess(ctx2)
	defer func() { cancel2(); wait2() }()
	ready()
}
