# Go result engine migration

## First release and acceptance window

First-stage commit `3911b63` retains independent `RESULT_ENGINE_BACKEND` and
`SVC_ENGINE_BACKEND` switches. Application defaults remain Python during this
stage; the deployment override explicitly selects Go. There is no error-driven
fallback. The current pinned Python rollback image is built separately and kept
available, including the SVC adapter absent from the previous running image.

Before deleting the legacy runtime, require at least 100 authenticated read-only
acceptance requests, both existing reference tasks, available table types,
sorting/filtering/pagination, CSV export, schema and scoring previews, concurrent
ordinary API measurements, and unchanged clinical adjustment/report snapshot
digests. Elapsed time alone does not satisfy this window. Automated acceptance
traffic is identified as such; it is not a claim of having observed a business peak.

Rollback during this stage: set both switches to `python` in the deployment
environment, then recreate Octopus with the retained Compose override and pinned
Python rollback image. Assessment storage is shared read/write with group 1000.
Do not rebuild COS objects or reset assessments when rolling back.

## Resource controls

Octopus memory limit: 1 GiB; `GOMEMLIMIT=768MiB`. Two result operations can be
active; admission waits at most one second and honours cancellation. Export
leases remain held until response closure. Scan batches are 1,000 rows, sort
runs 4 MiB/4,000 entries, merge fan-in 32; small interactive pages use a bounded
heap. Each request caps simultaneous spill disk at 4 GiB and completed CSV at
1 GiB (matching the existing public response limit). Failed/cancelled spill and
CSV files are removed; successful exported files are removed on response closure.

Cache and assessment directories must be writable by Octopus UID/GID 1000;
temporary files reside below `RESULT_ENGINE_TEMP_DIR`. Preserve cache objects,
assessment JSONL files, report snapshots and COS objects throughout migration.

## Final removal

Only after acceptance passes: remove the legacy client, both migration selectors,
`PARQUET_QUERY_URL`, Python service/network/dependency and Python Dockerfile.
Keep Python source, pinned vendor tree and tests exclusively as development tools.
Retain the first-stage Git commit, exact images and saved environment/Compose
files for rollback. Image cleanup excludes running/stopped-container references
and named rollback images; never prune volumes or unrelated services.
