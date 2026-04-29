package model

import "time"

// VariantReviewStatus is the base review/report status for all variant types
type VariantReviewStatus struct {
	Reviewed   bool       `json:"reviewed" gorm:"default:false"`
	Reported   bool       `json:"reported" gorm:"default:false"`
	ReviewedBy string     `json:"reviewedBy,omitempty" gorm:"size:100"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty"`
	ReportedBy string     `json:"reportedBy,omitempty" gorm:"size:100"`
	ReportedAt *time.Time `json:"reportedAt,omitempty"`
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

// MarkReported marks a variant as reported
func (v *VariantReviewStatus) MarkReported(reporter string) {
	v.Reported = true
	v.ReportedBy = reporter
	now := time.Now()
	v.ReportedAt = &now
}
