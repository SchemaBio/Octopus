package model

import (
	"encoding/json"
	"strconv"
	"time"
)

// Gender represents gender
type SampleGender string

const (
	SampleGenderMale    SampleGender = "male"
	SampleGenderFemale  SampleGender = "female"
	SampleGenderUnknown SampleGender = "unknown"
)

// SampleTypeFrontend represents sample type matching frontend
type SampleTypeFrontend string

const (
	SampleTypeWholeBlood SampleTypeFrontend = "全血"
	SampleTypeSaliva     SampleTypeFrontend = "唾液"
	SampleTypeDNA        SampleTypeFrontend = "DNA"
	SampleTypeTissue     SampleTypeFrontend = "组织"
	SampleTypeOther      SampleTypeFrontend = "其他"
)

// SampleStatus represents the status of a sample
type SampleStatus string

const (
	SampleStatusPending    SampleStatus = "pending"
	SampleStatusProcessing SampleStatus = "processing"
	SampleStatusCompleted  SampleStatus = "completed"
	SampleStatusFailed     SampleStatus = "failed"
)

// Sample represents a biological sample (germline)
type Sample struct {
	ID                  uint               `json:"-" gorm:"primaryKey"`
	UUID                string             `json:"id" gorm:"uniqueIndex;size:36;not null"`
	InternalID          string             `json:"internal_id" gorm:"uniqueIndex;size:100;not null"`
	Gender              SampleGender       `json:"gender" gorm:"size:20;default:unknown"`
	Age                 *int               `json:"age,omitempty"`
	SampleType          SampleTypeFrontend `json:"sample_type" gorm:"size:20;default:其他"`
	Batch               string             `json:"batch" gorm:"size:100"`
	ClinicalDiagnosis   string             `json:"clinical_diagnosis" gorm:"type:text"` // JSON
	HPOTerms            string             `json:"-" gorm:"type:text"`                  // JSON
	MatchedPair         string             `json:"-" gorm:"type:text"`                  // JSON
	SubmissionInfo      string             `json:"-" gorm:"type:text"`                  // JSON
	ProjectInfo         string             `json:"-" gorm:"type:text"`                  // JSON
	FamilyHistory       string             `json:"-" gorm:"type:text"`                  // JSON
	Remark              string             `json:"remark" gorm:"type:text"`
	Status              SampleStatus       `json:"status" gorm:"size:20;default:pending"`
	ProjectID           uint               `json:"project_id" gorm:"index"`
	CreatedBy           uint               `json:"created_by" gorm:"index"`
	CreatedAt           time.Time          `json:"created_at"`
	UpdatedAt           time.Time          `json:"updated_at"`
}

// HPOTerm represents an HPO term
type HPOTerm struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MatchedPair represents FASTQ file pair
type MatchedPair struct {
	R1Path string `json:"r1Path"`
	R2Path string `json:"r2Path"`
}

// ClinicalDiagnosisInfo is the structured clinical diagnosis
type ClinicalDiagnosisInfo struct {
	MainDiagnosis  string     `json:"mainDiagnosis"`
	Symptoms       []string   `json:"symptoms"`
	HPOTerms       []HPOTerm  `json:"hpoTerms,omitempty"`
	OnsetAge       string     `json:"onsetAge,omitempty"`
	DiseaseHistory string     `json:"diseaseHistory,omitempty"`
}

// SubmissionInfo represents sample submission details
type SubmissionInfo struct {
	SubmissionDate      string `json:"submissionDate"`
	SampleCollectionDate string `json:"sampleCollectionDate"`
	SampleReceiveDate   string `json:"sampleReceiveDate"`
	SampleQuality       string `json:"sampleQuality"` // good, acceptable, poor
}

// ProjectInfo represents project association
type ProjectInfo struct {
	ProjectID      string   `json:"projectId"`
	ProjectName    string   `json:"projectName"`
	TestItems      []string `json:"testItems"`
	Panel          string   `json:"panel,omitempty"`
	TurnaroundDays int      `json:"turnaroundDays"`
	Priority       string   `json:"priority"` // normal, urgent
}

