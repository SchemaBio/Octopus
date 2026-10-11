package assistance

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// StartProcess keeps AI failure independent from the analysis server. Only a
// small allowlist of environment values reaches the child; Octopus credentials
// and old page-agent model settings are never inherited.
func StartProcess(ctx context.Context) func() {
	if os.Getenv("AI_ASSISTANCE_MANAGED_PROCESS") != "true" {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		dir := os.Getenv("AI_ASSISTANCE_DATA_DIR")
		if dir == "" {
			dir = "/data/sepilens"
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			log.Print("SepiLens data directory unavailable")
			return
		}
		release, err := lockDirectory(filepath.Join(dir, ".octopus.lock"))
		if err != nil {
			log.Print("SepiLens directory already in use or unavailable")
			return
		}
		defer release()
		binary := os.Getenv("AI_ASSISTANCE_BINARY")
		if binary == "" {
			binary = "/app/sepilens/sepilens"
		}
		token := readSecret("AI_ASSISTANCE_TOKEN")
		if token == "" || readSecret("AI_ASSISTANCE_MASTER_KEY") == "" {
			log.Print("SepiLens runtime secrets unavailable")
			return
		}
		principals, _ := json.Marshal(map[string]any{token: map[string]string{"tenant": "octopus-service", "actor": "octopus", "role": "admin"}})
		env := []string{"PATH=" + os.Getenv("PATH"), "TZ=Asia/Shanghai", "SEPILENS_AUTH_MODE=required", "SEPILENS_PRINCIPALS=" + string(principals), "SEPILENS_KNOWLEDGE_DIR=/app/sepilens/knowledge/hpo"}
		if f := os.Getenv("AI_ASSISTANCE_MASTER_KEY_FILE"); f != "" {
			env = append(env, "SEPILENS_MASTER_KEY_FILE="+f)
		} else {
			env = append(env, "SEPILENS_MASTER_KEY="+readSecret("AI_ASSISTANCE_MASTER_KEY"))
		}
		if os.Getenv("AI_ASSISTANCE_ALLOW_PRIVATE_ENDPOINTS") == "true" {
			env = append(env, "SEPILENS_ALLOW_PRIVATE_ENDPOINTS=true")
		}
		for attempt := 0; attempt < 6 && ctx.Err() == nil; attempt++ {
			args := []string{"serve", "-addr", "127.0.0.1:18081", "-data-dir", dir}
			for name, flag := range map[string]string{"AI_ASSISTANCE_BUNDLE_DIR": "-bundle-dir", "AI_ASSISTANCE_RULES_DIR": "-rules-dir"} {
				if p := strings.TrimSpace(os.Getenv(name)); p != "" {
					args = append(args, flag, p)
				}
			}
			cmd := exec.Command(binary, args...)
			cmd.Env = env
			cmd.Dir = dir
			// Child log formats may evolve. Keep model/clinical payloads out of the
			// platform log; expose readiness and a generic exit notice instead.
			if err := cmd.Start(); err == nil {
				exited := make(chan error, 1)
				go func() { exited <- cmd.Wait() }()
				select {
				case <-ctx.Done():
					_ = cmd.Process.Signal(syscall.SIGTERM)
					select {
					case <-exited:
					case <-time.After(30 * time.Second):
						_ = cmd.Process.Kill()
						<-exited
					}
					return
				case <-exited:
					log.Print("SepiLens exited; AI temporarily unavailable")
				}
			} else {
				log.Print("SepiLens could not start; AI temporarily unavailable")
			}
			if attempt == 5 {
				return
			}
			timer := time.NewTimer(time.Second * time.Duration(1<<attempt))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return func() { <-done }
}
