package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/SchemaBio/Octopus/internal/config"
	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"strings"
	"testing"
	"time"
)

func TestParentIdentityRenameDeletionScopeAndAmbiguity(t *testing.T) {
	ss := &SampleService{cfg: &config.Config{Edition: "saas"}}
	child := model.Sample{UUID: "child", ExternalOrgID: "org-a"}
	rows := []model.Sample{{UUID: "dad", InternalID: "renamed", ExternalOrgID: "org-a", Gender: model.SampleGenderMale}, {UUID: "outsider", InternalID: "old", ExternalOrgID: "org-b"}}
	info, parent := resolveParent(&child, "old", "dad", "father", rows, ss.sameParentScope)
	if parent == nil || info.InternalID != "renamed" {
		t.Fatal("renamed parent identity lost")
	}
	rows[0].UUID = "replacement"
	rows[0].InternalID = "old"
	info, parent = resolveParent(&child, "old", "dad", "father", rows, ss.sameParentScope)
	if parent != nil || info.State != "missing" {
		t.Fatal("deleted parent rebound by name")
	}
	info, parent = resolveParent(&child, "old", "", "father", rows[1:], ss.sameParentScope)
	if parent != nil || info.State != "missing" {
		t.Fatal("cross-organization match")
	}
	rows = append(rows, rows[0])
	info, _ = resolveParent(&child, "old", "", "father", rows, ss.sameParentScope)
	if info.State != "ambiguous" {
		t.Fatal("ambiguous name accepted")
	}
	past := time.Now().Add(-time.Second)
	rows[0].ExpiresAt = &past
	info, _ = resolveParent(&child, "old", "replacement", "father", rows[:1], ss.sameParentScope)
	if info.State != "unavailable" {
		t.Fatal("expired parent accepted")
	}
}

