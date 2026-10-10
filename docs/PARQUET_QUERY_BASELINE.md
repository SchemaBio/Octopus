# Python query baseline

The measured legacy runtime is DuckDB 1.5.5 on the existing 2 vCPU CVM.
Raw timings and resource measurements: [baseline JSON](plans/resultengine-legacy-benchmark.json).
The limits established before Go measurements are in [performance gates](plans/resultengine-performance.md).

The 1M-row Python sort required a writable spill directory: the production
container's default directory fails with permission denied. Sorted CSV exceeded
the 30-second legacy deadline even after that directory workaround.

Authenticated ordinary API, measured through the local Squid proxy on the same
CVM: idle p95 78.318 ms, 30 samples. The concurrent acceptance ceiling is therefore
156.636 ms as well as the absolute 500 ms ceiling. Public Cloudflare/VPN round-trip
measurements are excluded from this server resource comparison.

The pinned scoring revision is `ef66faff51a265fef7b5c4e6439905f3aa540c46`.
Python source and tests are development oracles; the rule revision is unchanged.
