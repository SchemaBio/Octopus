# Direct COS download controls — 2026-10-10

ZIP and BAM now use exact-object COS STS links; neither transfers through Octopus/Squid. User, organization, task, execution attempt, original paid deadline, ETag and billing idempotency remain enforced. Public IP is audit information only, including for existing paid grants. No Agent or CVM system image change is needed.

| Setting | Default | Meaning |
| --- | --- | --- |
| Paid grant | 3 hours | Existing immutable deadline; BAM retention may shorten it |
| `RESULT_DOWNLOAD_LINK_TTL` | `30m` | Each URL expires no later than the paid deadline |
| `RESULT_DOWNLOAD_REFRESH_INTERVAL` | `60s` | Minimum interval between successful issuances |
| `RESULT_DOWNLOAD_MAX_ISSUES` | `12` | Total URLs per grant, including the first; counters persist across restarts |
| `RESULT_DOWNLOAD_TRAFFIC_LIMIT_BPS` | `83886080` | 10 MiB/s per COS request; value is signed |
| `RESULT_DOWNLOAD_PAUSE_FILE` | `/data/archive/.downloads-paused` | Existing file blocks quotes and all new URL issuances immediately |

`expires_at` is the short URL deadline; `grant_expires_at` is the paid grant deadline. Responses also include `refresh_after`, `remaining_issues`, `traffic_limit_bps` and `ip_bound=false`. Refreshing the same grant does not charge again, extend the grant or bypass counters. The UI disables expired links, keeps the grant visible for free refresh, and shows both deadlines and remaining issuances. A lost response can be retried after the cooldown using the original grant ID. Explicit new grants charge separately.

Old ZIP proxy endpoints no longer stream data; they ask the user to refresh and retrieve the existing grant. Existing paid grants start their new issuance counter at zero after migration. Previously generated IP-bound URLs remain valid under their original policy until they expire; users should retrieve a replacement from their existing grant rather than pay again.

## Monitoring and immediate pause

Use COS's existing bucket monitoring for actual public outbound bytes and request counts. Application `download_link_issued` logs identify issued grants and counts without URLs or credentials; they are not a record of actual bytes downloaded. No new paid service or external notification automation is installed. A budget threshold and notification recipients have not been supplied, so no automatic budget alarm/cutoff is claimed.

Pause immediately when monitoring shows abnormal egress:

```sh
docker exec schemabio-saas-octopus-1 touch /data/archive/.downloads-paused
```

Resume after investigation:

```sh
docker exec schemabio-saas-octopus-1 rm /data/archive/.downloads-paused
```

The marker is in the existing persistent archive volume and requires no restart. During a pause, signed URLs already handed out remain usable until expiration; active transfers may continue. Do not delete task evidence or alter tenant/bucket policies to stop a single user's traffic.

## Limits

These controls reduce exposure, not provide a hard aggregate byte quota. A URL can be replayed/shared before expiry; parallel requests each have their own rate limit. The rate limit does not apply to separate IGV/browser evidence URLs. Revoking/minting shorter links does not cancel old links. No automatic traffic budget cutoff has been added. Clients must reuse their partial local file with the refreshed URL for Range continuation; a browser may restart a download. The original paid window still ends even if a transfer was incomplete.

Tencent references: [single-request rate limiting](https://intl.cloud.tencent.com/zh/document/product/436/34072), [COS quota/monitoring center](https://cloud.tencent.com/document/product/436/126913).

## Validation and deployment

Go regressions cover paid-grant reuse after IP changes for ZIP and BAM with zero billing calls, fixed grant/link deadlines, cooldown/count exhaustion, refunded/expired grants, pause/resume, exact-object GetObject STS scope and signed rate limits. Frontend regressions cover direct COS anchors for both formats, expired-link refresh with the original grant ID, and absence of server-side ZIP downloads.

Deploy updated Octopus before YiJian; GORM adds `last_issued_at` and `issue_count` to the existing downloads table. Retain the previous images and Compose/environment backups. Run the separately built `download-preflight` CLI against a completed task to verify real COS Range behavior and rejection of modified limits without charging credits.

On 2026-10-10, deployed images `schemabio/octopus:direct-cos-downloads-20261010` and `schemabio/yijian:direct-cos-downloads-20261010`; both containers are healthy. Source/config backups, build logs and validation metadata (without signed URLs) are in `/home/ubuntu/schema/backups/direct-cos-downloads-20261010/`. Environment backups contain credentials and must stay private.

`go test -p 1 ./...` passed. YiJian typecheck passed; 189 tests passed and one existing performance test was skipped. Both production Docker builds passed.

Real COS checks for task `3987e24e-4eb6-4ce0-916e-198771b04bda` returned 206 (`bytes 0-0/5542312460`); removing the signed rate limit returned 403. Reused the existing paid grant `bf5d2a3f-91f0-475e-b1cd-a9dddd07c3ee` with a different audit IP: active/issue APIs returned 200, the direct COS link returned 206, and an immediate repeated issue returned 400. The original charged timestamp and paid deadline remained unchanged; the persistent issue count advanced from 0 to 1. No new quote, charge, package build or workflow was requested. The diagnostic link was discarded, so the user should retrieve the existing paid grant in the UI; this consumes one further allowed issuance but incurs no new fee.