func TestTrioSixAssetsOrderSnapshotAndValidation(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			db, mock := newUploadTransactionTestDB(t)
			previous := database.DB
			database.DB = db
			t.Cleanup(func() { database.DB = previous })
			svc := NewTaskService(&config.Config{Edition: "opensource"})
			var samples []model.Sample
			for i, role := range []string{"proband", "father", "mother"} {
				sample := model.Sample{UUID: role, InternalID: role, ExternalOrgID: "org", Gender: model.SampleGenderUnknown}
				sample.SetMatchedPair(&model.MatchedPair{R1Path: role + "-1", R2Path: role + "-2"})
				samples = append(samples, sample)
				if invalid && i > 0 {
					continue
				}
				for read := 1; read <= 2; read++ {
					if invalid && read > 1 {
						continue
					}
					kind := model.ReadTypeRead1
					if read == 2 {
						kind = model.ReadTypeRead2
					}
					if invalid {
						kind = model.ReadTypeRead2
					}
					mock.ExpectQuery(`SELECT \* FROM "data_assets"`).WithArgs(fmt.Sprintf("%s-%d", role, read), model.FileStatusCompleted, "org", 1).WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "external_org_id", "status", "read_type", "file_size"}).AddRow(i*2+read, fmt.Sprintf("%s-asset-%d", role, read), "org", model.FileStatusCompleted, kind, int64(10*1024*1024*1024)))
				}
			}
			inputs := map[string]interface{}{}
			_, assets, err := svc.prepareTrioMembers(samples, []string{"affected", "carrier", ""}, "task", model.OverlayActor{OrgID: "org"}, inputs)
			if invalid {
				if err == nil {
					t.Fatal("wrong read type accepted")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if len(assets) != 6 {
					t.Fatal("missing assets")
				}
				for i, a := range assets {
					if a.InputIndex != i/2 {
						t.Fatal("incorrect member order")
					}
				}
				snapshot := inputs["trio_members"].([]model.TrioMemberSnapshot)
				if snapshot[1].AffectedStatus != "carrier" || snapshot[2].AffectedStatus != "" {
					t.Fatal("parent statuses lost")
				}
				if trioEstimatedMinutes(inputs) != estimateTaskMinutesFromBytes(60*1024*1024*1024) {
					t.Fatal("estimate excludes parent reads")
				}
				samples[1].InternalID = "edited"
				if snapshot[1].InternalID != "father" {
					t.Fatal("snapshot changed with sample")
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestParentClearOmissionAndPendingCycle(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	ss := NewSampleService(&config.Config{Edition: "opensource"})
	self := "C"
	selfChild := model.Sample{UUID: "self", InternalID: "C"}
	if err := ss.applyParents(&selfChild, model.SampleParentUpdate{FatherSampleInternalID: &self}); err == nil {
		t.Fatal("self-parent name accepted")
	}
	empty := ""
	child := model.Sample{UUID: "child", InternalID: "C", SampleParents: model.SampleParents{FatherSampleInternalID: "D", FatherSampleUUID: "dad", FatherAffectedStatus: "carrier"}}
	if err := ss.applyParents(&child, model.SampleParentUpdate{FatherSampleInternalID: &empty, FatherAffectedStatus: &empty}); err != nil {
		t.Fatal(err)
	}
	if child.FatherSampleUUID != "" || child.FatherAffectedStatus != "" || child.FatherSampleInternalID != "" {
		t.Fatal("explicit clear ignored")
	}
	if err := ss.applyParents(&child, model.SampleParentUpdate{}); err != nil {
		t.Fatal(err)
	}
	parentName := "A"
	child.UUID = "b"
	child.InternalID = "B"
	mock.ExpectQuery(`SELECT \* FROM "samples"`).WillReturnRows(sqlmock.NewRows([]string{"uuid", "internal_id", "father_sample_internal_id"}).AddRow("a", "A", "B"))
	if err := ss.applyParents(&child, model.SampleParentUpdate{FatherSampleInternalID: &parentName}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatal("pending-name cycle accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestParentBlankFieldsDoNotLoadSampleScope(t *testing.T) {
	empty := ""
	child := &model.Sample{InternalID: "single"}
	// The default four blank fields must not query every sample during imports.
	if err := (&SampleService{}).applyParents(child, model.SampleParentUpdate{FatherSampleInternalID: &empty, MotherSampleInternalID: &empty, FatherAffectedStatus: &empty, MotherAffectedStatus: &empty}); err != nil {
		t.Fatal(err)
	}
}

func TestParentListEnrichmentUsesOneScopedQueryAcrossThePage(t *testing.T) {
	db, mock := newUploadTransactionTestDB(t)
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	ss := NewSampleService(&config.Config{Edition: "saas"})
	children := []model.Sample{{UUID: "child1", ExternalOrgID: "org-a", SampleParents: model.SampleParents{FatherSampleUUID: "dad", MotherSampleUUID: "mom"}}, {UUID: "child2", ExternalOrgID: "org-a", SampleParents: model.SampleParents{FatherSampleUUID: "dad", MotherSampleUUID: "mom"}}}
	mock.ExpectQuery(`SELECT \* FROM "samples" WHERE .*external_org_id =`).WillReturnRows(sqlmock.NewRows([]string{"uuid", "internal_id", "external_org_id"}).AddRow("dad", "DAD-OUTSIDE-PAGE", "org-a").AddRow("mom", "MOM-OUTSIDE-PAGE", "org-a").AddRow("outsider", "DAD-OUTSIDE-PAGE", "org-b"))
	if err := ss.enrichParents(children); err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		if child.Father.ID != "dad" || child.Mother.ID != "mom" || child.FatherSampleInternalID != "DAD-OUTSIDE-PAGE" {
			t.Fatal("parent outside page was lost")
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTrioPEDStatusSnapshotAndTotalEstimate(t *testing.T) {
	members := []model.PedigreeMember{{Gender: model.GenderMale, AffectedStatus: model.AffectedStatusAffected}, {Gender: model.GenderMale, AffectedStatus: model.AffectedStatusCarrier}, {Gender: model.GenderFemale, AffectedStatus: model.AffectedStatusUnaffected}}
	ped := trioPED("family", members)
	if !strings.Contains(ped, "proband\tfather\tmother\t1\t2") || !strings.Contains(ped, "father\t0\t0\t1\t0") || !strings.Contains(ped, "mother\t0\t0\t2\t1") {
		t.Fatal(ped)
	}
	total := int64(60 * 1024 * 1024 * 1024)
	if trioEstimatedMinutes(map[string]interface{}{"trio_input_bytes": total}) != estimateTaskMinutesFromBytes(total) {
		t.Fatal("Trio did not use total input bytes")
	}
	snapshot := []model.TrioMemberSnapshot{{Role: "father", SampleID: "original", AffectedStatus: "carrier"}}
	raw, _ := json.Marshal(map[string]interface{}{"trio_members": snapshot})
	task := model.Task{InputJSON: string(raw)}
	response := task.ToResponse()
	if len(response.TrioMembers) != 1 || response.TrioMembers[0].AffectedStatus != "carrier" || response.TrioMembers[0].SampleID != "original" {
		t.Fatal("snapshot lost")
	}
}

func TestTrioWDLConversionPreservesAdmissionEstimateAndMemberSnapshot(t *testing.T) {
	for _, genome := range []string{"hg19", "hg38"} {
		t.Run(genome, func(t *testing.T) {
			total := int64(29995918382)
			snapshot := []model.TrioMemberSnapshot{{Role: "proband", SampleID: "child"}, {Role: "father", SampleID: "father", AffectedStatus: "carrier"}, {Role: "mother", SampleID: "mother"}}
			inputs, err := buildCVMWDLInputs("trio", genome, map[string]interface{}{"trio_input_bytes": total, "trio_members": snapshot})
			if err != nil {
				t.Fatal(err)
			}
			if got := trioEstimatedMinutes(inputs); got != 168 {
				t.Fatalf("admission estimate=%d, want six-file estimate 168", got)
			}
			raw, err := json.Marshal(inputs)
			if err != nil {
				t.Fatal(err)
			}
			task := model.Task{InputJSON: string(raw)}
			members := task.ToResponse().TrioMembers
			if len(members) != 3 || members[1].SampleID != "father" || members[1].AffectedStatus != "carrier" {
				t.Fatalf("member snapshot lost during WDL conversion: %+v", members)
			}
			var persisted map[string]interface{}
			if err := json.Unmarshal(raw, &persisted); err != nil {
				t.Fatal(err)
			}
			if trioEstimatedMinutes(persisted) != 168 {
				t.Fatal("persisted estimate changed")
			}
		})
	}
}

func TestTrioReadinessDoesNotReloadOrRewriteCurrentSample(t *testing.T) {
	original := `{"TrioWES.meta_info":{"members":["proband","father","mother"],"read_1":["old-c1","old-f1","old-m1"],"read_2":["old-c2","old-f2","old-m2"]},"trio_members":[{"role":"father","sample_id":"old-father","affected_status":"carrier"}]}`
	task := &model.Task{Template: "trio", Executor: model.ExecutorCVM, SampleIDRef: 42, InputJSON: original}
	// No repositories are supplied: touching current sample data would panic.
	if ready, reason := (&TaskService{}).checkDataReady(task); !ready {
		t.Fatal(reason)
	}
	if task.InputJSON != original {
		t.Fatal("Trio snapshot was rewritten")
	}
}

func TestTrioQCGenderComparisonUsesSnapshotAfterSampleChangesOrDeletion(t *testing.T) {
	// No DB is supplied: new Trio results must not consult mutable sample/pedigree rows.
	previous := database.DB
	database.DB = nil
	t.Cleanup(func() { database.DB = previous })
	task := &model.Task{SampleID: "changed-or-deleted", PedigreeID: "old-family", InputJSON: `{"TrioWES.sry_sex_cutoff":25,"trio_members":[{"role":"proband","gender":"male"},{"role":"father","gender":"male"},{"role":"mother","gender":"female"}]}`}
	count := 100.0
	zero := 0.0
	qc := []model.QCMemberSummary{{MemberRole: "proband", Metrics: []model.QCMetric{{Key: "sryCount", Value: &count}}}, {MemberRole: "father", Metrics: []model.QCMetric{{Key: "sryCount", Value: &count}}}, {MemberRole: "mother", Metrics: []model.QCMetric{{Key: "sryCount", Value: &zero}}}}
	if err := (&ResultService{}).addQCGenderComparison(context.Background(), task, qc); err != nil {
		t.Fatal(err)
	}
	for i, expected := range []string{"male", "male", "female"} {
		if qc[i].DeclaredGender != expected || qc[i].GenderComparison != "match" {
			t.Fatalf("QC snapshot lost: %+v", qc[i])
		}
	}
}
