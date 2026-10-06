# BAM seven-day retention

## Policy

SaaS uses `BAM_RETENTION_DAYS=7`. The deadline is the successful CVM attempt's `finished_at + 168 hours`, in UTC. It is not based on upload, import, last access or the task creation date. Retries and result reimports do not extend a recorded deadline. Self-hosted installations default to `0` (disabled).

`BAM_CLEANUP_ENABLED=true` enables physical cleanup. With it disabled, the seven-day access cutoff and completion registration still apply. Startup validates this option requires a seven-day policy and COS/S3 storage.

## Durable completion identity

`bam_retention_jobs` has a unique `(task_uuid, org_id, attempt_id)` and stores the bucket, completion, deadline, object plan, manifest checksum, cleanup lease, status, retry time and confirmation time. Task lifecycle updates register both the previous completed attempt and the new completed attempt in the same transaction. Registration uses insert-on-conflict-do-nothing; it never resets an existing deadline.

Startup/minute polling backfills provable current completed CVM attempts, including soft-deleted tasks. Historic overwritten attempts without a reliable completion timestamp are not guessed or automatically deleted. Local/Slurm/LSF storage is outside this policy.

## Deletion safety

The worker runs every minute, processes at most ten due attempts per cycle, and claims each job with a five-minute lease. Each operation has a two-minute context. It resolves only final `bam` and paired `bai` references from the unique `outputs.resolved.json` inside the exact organization/task/attempt prefix. Ambiguous names, unpaired declared indices, invalid scope and missing manifests stop the deletion.

Before deletion, the exact target keys, size, ETag and canonical manifest SHA256 are persisted. All remaining objects are checked before the first delete; each object is checked again immediately before its delete. Partial cleanup resumes from that fixed plan, allowing already absent objects. Changed manifests or object identities stop retries rather than expanding the scope.

Each DeleteObject is followed by HEAD. Only an explicit not-found response confirms absence; 403, network errors and timeouts are failures. Per-object confirmations and final status are saved in append-only `bam_retention_events`. Failures retry after one hour; crashes leave a recoverable lease. No credits are charged. This implementation removes current objects; an externally enabled bucket version-history policy must separately govern any noncurrent versions.

Parquet, VCF/TBI, BED, CNR, QC, other reports, raw result ZIPs, result datasets and interpretation overlays are retained. No bucket-wide lifecycle or wildcard prefix deletion is used.

## Access cutoff

At the deadline, new BAM quotes/links and IGV BAM/index authorizations stop even if a cleanup retry is pending. New BAM downloads are valid for at most three hours and never beyond the retention deadline. Existing quotes are clamped on issuance and re-issuance before any billing. IGV signatures are similarly clamped. ZIP/VCF/CNR access remains available.

The downloads catalog exposes `bam_retention_days`, `bam_expires_at` and `bam_status` (`available`, `expired`, `cleanup_pending`, `deleted`). `deleted` requires the worker's confirmed absence status; expiry alone never claims deletion. IGV retains unavailable BAM descriptors with an explicit reason.

Previously issued links cannot be recalled purely by changing the application's policy; object deletion ends their storage access. Future grants are capped to the deadline.

## Operations

Deploy first with cleanup disabled, review exact targets, then enable cleanup. The command does not run migrations, task queues or billing:

```sh
octopus bam-retention --task TASK_UUID --attempt ATTEMPT_UUID
octopus bam-retention --task TASK_UUID --attempt ATTEMPT_UUID --execute
```

Default inspection makes no database or storage mutations. Execute requires cleanup enabled, an already registered completion, and an expired deadline. It is fenced to one durable attempt, including a previous attempt whose task has since been retried. Keep output in a restricted operator audit directory; it contains archive object identities but no signed URLs or credentials.

Inspect `bam_retention_jobs.status`, `error_code`, `next_retry_at` and `bam_retention_events` when cleanup is pending. Do not mark a failed deletion successful manually or replace a persisted plan without investigating the archive identity.
