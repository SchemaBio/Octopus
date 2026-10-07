package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/SchemaBio/Octopus/internal/database"
	"github.com/SchemaBio/Octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const browserAssessmentProfile = "germline-browser-assessment-v1"

var validHPO = regexp.MustCompile(`^HP:\d{7}$`)

type BrowserAssessmentContext struct {
	TaskID       string                     `json:"taskId"`
	AttemptID    string                     `json:"attemptId"`
	Version      string                     `json:"version"`
	Reference    string                     `json:"reference"`
	HPO          []string                   `json:"hpo"`
	HPOVersion   string                     `json:"hpoVersion"`
	Pack         json.RawMessage            `json:"pack"`
	Proofs       map[string]json.RawMessage `json:"proofs"`
	Members      interface{}                `json:"members"`
	Availability []string                   `json:"availability"`
	Tables       []string                   `json:"tables"`
	Profile      string                     `json:"profile"`
}
type BrowserAssessmentEnvelope struct {
	Active                *BrowserAssessmentContext `json:"active"`
	LatestVersion         string                    `json:"latestVersion"`
	ReassessmentAvailable bool                      `json:"reassessmentAvailable"`
}

func digestAssessment(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }

// Reference files are deployed by administrators, not fetched from client URLs.
// The current release is named hg19.json/hg38.json and older versions are kept in
// the context JSON, ensuring an upgraded package cannot silently alter a session.
func (s *ResultService) latestAssessmentContext(ctx context.Context, task *model.Task) (*BrowserAssessmentContext, error) {
	workspace, err := s.GetContext(ctx, task)
	if err != nil {
		return nil, err
	}
	if workspace.State != "ready" {
		return nil, ErrParquetIncomplete
	}
	reference := workspace.Reference.DeclaredID
	if reference != "hg19" && reference != "hg38" {
		reference = "unknown"
	}
	c := &BrowserAssessmentContext{TaskID: task.UUID, AttemptID: executionAttempt(task), Reference: reference, HPO: []string{}, Proofs: map[string]json.RawMessage{}, Members: workspace.Members, Availability: []string{}, Tables: workspace.Parquet.Tables, Profile: browserAssessmentProfile}
	if task.SampleID != "" {
		var sample model.Sample
		query := database.DB.WithContext(ctx).Where("uuid = ?", task.SampleID)
		if task.ExternalOrgID != "" {
			query = query.Where("external_org_id=?", task.ExternalOrgID)
		} else {
			query = query.Where("created_by=? AND COALESCE(external_org_id,'')=''", task.CreatedBy)
		}
		err = query.First(&sample).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			c.HPO = positiveAssessmentHPO(sample.HPOTerms)
		}
	}
	sort.Strings(c.HPO)
	c.HPOVersion = digestAssessment([]byte(fmt.Sprint(c.HPO)))
	empty := map[string]interface{}{"version": "unavailable", "reference": reference, "licenses": []interface{}{}, "hpo": map[string]interface{}{}, "diseases": []interface{}{}, "dosage": []interface{}{}, "str": []interface{}{}, "imprinting": []interface{}{}}
	c.Pack, _ = json.Marshal(empty)
	if reference != "unknown" {
		file, openErr := os.Open(filepath.Join(s.cfg.ResultQuery.ReferenceDir, reference+".json"))
		if openErr == nil {
			raw, readErr := io.ReadAll(io.LimitReader(file, 32*1024*1024+1))
			file.Close()
			if readErr != nil || len(raw) > 32*1024*1024 {
				return nil, fmt.Errorf("reference evidence package exceeds limit")
			}
			var header struct {
				Version   string `json:"version"`
				Reference string `json:"reference"`
				Licenses  []struct {
					Source     string `json:"source"`
					License    string `json:"license"`
					AccessedAt string `json:"accessedAt"`
				} `json:"licenses"`
			}
			if json.Unmarshal(raw, &header) != nil || header.Version == "" || header.Reference != reference || len(header.Licenses) == 0 {
				return nil, fmt.Errorf("invalid reference evidence package")
			}
			for _, license := range header.Licenses {
				if license.Source == "" || license.License == "" || license.AccessedAt == "" {
					return nil, fmt.Errorf("unverified evidence license")
				}
			}
			if err := validateAssessmentPack(raw, reference); err != nil {
				return nil, err
			}
			c.Pack = raw
			var coverage struct {
				STR        []json.RawMessage `json:"str"`
				Imprinting []json.RawMessage `json:"imprinting"`
			}
			_ = json.Unmarshal(raw, &coverage)
			if len(coverage.STR) == 0 {
				c.Availability = append(c.Availability, "STR 疾病阈值包未配置；不会根据报告中的未核验阈值自动置顶")
			}
			if len(coverage.Imprinting) == 0 {
				c.Availability = append(c.Availability, "印记区域证据包未配置；UPD 规则保持证据不足")
			}
		} else if !os.IsNotExist(openErr) {
			return nil, fmt.Errorf("reference evidence package unreadable")
		} else {
			c.Availability = append(c.Availability, "本地证据包未发布；疾病、表型及家系规则不会推断缺失证据")
		}
	}
	proofs, proofErr := s.loadAssessmentProofs(ctx, task, reference)
	if proofErr != nil {
		return nil, proofErr
	}
	if proofs != nil {
		c.Proofs = proofs
		usableGenotypes := 0
		for _, raw := range proofs {
			var proof struct {
				Proband *struct {
					GQ *float64 `json:"gq"`
				} `json:"proband"`
			}
			if json.Unmarshal(raw, &proof) == nil && proof.Proband != nil && proof.Proband.GQ != nil {
				usableGenotypes++
			}
		}
		if usableGenotypes == 0 {
			c.Availability = append(c.Availability, "当前归档没有先证者 GQ；依赖家系质量的候选及 ACMG 家系证据保持未选")
		}
	} else {
		c.Availability = append(c.Availability, "归档尚未提供经核验的 GQ、亲缘及相位证据；相关家系条款保持未选")
	}
	versionBytes, _ := json.Marshal(struct {
		Context        *BrowserAssessmentContext
		DatasetVersion string
	}{c, workspace.Version})
	c.Version = digestAssessment(versionBytes)
	return c, nil
}

