# Interpretation workspace and paid raw downloads (2026-10-06)

## Behavior

- Interpretation tabs no longer include runtime/status. Old `tab=runtime` links normalize to overview. Task list status is preserved.
- `GET /tasks/:id/downloads` describes the current attempt's manifest-declared outputs.
- `POST .../downloads/prepare` creates an immutable ZIP asynchronously; it never charges. ZIP includes final VCF/index, SNP/InDel, MT, CNV region/exon, MEI, UPD, ROH, STR and QC outputs where present. Original report text is preferred where the Parquet catalogue proves its conversion source; otherwise the immutable archived Parquet remains the report source. Inline resolved QC becomes `reports/QC.json`. Missing outputs are recorded in `download-manifest.json`; no BAM/intermediate/reference/input files are included.
- `POST .../downloads/quote` HEADs the object and snapshots size, ETag, user, organization and attempt. Source IP is audit metadata only. Quote validity is ten minutes.
- `POST .../downloads/issue` prepares exact-object COS STS authorization before charging. Both ZIP and BAM download directly from COS. ZIP costs 1 credit; BAM costs ceil(bytes / 1,000,000,000), minimum 1. Squid prices the billing codes independently and writes its normal ledger.
- A confirmed request has a durable three-hour deadline. Retries use `download:<quote UUID>` as the idempotent reference. Recovering a paid request retains its deadline and costs nothing extra. Explicit new requests charge separately.
- `GET .../downloads/active` allows the same user to recover an unexpired grant after changing networks. Signed URLs/STS credentials are not stored. URLs stay in browser memory, not query strings/local storage.
- Expired cross-service requests with no local charge completion are reconciled every five minutes, using the existing idempotent refund API. Successfully issued requests are excluded.
- Old individual raw export endpoints now return 410. Browser Parquet analysis, filtered exports and IGV evidence remain separate capabilities.

## Security and deployment

- Squid signs `client_ip` in its two-minute identity token. Octopus uses only this verified claim for SaaS requests, not an arbitrary forwarded header or the container address. Older tokens without the claim fail closed for downloads.
- COS STS grants only `GetObject` for one exact object, without an IP condition. Short signatures include `x-cos-traffic-limit` in the signed query. Changing/removing that parameter invalidates the signature. See [current controls](DIRECT_COS_DOWNLOADS_20261010.md) for configuration and pause operations.
- Deploy Squid (new billing codes and signed client IP), then Octopus (tables `result_downloads`, `raw_result_packages`), then YiJian.
- No Agent/system image change is needed for the download feature itself. Missing workflow outputs cannot be recovered from a final manifest that never archived them.

## Read-only evidence

Controlled CLI: `download-preflight --task <UUID> --probe-network`. It does not migrate, create quotes, charge/refund, prepare ZIPs or start a queue/node. It reads at most one byte and prints no credentials/URLs. The optional legacy `--ip` argument is ignored. Current probes verify a direct single-byte GET and rejection when the signed limit is removed.

The following evidence describes the previous IP-bound implementation, before the 2026-10-10 update.

For task 73ac68fd / attempt 5298563b:

- BAM: 5,428,396,048 bytes => **6 credits**.
- STS covers the requested three hours.
- Same-IP single-byte GET: **206**, `bytes 0-0/5428396048`.
- Grant bound to a different IP: **403**.
- Actual SaaS-to-COS traffic uses the internal source IP; binding the SaaS public IP correctly yielded 403. Browser downloads use their own verified public IP.
- Final ZIP prepared from this archive: **9,632,140 bytes**, **11 entries**, including VCF/index and QC. All entry CRC checks passed. Five original text reports and two immutable Parquet reports are included. ZIP preparation and verification charged **0** credits.
- Credits charged during these checks: **0**.
- Existing single archive has an MT report but no declared MT VCF; UPD was not produced. Both remain explicitly unavailable, not synthesized or substituted. A future workflow output change must expose/archive MT VCF for that file to be included.

## Checks

Octopus and Squid Go builds pass; YiJian production build and TypeScript check pass. Existing CSS `focusbutton` and Next middleware deprecation warnings remain. No unit/integration suite or paid user download was executed in this request. UI payment confirmation and real user browser download still require acceptance with an authenticated interpretation account.

## Deployment

Squid `3d153e6` and YiJian `370c7c2` deployed healthy. The Octopus follow-up commit selects original report provenance and versions the ZIP cache. Backup directory: `/home/ubuntu/schema/backups/result-downloads-20261006`. Image tags are persisted; no compute image or instance was changed.
