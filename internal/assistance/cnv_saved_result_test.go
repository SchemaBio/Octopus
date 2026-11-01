package assistance

import (
	"context"
	"strings"
	"testing"
)

func TestSavedCNVResultBindingAndEditionIsolation(t *testing.T) {
	for _, saas := range []bool{true, false} {
		t.Run(map[bool]string{true: "saas", false: "selfhosted"}[saas], func(t *testing.T) {
			s, _ := integrationService(t)
			s.SaaS = saas
			fixture, r := savedSVCFixture(t)
			s.Cipher = fixture.Cipher
			r.Feature = "cnv_loss"
			r.UserID = 7
			row := strings.Repeat("a", 64)
			var err error
			r.ResultCiphertext, err = seal(s.Cipher, r.ID+":result", Result{Feature: "cnv_loss", VariantID: row, Summary: "server evidence", ReviewerDecision: map[string]any{"confirmed": true}})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.DB.Create(&r).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if !s.CanSaveCalculatorResult(ctx, r.Scope, r.TaskID, 7, r.ID, "cnv") {
				t.Fatal("owner denied")
			}
			if s.CanSaveCalculatorResult(ctx, "other", r.TaskID, 8, r.ID, "cnv") == saas {
				t.Fatal("edition isolation incorrect")
			}
			if saas && s.CanSaveCalculatorResult(ctx, r.Scope, r.TaskID, 0, r.ID, "cnv") {
				t.Fatal("missing SaaS user accepted")
			}
			if _, err = s.SavedCNVResult(ctx, r.Scope, r.TaskID, row, r.ID); err != nil {
				t.Fatal(err)
			}
			saved, err := s.SavedCNVResult(ctx, r.Scope, r.TaskID, row, r.ID)
			if err != nil || saved["reviewerDecision"] != nil || saved["summary"] != "server evidence" {
				t.Fatal(saved, err)
			}
			if _, err = s.SavedCNVResult(ctx, r.Scope, r.TaskID, "other-row", r.ID); err == nil {
				t.Fatal("foreign row accepted")
			}
			if _, err = s.SavedCNVResult(ctx, r.Scope, "other-task", row, r.ID); err == nil {
				t.Fatal("foreign task accepted")
			}
			_, err = s.SavedCNVResult(ctx, "other", r.TaskID, row, r.ID)
			if (err != nil) != saas {
				t.Fatal("scope edition rule incorrect", err)
			}
			// Completed jobs with erased input must retain their row identity.
			r.ResultCiphertext, _ = seal(s.Cipher, r.ID+":result", Result{Feature: "cnv_loss", Summary: "unbound"})
			r.Ciphertext = ""
			if _, err = s.savedCNVResult(r, r.TaskID, row); err == nil {
				t.Fatal("unbound history accepted")
			}
		})
	}
}