func positiveAssessmentHPO(raw string) []string {
	var terms []struct {
		ID        string `json:"id"`
		Negated   bool   `json:"negated"`
		Excluded  bool   `json:"excluded"`
		Qualifier string `json:"qualifier"`
	}
	if json.Unmarshal([]byte(raw), &terms) != nil {
		return []string{}
	}
	seen := map[string]bool{}
	result := []string{}
	for _, term := range terms {
		if validHPO.MatchString(term.ID) && !term.Negated && !term.Excluded && term.Qualifier != "NOT" && !seen[term.ID] {
			seen[term.ID] = true
			result = append(result, term.ID)
		}
	}
	sort.Strings(result)
	return result
}

// Fail a malformed administrative package before a browser starts its Worker.
// In particular, null arrays and unknown HPO parents must not yield partial
// statistics or silently change the ontology used for phenotype matching.
func validateAssessmentPack(raw []byte, reference string) error {
	var pack struct {
		HPO map[string]struct {
			ID        string   `json:"id"`
			IC        float64  `json:"ic"`
			Ancestors []string `json:"ancestors"`
		} `json:"hpo"`
		Diseases []struct {
			ID          string   `json:"id"`
			Gene        string   `json:"gene"`
			HPO         []string `json:"hpo"`
			Validity    string   `json:"validity"`
			Inheritance string   `json:"inheritance"`
		} `json:"diseases"`
		Dosage     []json.RawMessage `json:"dosage"`
		STR        []json.RawMessage `json:"str"`
		Imprinting []json.RawMessage `json:"imprinting"`
	}
	bad := func() error { return fmt.Errorf("invalid reference evidence schema") }
	if json.Unmarshal(raw, &pack) != nil || pack.HPO == nil || pack.Diseases == nil || pack.Dosage == nil || pack.STR == nil || pack.Imprinting == nil {
		return bad()
	}
	for id, term := range pack.HPO {
		if !validHPO.MatchString(id) || term.ID != id || math.IsNaN(term.IC) || math.IsInf(term.IC, 0) || term.IC < 0 || term.Ancestors == nil {
			return bad()
		}
		for _, parent := range term.Ancestors {
			if _, ok := pack.HPO[parent]; !ok {
				return bad()
			}
		}
	}
	for _, d := range pack.Diseases {
		if d.ID == "" || d.Gene == "" || !map[string]bool{"Moderate": true, "Strong": true, "Definitive": true}[d.Validity] || !map[string]bool{"AD": true, "AR": true, "XL": true, "MT": true}[d.Inheritance] {
			return bad()
		}
		for _, id := range d.HPO {
			if _, ok := pack.HPO[id]; !ok {
				return bad()
			}
		}
	}
	for _, group := range [][]json.RawMessage{pack.Dosage, pack.STR, pack.Imprinting} {
		for _, raw := range group {
			var interval struct {
				Reference  string `json:"reference"`
				Chromosome string `json:"chromosome"`
				Start      int64  `json:"start"`
				End        int64  `json:"end"`
			}
			if json.Unmarshal(raw, &interval) != nil || interval.Reference != reference || interval.Chromosome == "" || interval.Start < 0 || interval.End <= interval.Start {
				return bad()
			}
		}
	}
	return nil
}