// FamilyHistoryInfo represents family history
type FamilyHistoryInfo struct {
	HasHistory      bool              `json:"hasHistory"`
	AffectedMembers []AffectedMember  `json:"affectedMembers,omitempty"`
	PedigreeNote    string            `json:"pedigreeNote,omitempty"`
}

// AffectedMember represents an affected family member
type AffectedMember struct {
	Relation  string `json:"relation"`
	Condition string `json:"condition"`
	OnsetAge  string `json:"onsetAge,omitempty"`
}

// SampleResponse is the API response for a sample list item
type SampleResponse struct {
	ID                string             `json:"id"`
	InternalID        string             `json:"internal_id"`
	Gender            SampleGender       `json:"gender"`
	Age               *int               `json:"age,omitempty"`
	SampleType        SampleTypeFrontend `json:"sample_type"`
	Batch             string             `json:"batch"`
	ClinicalDiagnosis string             `json:"clinical_diagnosis"`
	HPOTerms          []HPOTerm          `json:"hpo_terms"`
	MatchedPair       *MatchedPair       `json:"matched_pair,omitempty"`
	Remark            string             `json:"remark"`
	Status            SampleStatus       `json:"status"`
	CreatedAt         string             `json:"created_at"`
	UpdatedAt         string             `json:"updated_at"`
}

// SampleDetailResponse is the API response for a sample detail
type SampleDetailResponse struct {
	ID                string              `json:"id"`
	InternalID        string              `json:"internal_id"`
	Gender            SampleGender        `json:"gender"`
	Age               *int                `json:"age,omitempty"`
	SampleType        SampleTypeFrontend  `json:"sample_type"`
	Batch             string              `json:"batch"`
	MatchedPair       *MatchedPair        `json:"matched_pair,omitempty"`
	Remark            string              `json:"remark"`
	ClinicalDiagnosis ClinicalDiagnosisInfo `json:"clinical_diagnosis"`
	SubmissionInfo    SubmissionInfo       `json:"submission_info"`
	ProjectInfo       ProjectInfo          `json:"project_info"`
	FamilyHistory     FamilyHistoryInfo    `json:"family_history"`
	AnalysisTasks     []AnalysisTaskBrief  `json:"analysis_tasks"`
	CreatedAt         string               `json:"created_at"`
	UpdatedAt         string               `json:"updated_at"`
}

// AnalysisTaskBrief is a brief task reference in sample detail
type AnalysisTaskBrief struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// SampleCreateRequest is the request body for creating a sample
type SampleCreateRequest struct {
	InternalID        string             `json:"internal_id" binding:"required"`
	Gender            SampleGender       `json:"gender"`
	Age               *int               `json:"age"`
	SampleType        SampleTypeFrontend `json:"sample_type"`
	Batch             string             `json:"batch"`
	ClinicalDiagnosis string             `json:"clinical_diagnosis"`
	HPOTerms          []HPOTerm          `json:"hpo_terms"`
	R1Path            string             `json:"r1_path"`
	R2Path            string             `json:"r2_path"`
	Remark            string             `json:"remark"`
}

// SampleUpdateRequest is the request body for updating a sample
type SampleUpdateRequest struct {
	InternalID        string             `json:"internal_id"`
	Gender            SampleGender       `json:"gender"`
	Age               *int               `json:"age"`
	SampleType        SampleTypeFrontend `json:"sample_type"`
	Batch             string             `json:"batch"`
	ClinicalDiagnosis string             `json:"clinical_diagnosis"`
	HPOTerms          []HPOTerm          `json:"hpo_terms"`
	R1Path            string             `json:"r1_path"`
	R2Path            string             `json:"r2_path"`
	Remark            string             `json:"remark"`
	Status            SampleStatus       `json:"status"`
}

