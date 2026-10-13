# Minibwa Reference Priority Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make minibwa the preferred aligner and reference index set for CNV baseline calibration and database initialization, while retaining a complete standard BWA fallback and precise failure diagnostics.

**Architecture:** Octopus validates the shared FASTA/FAI/CNV reference and accepts a complete minibwa index pair before considering BWA. Squid initializes that same preferred pair before downloading other baseline resources. Schema_CPU initializes mapping indexes before bulk database synchronization; the WDL selects a usable minibwa installation first and fails clearly if no supported aligner/index combination exists.

**Tech Stack:** Go services and tests, Bash initialization, WDL workflows, Python shell regression tests.

## Tasks

1. Update `internal/service/cvm_reference_preflight.go` and its tests for both genomes: minibwa-only success, BWA fallback, incomplete sets rejected, required CNV reference remains enforced.
2. Update Squid `internal/service/cvm_baseline_bootstrap.go` and tests: download minibwa first, retain BWA fallback, preserve download failure propagation and validate generated shell syntax.
3. Update Schema_CPU `schemabio-server.sh` database initialization and README: prepare minibwa indexes before annotation synchronization, retain resumable full sync, and verify shell behavior with mocked COSCLI.
4. Update schema-germline `tasks/bwamem.wdl`: prioritize minibwa independently of thread count, check runtime/index readiness, validate BWA fallback, and propagate alignment failures through pipes. Verify generated command branches with mocked tools and validate WDL syntax.
5. Update Octopus CVM callback diagnostics and regression tests: a generic terminal callback for the same failed attempt must retain the existing concrete message and reason code; an explicit newer diagnosis replaces them.
6. Run targeted service suites and shell/workflow checks, inspect diffs across all four repositories, and report source changes and any deployment/resource limitations. No production deployment or task resubmission is included without explicit authorization.

## Completion and validation

All implementation tasks are complete. Octopus service/model/workflow tests and Squid service/model tests passed. Bootstrap execution tests cover preferred minibwa, complete BWA fallback, missing indexes and a partial minibwa pair. The eight Python regression tests cover database initialization ordering, runtime selection, unsupported CPU/memory fallback and alignment pipeline failure propagation. Linux miniwdl 1.15.0 validated `tasks/bwamem.wdl`, `baseline_fix.wdl`, `single.wdl` and `trio.wdl`; Bash syntax and Git whitespace checks passed.

Low-thread alignment tasks now request at least 32 GiB to keep minibwa's memory requirement independent of thread count. Verification used mocked aligners/COSCLI and static WDL validation; no paid analysis was launched. Source changes are not committed or deployed. The separately observed missing hg38 bundled CNV reference in COS remains a resource issue; its mandatory validation has not been bypassed.