// Sidecars are supplied by the controlled archive adapter. Their ownership and
// immutable dataset checksums are checked before any row proof reaches a browser.
func (s *ResultService) loadAssessmentProofs(ctx context.Context, task *model.Task, reference string) (map[string]json.RawMessage, error) {
	if _, err := uuid.Parse(task.UUID); err != nil {
		return nil, nil
	}
	if _, err := uuid.Parse(executionAttempt(task)); err != nil {
		return nil, nil
	}
	filename := filepath.Join(s.cfg.ResultQuery.ReferenceDir, "task-proofs", digestAssessment([]byte(model.TenantIDForTask(task))), task.UUID, executionAttempt(task)+".json")
	f, err := os.Open(filename)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("assessment sidecar unreadable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 64*1024*1024+1))
	if err != nil || len(raw) > 64*1024*1024 {
		return nil, fmt.Errorf("assessment sidecar exceeds limit")
	}
	var sidecar struct {
		TaskID    string                     `json:"taskId"`
		AttemptID string                     `json:"attemptId"`
		Reference string                     `json:"reference"`
		Datasets  map[string]string          `json:"datasets"`
		Proofs    map[string]json.RawMessage `json:"proofs"`
	}
	if json.Unmarshal(raw, &sidecar) != nil || sidecar.TaskID != task.UUID || sidecar.AttemptID != executionAttempt(task) || sidecar.Reference != reference || len(sidecar.Datasets) == 0 {
		return nil, fmt.Errorf("assessment sidecar identity mismatch")
	}
	for table, hash := range sidecar.Datasets {
		if !validParquetTable(table) {
			return nil, fmt.Errorf("invalid proof table")
		}
		var d model.ResultDataset
		if err := database.DB.WithContext(ctx).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=? AND \"table\"=?", model.TenantIDForTask(task), task.UUID, executionAttempt(task), table).First(&d).Error; err != nil {
			return nil, err
		}
		if d.ObjectSHA256 != hash {
			return nil, ErrAdjustmentConflict
		}
	}
	for row, rawProof := range sidecar.Proofs {
		if !validResultRowID(row) {
			return nil, fmt.Errorf("invalid proof row identity")
		}
		var p struct {
			Reference string `json:"reference"`
		}
		if json.Unmarshal(rawProof, &p) != nil || p.Reference != reference {
			return nil, fmt.Errorf("invalid proof reference")
		}
	}
	return sidecar.Proofs, nil
}
func (s *ResultService) AssessmentContext(ctx context.Context, task *model.Task) (*BrowserAssessmentEnvelope, error) {
	latest, err := s.latestAssessmentContext(ctx, task)
	if err != nil {
		return nil, err
	}
	var saved model.ResultAssessmentContext
	err = database.DB.WithContext(ctx).Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=?", model.TenantIDForTask(task), task.UUID, executionAttempt(task)).Order("activated_at DESC, created_at DESC, id DESC").First(&saved).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &BrowserAssessmentEnvelope{LatestVersion: latest.Version}, nil
	}
	if err != nil {
		return nil, err
	}
	var active BrowserAssessmentContext
	if json.Unmarshal([]byte(saved.PayloadJSON), &active) != nil {
		return nil, fmt.Errorf("invalid assessment snapshot")
	}
	if err := expandAssessmentArtifacts(database.DB.WithContext(ctx), &active); err != nil {
		return nil, err
	}
	return &BrowserAssessmentEnvelope{Active: &active, LatestVersion: latest.Version, ReassessmentAvailable: active.Version != latest.Version}, nil
}
func (s *ResultService) ActivateAssessmentContext(ctx context.Context, task *model.Task, expected string) (*BrowserAssessmentContext, error) {
	latest, err := s.latestAssessmentContext(ctx, task)
	if err != nil {
		return nil, err
	}
	err = database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize initialization/reassessment with task deletion and retry.
		var locked model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id=?", task.ID).Error; err != nil {
			return err
		}
		if executionAttempt(&locked) != executionAttempt(task) {
			return ErrAdjustmentConflict
		}
		var previous model.ResultAssessmentContext
		err := tx.Where("tenant_id=? AND task_uuid=? AND execution_attempt_id=?", model.TenantIDForTask(task), task.UUID, executionAttempt(task)).Order("activated_at DESC, created_at DESC, id DESC").First(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && expected == "" {
			if err := json.Unmarshal([]byte(previous.PayloadJSON), latest); err != nil {
				return err
			}
			return expandAssessmentArtifacts(tx, latest)
		}
		if (err == nil && previous.Version != expected) || (errors.Is(err, gorm.ErrRecordNotFound) && expected != "") {
			return ErrAdjustmentConflict
		}
		snapshot := *latest
		packID, err := persistAssessmentArtifact(tx, latest.Pack)
		if err != nil {
			return err
		}
		snapshot.Pack, _ = json.Marshal(map[string]string{"artifactSha256": packID})
		proofs, _ := json.Marshal(latest.Proofs)
		proofID, err := persistAssessmentArtifact(tx, proofs)
		if err != nil {
			return err
		}
		marker, _ := json.Marshal(map[string]string{"artifactSha256": proofID})
		snapshot.Proofs = map[string]json.RawMessage{"__artifact": marker}
		raw, _ := json.Marshal(snapshot)
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.Assignments(map[string]interface{}{"activated_at": time.Now().UTC()})}).Create(&model.ResultAssessmentContext{ID: digestAssessment([]byte(model.TenantIDForTask(task) + "/" + task.UUID + "/" + latest.AttemptID + "/" + latest.Version)), TenantID: model.TenantIDForTask(task), TaskUUID: task.UUID, ExecutionAttemptID: latest.AttemptID, Version: latest.Version, PayloadJSON: string(raw), ActivatedAt: time.Now().UTC()}).Error
	})
	return latest, err
}

