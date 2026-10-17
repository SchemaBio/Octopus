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
