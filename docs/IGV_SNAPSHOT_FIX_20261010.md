# IGV snapshot tenant scope fix — 2026-10-10

## Incident

Task `3987e24e-4eb6-4ce0-916e-198771b04bda`, position `X:99657579`, requested a browser screenshot of `chrX:99657479-99657679`. Octopus accepted the PNG and uploaded it to COS, then PostgreSQL rejected the snapshot metadata insert with SQLSTATE `22001`: `value too long for type character varying(36)`. The failure cleanup deleted the uploaded PNG. Both the snapshot table and task snapshot COS prefix were empty when checked.

The model incorrectly defined `IGVSnapshot.TenantID` with capacity 36. Canonical organization scope is `org:<UUID>` (40 characters); task and other tenant-scoped result models use capacity 160. This affected organization screenshot creation generally, independent of genomic position or COS path.

## Fix and deployment

The snapshot tenant column now uses size 160. Tenant identity, access checks, object keys and image generation are unchanged. A PostgreSQL dialect regression test checks that the column accepts a real organization scope and matches the task tenant type.

On the deployment host, built image `schemabio/octopus:igv-tenant-fix-20261010` from the corrected source and changed the deployment's `OCTOPUS_IMAGE`. Recreated only Octopus using the existing Compose files. Startup AutoMigrate widened the live `igv_snapshots.tenant_id` to `varchar(160)`; the container is healthy. The previous image is retained. Source and Compose/environment backups plus the build log are at `/home/ubuntu/schema/backups/igv-snapshot-tenant-20261010/`; the environment backup contains secrets and must stay private.

## Validation

- `go test -p 1 ./internal/model ./internal/service ./internal/handler` passed.
- The production column reports capacity 160 after startup migration.
- A production metadata insert using this task's real 40-character tenant scope is checked inside a transaction that is rolled back; no synthetic screenshot or persistent test record is created.
- A real browser screenshot still needs a retry at the original position to verify the full browser-to-COS chain. Failed previous screenshots cannot be recovered because their PNGs were cleaned up; refreshing and reopening the locus regenerates them while BAM is available.

Deploy the corrected source/image for future restarts. Running the old model through AutoMigrate can attempt to restore its incorrect size 36; widening the database alone is not a durable source fix.
