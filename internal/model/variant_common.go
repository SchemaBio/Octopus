package model

import "time"

// ResultProvenance identifies the tenant and execution that produced a
// structured result.
type ResultProvenance struct {
	TenantID           string `json:"-" gorm:"size:160;index"`
	ExecutionAttemptID string `json:"-" gorm:"size:36;index"`
	ImportBatchID      uint   `json:"-" gorm:"index"`
}

// VariantReviewStatus is the base review/focus status for all variant types
type VariantReviewStatus struct {
	Reviewed   bool       `json:"reviewed" gorm:"default:false"`
	Focused    bool       `json:"focused" gorm:"default:false"`
	ReviewedBy string     `json:"reviewedBy,omitempty" gorm:"size:100"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty" gorm:"type:timestamptz"`
	FocusedBy  string     `json:"focusedBy,omitempty" gorm:"size:100"`
	FocusedAt  *time.Time `json:"focusedAt,omitempty" gorm:"type:timestamptz"`
}

// ACMGClassification represents ACMG variant classification
type ACMGClassification string

const (
	ACMGPathogenic       ACMGClassification = "Pathogenic"
	ACMGLikelyPathogenic ACMGClassification = "Likely_Pathogenic"
	ACMGVUS              ACMGClassification = "VUS"
	ACMGLikelyBenign     ACMGClassification = "Likely_Benign"
	ACMGBenign           ACMGClassification = "Benign"
)

// MarkReviewed marks a variant as reviewed
func (v *VariantReviewStatus) MarkReviewed(reviewer string) {
	v.Reviewed = true
	v.ReviewedBy = reviewer
	now := time.Now()
	v.ReviewedAt = &now
}

// MarkFocused marks a variant as focused
func (v *VariantReviewStatus) MarkFocused(focuser string) {
	v.Focused = true
	v.FocusedBy = focuser
	now := time.Now()
	v.FocusedAt = &now
}
