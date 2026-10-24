# Trio analysis from a proband sample

Samples store optional `father_sample_internal_id`, `mother_sample_internal_id`,
`father_affected_status` and `mother_affected_status`. Status is blank,
`affected`, `unaffected` or `carrier`. Update requests omit a field to preserve
it and send an empty string to clear it. Parents need not exist when saving.

Internal IDs resolve strictly within the child's sample scope. Once resolved,
the stored parent UUID survives renaming. Deletion/expiry makes the relationship
unavailable; it does not bind to a replacement with the same name. Clear and
save the parent field before explicitly choosing a replacement. Cycles and
identical parents are rejected. Parent sex inconsistencies are warnings.

- `POST /api/v1/samples/resolve-parents`: read-only lookup. Body contains optional
  `sample_id`/`internal_id` and the two parent internal IDs. Returns `father` and
  `mother` with `state`, public identity, sex, data readiness and warnings.
- `GET /api/v1/samples/:id/trio-context`: validates the trio and its six distinct,
  completed, unexpired FASTQ assets. Returns readiness, members, errors,
  warnings and estimated minutes. No task or billing record is created.
- `GET /api/v1/samples?analysis_type=trio`: paginated candidates with both parent
  fields configured. Complete input readiness is checked through trio-context.
- Trio estimate/create requests use `sampleId` as the proband. Legacy
  `pedigreeId` requests remain supported. Batch rows accept a proband sample
  identifier; the pedigree column is optional for new templates.

New tasks persist the three members and asset identities in their input
snapshot. Task responses expose `trio_members` without storage paths. Existing
tasks execute/retry from their saved inputs rather than current sample links.
The member order remains proband, father, mother. The proband is affected;
parent carrier/blank states encode PED phenotype 0 and remain distinct in the
snapshot. Affected and unaffected encode 2 and 1. Estimates use total bytes of
all six input assets with the existing runtime estimation formula.

Startup adds the sample columns through the existing additive migration. It
backfills only unambiguous, scope-compatible legacy pedigrees into samples
whose parent configuration is entirely blank. Conflicts are logged by sample
UUID. The `parents_migrated` marker prevents repeated startup from restoring
relationships subsequently cleared by a user. Historical pedigrees/tasks are
retained. Deploy the compatible backend before the frontend. Rollback can retain
the additive columns and migrated sample values; do not delete business data.
