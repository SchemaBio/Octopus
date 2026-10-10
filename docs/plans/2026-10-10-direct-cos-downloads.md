# Direct COS downloads with bounded authorization

**Goal:** Keep files off the application server data path and reduce reusable-link exposure without IP binding or new paid cloud services.

**Architecture:** Preserve the original three-hour paid grant and billing idempotency key. Issue exact-object, GetObject-only STS URLs lasting at most 30 minutes, sign a 10 MiB/s traffic limit, and serialize issuance counters (12 URLs per grant; at least 60 seconds apart) using the existing PostgreSQL row lock. Both ZIP and BAM use direct COS links. Keep source IP only as audit metadata. Administrators can create a pause marker in the existing archive volume to stop new signatures immediately; COS metrics remain the source of actual egress accounting.

**Tech Stack:** Go/GORM/PostgreSQL, COS STS, React/Next.js.

1. Add validated settings and persistent issuance fields; isolate validity/counter checks for regression tests.
2. Remove IP checks on paid-grant reuse and replace the IP-conditioned STS policy. Include traffic limit in canonical query and signed parameter list; test policy scope and signature tamper detection.
3. Return distinct link and grant deadlines. Convert ZIP to direct links, expose free refresh and remaining issue count, and remove IP-bound UI text.
4. Verify billing idempotency, expiry, pause/counters, frontend tests/typecheck and real COS single-byte reads; changed/removed limit must fail. Use an existing paid grant to verify reuse without new billing.
5. Deploy only Octopus/YiJian, retaining previous images/configuration. Document limits, monitoring and pause/recovery. Commit changes for user-managed push.

This bounds signing opportunities, not actual downloaded bytes. Parallel/replayed COS requests can still multiply traffic. Short links do not revoke older issued URLs or force an already accepted request to stop. No claim of a strict byte quota or automated COS budget cutoff is made.
