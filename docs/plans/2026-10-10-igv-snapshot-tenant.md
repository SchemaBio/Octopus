# IGV snapshot tenant column implementation plan

**Goal:** Save IGV screenshots for organization tenants whose canonical `org:<UUID>` scope has 40 characters.

**Architecture:** Keep canonical tenant identity unchanged and widen `IGVSnapshot.TenantID` to the existing task column capacity of 160. GORM AutoMigrate applies the same definition to PostgreSQL on startup. Deploy the corrected binary so later restarts retain the widened column.

**Tech Stack:** Go, GORM, PostgreSQL, COS, Docker Compose.

1. Update `internal/model/igv_snapshot.go` and add a PostgreSQL dialect schema regression test in `internal/model/igv_snapshot_test.go` using a real organization scope.
2. Run model and IGV service/handler tests. Build the corrected Octopus image, preserving the previous image and compose configuration for rollback.
3. Recreate only Octopus, check health and verify the live column is `varchar(160)`. Verify screenshot persistence with a real browser screenshot when available; never save synthetic evidence in a user task.
4. Document the incident and validation. Commit Octopus and the existing Sepiida Parquet changes separately; leave unchanged repositories without empty commits.
