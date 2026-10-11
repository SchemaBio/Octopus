package assistance

import (
	"encoding/json"
	"time"
)

var Engines = map[string]string{"hpo": "hpo_extract", "acmg": "acmg_amp_2015", "svcv4": "svc_v4", "cnv_gain": "cnv_gain_2020", "cnv_loss": "cnv_loss_2020"}

type Settings struct {
	ID         uint `gorm:"primaryKey"`
	Enabled    bool
	Configured bool
	Version    int
	ProfileID  string
	UpstreamID string
	Ciphertext string
	UpdatedAt  time.Time
}

func (Settings) TableName() string { return "ai_assistance_settings" }

type SettingsEvent struct {
	ID        uint `gorm:"primaryKey"`
	UserID    uint
	OrgID     string
	Reason    string
	CreatedAt time.Time
}

func (SettingsEvent) TableName() string { return "ai_assistance_settings_events" }

type ProfileRecord struct {
	Version    int `gorm:"primaryKey"`
	ProfileID  string
	UpstreamID string
	Deleted    bool
	CreatedAt  time.Time
}

func (ProfileRecord) TableName() string { return "ai_assistance_profiles" }

type Request struct {
	ID               string `gorm:"primaryKey;size:36"`
	Scope            string `gorm:"size:160;uniqueIndex:ai_request_key"`
	UserID           uint   `gorm:"uniqueIndex:ai_request_key"`
	Key              string `gorm:"size:64;uniqueIndex:ai_request_key"`
	Hash             string `gorm:"size:64"`
	TaskID           string `gorm:"size:36;index"`
	OrgID            string `gorm:"size:100"`
	Feature          string `gorm:"size:20"`
	State            string `gorm:"size:30;index"`
	Ciphertext       string
	ResultCiphertext string
	JobID            string
	UpstreamID       string `gorm:"index"`
	ExecutionOwner   string
	UpstreamDeleted  bool
	ErrorCode        string
	CancelRequested  bool
	BillingEnabled   bool
	BillingReserved  bool
	BillingDone      bool
	Billable         bool
	ProfileVersion   int
	LeaseUntil       time.Time `gorm:"index"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (Request) TableName() string { return "ai_assistance_requests" }

type Draft struct {
	Enabled  bool   `json:"enabled"`
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
	Model    string `json:"model"`
	Protocol string `json:"protocol"`
	ClearKey bool   `json:"clearKey"`
	Reason   string `json:"reason"`
}
type Context struct {
	DiseaseID     string   `json:"diseaseId"`
	Inheritance   string   `json:"inheritance"`
	SampleType    string   `json:"sampleType"`
	VariantOrigin string   `json:"variantOrigin"`
	HPOTerms      []string `json:"hpoTerms"`
}
type Input struct {
	Feature         string          `json:"feature"`
	Mode            string          `json:"mode,omitempty"`
	DraftAssessment json.RawMessage `json:"draftAssessment,omitempty"`
	TaskID          string          `json:"taskId"`
	Reference       string          `json:"reference"`
	Diagnosis       string          `json:"diagnosis"`
	Variant         map[string]any  `json:"variant"`
	Calculator      json.RawMessage `json:"calculator"`
	ClinicalContext Context         `json:"clinicalContext"`
	ConfirmedCharge bool            `json:"confirmedCharge"`
}
type StoredInput struct {
	Input          Input
	ProfileID      string
	ProfileVersion int
	Path           string
	Payload        map[string]any
	Model          string
	ServiceVersion string
	RulesetHash    string
}
type Capability struct {
	Enabled        bool   `json:"enabled"`
	Reason         string `json:"reason,omitempty"`
	Mode           string `json:"mode"`
	BillingEnabled bool   `json:"billingEnabled"`
	RulesetHash    string `json:"rulesetHash,omitempty"`
}
type PublicSettings struct {
	Enabled        bool                  `json:"enabled"`
	Connected      bool                  `json:"connected"`
	Configured     bool                  `json:"configured"`
	BaseURL        string                `json:"baseUrl,omitempty"`
	Model          string                `json:"model,omitempty"`
	Protocol       string                `json:"protocol"`
	KeyConfigured  bool                  `json:"keyConfigured"`
	BillingEnabled bool                  `json:"billingEnabled"`
	Capabilities   map[string]Capability `json:"capabilities"`
	ServiceVersion string                `json:"serviceVersion,omitempty"`
}
type Result struct {
	Feature            string              `json:"feature"`
	Model              string              `json:"model,omitempty"`
	GeneratedAt        string              `json:"generatedAt"`
	Summary            string              `json:"summary"`
	Classification     string              `json:"classification,omitempty"`
	Evidence           []map[string]string `json:"evidence,omitempty"`
	HPOTerms           []HPOTerm           `json:"hpoTerms,omitempty"`
	Warnings           []string            `json:"warnings,omitempty"`
	AgentProposal      any                 `json:"agentProposal,omitempty"`
	RuleEngineResult   any                 `json:"ruleEngineResult,omitempty"`
	ReviewerDecision   any                 `json:"reviewerDecision,omitempty"`
	ResultKind         string              `json:"resultKind,omitempty"`
	ExperimentalResult any                 `json:"experimentalResult,omitempty"`
	RulesetHash        string              `json:"rulesetHash,omitempty"`
	DatasetHash        string              `json:"datasetHash,omitempty"`
}
type HPOTerm struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Polarity  string `json:"polarity"`
	SpanStart *int   `json:"spanStart,omitempty"`
	SpanEnd   *int   `json:"spanEnd,omitempty"`
	SpanUnit  string `json:"spanUnit,omitempty"`
}
type View struct {
	ID        string  `json:"requestId"`
	Status    string  `json:"status"`
	Result    *Result `json:"result,omitempty"`
	ErrorCode string  `json:"errorCode,omitempty"`
}