// SampleListQuery is the query parameters for listing samples
type SampleListQuery struct {
	Page       int              `form:"page" binding:"min=1"`
	PageSize   int              `form:"page_size" binding:"min=1,max=100"`
	Search     string           `form:"search"`
	Status     SampleStatus     `form:"status"`
	SampleType SampleTypeFrontend `form:"sample_type"`
	ProjectID  uint             `form:"project_id"`
}

// SampleListResponse is the response for listing samples
type SampleListResponse struct {
	Total int              `json:"total"`
	Items []SampleResponse `json:"items"`
}

// TableName specifies the table name for Sample
func (Sample) TableName() string {
	return "samples"
}

// GetHPOTerms parses HPOTerms JSON
func (s *Sample) GetHPOTerms() []HPOTerm {
	if s.HPOTerms == "" {
		return []HPOTerm{}
	}
	var terms []HPOTerm
	json.Unmarshal([]byte(s.HPOTerms), &terms)
	return terms
}

// SetHPOTerms sets HPOTerms JSON
func (s *Sample) SetHPOTerms(terms []HPOTerm) {
	b, _ := json.Marshal(terms)
	s.HPOTerms = string(b)
}

// GetMatchedPair parses MatchedPair JSON
func (s *Sample) GetMatchedPair() *MatchedPair {
	if s.MatchedPair == "" {
		return nil
	}
	var mp MatchedPair
	json.Unmarshal([]byte(s.MatchedPair), &mp)
	return &mp
}

// SetMatchedPair sets MatchedPair JSON
func (s *Sample) SetMatchedPair(mp *MatchedPair) {
	if mp == nil {
		s.MatchedPair = ""
		return
	}
	b, _ := json.Marshal(mp)
	s.MatchedPair = string(b)
}

// GetSubmissionInfo parses SubmissionInfo JSON
func (s *Sample) GetSubmissionInfo() SubmissionInfo {
	if s.SubmissionInfo == "" {
		return SubmissionInfo{}
	}
	var info SubmissionInfo
	json.Unmarshal([]byte(s.SubmissionInfo), &info)
	return info
}

// SetSubmissionInfo sets SubmissionInfo JSON
func (s *Sample) SetSubmissionInfo(info SubmissionInfo) {
	b, _ := json.Marshal(info)
	s.SubmissionInfo = string(b)
}

// GetProjectInfo parses ProjectInfo JSON
func (s *Sample) GetProjectInfo() ProjectInfo {
	if s.ProjectInfo == "" {
		return ProjectInfo{}
	}
	var info ProjectInfo
	json.Unmarshal([]byte(s.ProjectInfo), &info)
	return info
}

// SetProjectInfo sets ProjectInfo JSON
func (s *Sample) SetProjectInfo(info ProjectInfo) {
	b, _ := json.Marshal(info)
	s.ProjectInfo = string(b)
}

// GetFamilyHistory parses FamilyHistory JSON
func (s *Sample) GetFamilyHistory() FamilyHistoryInfo {
	if s.FamilyHistory == "" {
		return FamilyHistoryInfo{}
	}
	var info FamilyHistoryInfo
	json.Unmarshal([]byte(s.FamilyHistory), &info)
	return info
}

// SetFamilyHistory sets FamilyHistory JSON
func (s *Sample) SetFamilyHistory(info FamilyHistoryInfo) {
	b, _ := json.Marshal(info)
	s.FamilyHistory = string(b)
}

// SampleToResponse converts Sample to SampleResponse
func SampleToResponse(s *Sample) SampleResponse {
	return SampleResponse{
		ID:                s.UUID,
		InternalID:        s.InternalID,
		Gender:            s.Gender,
		Age:               s.Age,
		SampleType:        s.SampleType,
		Batch:             s.Batch,
		ClinicalDiagnosis: s.ClinicalDiagnosis,
		HPOTerms:          s.GetHPOTerms(),
		MatchedPair:       s.GetMatchedPair(),
		Remark:            s.Remark,
		Status:            s.Status,
		CreatedAt:         s.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         s.UpdatedAt.Format(time.RFC3339),
	}
}

// FormatID converts uint to string
func FormatID(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
