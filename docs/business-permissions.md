# Self-hosted business permissions

Enabled only by `DEPLOYMENT_EDITION=opensource`. SaaS keeps existing Squid authentication and tenant isolation.

All authenticated local users can browse, query and download local business resources. Local resources are shared across the instance; records with an external organization are excluded. Creator IDs, historical task tenant IDs, object keys and paths are retained. Credentials, personal views and preferences remain private. Report-service secrets are never returned.

Administrators always have all nine permissions. New, registered and existing ordinary users default to an empty JSONB permissions array. Administrators assign permissions through the user create/update API. An omitted update field preserves existing grants; `[]` revokes all. Unknown values return 400; duplicates are removed. Demotion clears grants unless explicitly supplied in the same request.

| Permission | Operations |
| --- | --- |
| samples.write | Samples, pedigrees, projects and sample/file matching |
| data.manage | Sequencing upload lifecycle and data management |
| tasks.manage | Ordinary task lifecycle and import retry |
| pipelines.manage | Pipelines and workflow parameters |
| gene_lists.manage | Gene list editing and publication |
| bed.manage | BED upload, validation and management |
| baselines.manage | Baseline creation and task lifecycle |
| report_services.manage | Report-service configuration and connection testing |
| interpretation_reports.manage | Interpretation edits, review, focus, screenshots, AI and report generation |

Writes read the current active user from the database, so permission changes apply to existing sessions on the next request. Denials return HTTP 403 with `code: permission_denied` and `required_permission`. Running tasks are not stopped. Existing signed upload URLs may remain usable, but subsequent backend upload completion requires current authorization.

Shared upload and task endpoints classify persisted resources, not user-supplied operation types. Uploads with sample matching additionally require `samples.write`; BED and baseline permissions remain independent of sequencing and ordinary-task permissions. POST queries, exports and downloads remain accessible without write grants. Shared history synchronization uses a vector of existing per-creator clocks, preserving cursor correctness without rewriting historical tenant IDs.

## Rollout

Back up the database, deploy Octopus first (AutoMigrate adds the non-null JSONB array defaulting to `[]`), then deploy YiJian. Existing ordinary users immediately become read-only until granted permissions. No data files are moved. Verify grants and revocation with temporary local accounts, then remove only their test records.

## Registering server FASTQ files (self-hosted local storage)

Copying FASTQ files into `STORAGE_LOCAL_DIR` alone does not register or display them in Data Center. Self-hosted local storage disables the legacy automatic directory scanner, even when `DATA_SCAN_LOCAL_DIR` is configured. Operators must submit the information file and pass validation before server-copied files receive asset UUIDs and become visible. Browser uploads already create explicit upload metadata and continue to appear normally; visibility is not based on whether the optional internal ID is empty. SaaS and S3 scanner behavior is unchanged.

Data Center provides a three-column XLSX template and bulk information upload: internal ID, Read1 absolute path and Read2 absolute path. Paths must be visible inside the Octopus container and resolve within `STORAGE_LOCAL_DIR`. The configured container directory is shown in the dialog. Mount host directories there before registering files; Windows drive paths are not container paths.

`POST /api/v1/data/local-import` registers one pair per request. It requires both `data.manage` and `samples.write` because the internal ID participates in sample matching. The route exists only in the open-source edition. It reads both files in full, checks gzip integrity, four-line FASTQ records, sequence/quality lengths, paired read IDs and record counts, and verifies that files did not change during reading. It records actual sizes, SHA-256 digests and independent file UUIDs. No second copy is created. File paths already in the inventory are rejected; registration is transactional per pair and current grants are rechecked before insertion. Structural integrity cannot establish that a dataset contains every original read without an independently supplied reference checksum or count.

Self-hosted automatic matching considers all local assets in the instance; SaaS retains its organization/user matching scope. A batch may partially succeed; the dialog retains per-row failures and retries only unsuccessful rows. Full scans of large files may take time. Closing the page cancels further requests. The XLSX file is limited to 1,000 rows and 2 MB.
