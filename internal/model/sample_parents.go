package model

// SampleParents stores user input and stable identities separately. A deleted
// bound identity must never silently rebind to a newly created matching name.
type SampleParents struct {
	ParentsMigrated        bool              `json:"-" gorm:"not null;default:false"`
	FatherSampleInternalID string            `json:"father_sample_internal_id" gorm:"size:100;not null;default:''"`
	MotherSampleInternalID string            `json:"mother_sample_internal_id" gorm:"size:100;not null;default:''"`
	FatherAffectedStatus   string            `json:"father_affected_status" gorm:"size:20;not null;default:''"`
	MotherAffectedStatus   string            `json:"mother_affected_status" gorm:"size:20;not null;default:''"`
	FatherSampleUUID       string            `json:"-" gorm:"size:36;index;not null;default:''"`
	MotherSampleUUID       string            `json:"-" gorm:"size:36;index;not null;default:''"`
	Father                 *ParentSampleInfo `json:"father,omitempty" gorm:"-"`
	Mother                 *ParentSampleInfo `json:"mother,omitempty" gorm:"-"`
}

type SampleParentUpdate struct {
	FatherSampleInternalID *string `json:"father_sample_internal_id"`
	MotherSampleInternalID *string `json:"mother_sample_internal_id"`
	FatherAffectedStatus   *string `json:"father_affected_status"`
	MotherAffectedStatus   *string `json:"mother_affected_status"`
}

type ParentSampleInfo struct {
	State      string       `json:"state"`
	ID         string       `json:"id,omitempty"`
	InternalID string       `json:"internal_id,omitempty"`
	Gender     SampleGender `json:"gender,omitempty"`
	DataReady  bool         `json:"data_ready"`
	Warning    string       `json:"warning,omitempty"`
}

type TrioMemberSnapshot struct {
	Role           string       `json:"role"`
	SampleID       string       `json:"sample_id"`
	InternalID     string       `json:"internal_id"`
	Gender         SampleGender `json:"gender"`
	AffectedStatus string       `json:"affected_status"`
	Read1AssetID   string       `json:"read1_asset_id"`
	Read2AssetID   string       `json:"read2_asset_id"`
}

type TrioContext struct {
	Ready            bool                 `json:"ready"`
	Members          []TrioMemberSnapshot `json:"members"`
	Errors           []string             `json:"errors"`
	Warnings         []string             `json:"warnings"`
	EstimatedMinutes int                  `json:"estimated_minutes"`
}
