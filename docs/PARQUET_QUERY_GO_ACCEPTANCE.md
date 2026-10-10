# Go engine acceptance — 2026-10-10

## Exact compatibility

- 623 pinned upstream function vectors, 193 full scoring requests, 375 input
  validation/public-JSON-boundary cases; classification/VUS/missing status,
  score components, caps, exclusivity and calculation provenance agree exactly.
- 93 query/preparation/export vectors: compression, empty schema, absent fields,
  null versus zero, 64-bit integer precision, multi-value annotations, overlays,
  stable pagination, numeric/natural chromosome order and byte-exact CSV.
- Wide-table oracle: 55,041 rows and 46 columns generated from production schema,
  mean string lengths and null ratios. **Every cell is synthetic**, including
  genetic and numerical fields; no production rows were copied. Seven query
  cases and a 33,280,237-byte sorted CSV agree exactly, including SHA-256.
- Full Go suite passed; race checks passed for resultengine, svcv4 and service.
  Tests exercise 100,000 overlays, large offsets, a >4 MiB heap fallback, >32
  merge runs, corrupt pages/footer/hash, cancellation and failed spill cleanup.
- Python fixed source: 515 passed. YiJian typecheck passed; Vitest: 195 passed,
  1 skipped. Existing browser-local evaluation, independent ACMG versions,
  selection/audit/reasons, readonly, conflicts, idempotency and report snapshot
  regression tests remain in their existing suites. No frontend code change.

## Same-CVM measurements

| Rows | Query | Numeric sort | Filter | Sorted CSV | Cold preparation |
| --- | --- | --- | --- | --- | --- |
| 100,000 | 0.145 s | 0.695 s | 0.682 s | 1.903 s | see raw warm-cache result |
| 1,000,000 | 0.264 s | 6.607 s | 6.824 s | 20.558 s | 14.697 s |
| 55,041 / 46 columns | 0.307 s | 1.528 s | 1.644 s | 2.504 s | 2.046 s |

The million-row export peaked at 147,656 KiB RSS; cold preparation at 135,680
KiB. All predeclared ceilings passed. Warm preparation measurements are
explicitly labelled in raw data and are not used as cold-start evidence.
Go filtering is slower than DuckDB on the flat fixture; Go is not claimed to be
universally faster. Bounded memory and removal of a runtime are the migration goals.

Local microbenchmarks on Windows: Query 2.34 ms, 280,098 B / 2,188 allocations;
Sort 3.51 ms, 294,741 B / 2,338 allocations. These tiny-fixture measurements are
not substitutes for the same-CVM large-data measurements.

## First-stage business gate

The retained-Python stage completed 138 authenticated acceptance requests on
tasks `3987e24e-4eb6-4ce0-916e-198771b04bda` and
`fc5ebe5d-1a87-4901-864d-6b75428ef5ae`. All seven available table types, including
empty ROH, loaded/sorted; both 55,459-row SNV datasets exported successfully.
SVC schema pin and valid zero-score VUS-low previews passed.

With two Go numeric sorts active, ordinary API p50/p95/max were
31.82/42.34/50.24 ms across 40 samples, below the predeclared 156.636 ms p95 gate.
Octopus cgroup peak: 212,410,368 bytes; 1 GiB limit; no OOM.
The digest of all 11 manual adjustment records was unchanged. There were zero
stored reports/report-generation snapshots on this deployment; historical
snapshot immutability is covered by repository regression tests, not a fabricated
production snapshot check. No assessment save or report generation was performed.

This was controlled acceptance traffic, not observation of an actual business
peak. The initial request-based release gate passed before runtime removal.
Raw, non-clinical evidence lives in [acceptance data](plans/resultengine-go-acceptance.json).

## Final Go-only deployment

Runtime image: `schemabio/octopus:go-only-6ad61a1`, SHA-256
`1eec470df3eb2609c86e700e2953357abc3ea426d001be90848788c541a84012`.
Octopus is healthy, statically built with CGO disabled. The runtime image has no
Python or DuckDB executable; the Python service, dedicated network, HTTP client,
selectors and runtime Dockerfile have been removed. Cache/assessment mounts are
writable by UID/GID 1000. Default and Sepiida networks remain attached.

After removing the Python container, 58 additional authenticated requests passed
across both tasks and all seven available tables, schema/scoring and complete
CSV exports. Both CSV byte counts and hashes matched the first-stage release.
Two concurrent numeric queries: ordinary API p95 38.97 ms (40 samples).
Final container peak 159,703,040 bytes, no OOM; no temporary outputs remained.
The 11 manual adjustment digests remained identical to the pre-release values.

97 unused SaaS image entries were deleted without force. All running/stopped
container references and five named rollback tags were preserved; no volumes,
COS objects or unrelated services were deleted. Measured filesystem recovery
was 237,613,056 bytes; shared build layers/caches were not globally pruned.
First-stage rollback instructions are in [migration](PARQUET_QUERY_GO_MIGRATION.md).

The attempted partly redacted fixture was rejected by automatic approval review
because it would have retained genetic values. It was not created. Instead,
`scripts/resultengine_oracle/shape.py` reads only aggregate shape statistics and
generates every cell synthetically. Actual existing tasks were also exercised
through their authorised read-only APIs, with no clinical content in this report.
