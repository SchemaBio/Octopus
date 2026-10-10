# Result engine migration performance acceptance

Measured on the existing CVM and running Python/DuckDB 1.5.5 container on 2026-10-10, using deterministic synthetic SNV rows (no patient data). Each operation starts a fresh interpreter; measurements include reading and hashing the fixture before the timed operation, as in the benchmark script's process peak RSS. Raw measurements are in `resultengine-legacy-benchmark.json`.

| Rows | Query | Numeric sort | Filter | Sorted CSV | Automatic preparation |
| --- | --- | --- | --- | --- | --- |
| 10,000 | 0.032 s | 0.067 s | 0.042 s | 0.376 s | 0.208 s |
| 100,000 | 0.043 s | 0.314 s | 0.111 s | 2.697 s | 1.698 s |
| 1,000,000 | 0.048 s | 5.456 s | 1.368 s | timeout at 30 s | 16.659 s |

The production container cannot spill to its default `.tmp` directory (permission denied). The million-row sort value above was measured with an explicit writable `/tmp` spill directory; this workaround is confined to the benchmark process. The million-row sorted export still exceeded the old 30-second operation timeout. This failure is part of the baseline, not a successful result.

## Gates set before Go measurements

- Exact result parity is mandatory. No timing or numeric tolerance can waive a classification, missing-state, row identity, ordering, or CSV difference.
- On this CVM, 100k-row filter/sort must complete within 3 seconds and 1M-row filter/sort within 15 seconds; 1M-row preparation within 30 seconds. Sorted 1M-row export must complete within 60 seconds, including spill cleanup. These ceilings allow bounded-memory processing while retaining interactive responsiveness.
- Peak process RSS for a single 1M-row operation must stay below 512 MiB; concurrent result operations must stay within the deployed container budget, with a maximum of two active operations and bounded admission waiting.
- Under two concurrent result operations, ordinary authenticated Octopus API p95 must stay below 500 ms and no more than twice its measured idle baseline. Measure both before accepting production cutover.
- Benchmark dense overlays, cancellation and a deidentified real dataset separately. Synthetic results cannot substitute for the business-chain acceptance gate.

If a gate fails, optimize or change the pure Go reading implementation before cutover. Keep the legacy service available until the full scoring, query, integration and resource gates pass.
