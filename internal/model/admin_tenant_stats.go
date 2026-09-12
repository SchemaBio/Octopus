package model

// AdminTenantStats is a bounded cross-tenant aggregate used by the platform
// administration console. The endpoint accepts at most 100 tenant IDs per
// request so the admin UI never has to scan the complete task/upload history.
type AdminTenantStats struct {
	OrgID                    string `json:"org_id"`
	TaskCount                int64  `json:"task_count"`
	FailedTaskCount          int64  `json:"failed_task_count"`
	RunningTaskCount         int64  `json:"running_task_count"`
	ResultImportFailureCount int64  `json:"result_import_failure_count"`
	UploadedFileCount        int64  `json:"uploaded_file_count"`
	UploadedBytes            int64  `json:"uploaded_bytes"`
	StaleUploadCount         int64  `json:"stale_upload_count"`
}