func persistAssessmentArtifact(tx *gorm.DB, raw json.RawMessage) (string, error) {
	id := digestAssessment(raw)
	err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.ResultAssessmentArtifact{ID: id, PayloadJSON: string(raw)}).Error
	return id, err
}
func expandAssessmentArtifacts(tx *gorm.DB, c *BrowserAssessmentContext) error {
	var marker struct {
		ID string `json:"artifactSha256"`
	}
	if json.Unmarshal(c.Pack, &marker) == nil && marker.ID != "" {
		var artifact model.ResultAssessmentArtifact
		if err := tx.First(&artifact, "id=?", marker.ID).Error; err != nil {
			return err
		}
		c.Pack = json.RawMessage(artifact.PayloadJSON)
	}
	if raw, ok := c.Proofs["__artifact"]; ok {
		if json.Unmarshal(raw, &marker) != nil || marker.ID == "" {
			return fmt.Errorf("invalid proof artifact")
		}
		var artifact model.ResultAssessmentArtifact
		if err := tx.First(&artifact, "id=?", marker.ID).Error; err != nil {
			return err
		}
		// Unmarshal merges into a non-nil map; discard the storage marker first.
		c.Proofs = nil
		if json.Unmarshal([]byte(artifact.PayloadJSON), &c.Proofs) != nil {
			return fmt.Errorf("invalid proof content")
		}
	}
	return nil
}
