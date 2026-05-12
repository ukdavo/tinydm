# TinyDM — Performance Benchmarks

This document describes the benchmark suite, explains how to run it, and provides
a template for recording baseline results so regressions can be spotted over time.

---

## Overview

Benchmarks are written using Go's built-in `testing.B` framework and live
alongside the production code they exercise:

| Package | File | What is measured |
|---------|------|-----------------|
| `internal/auth` | `bench_test.go` | Password hashing (bcrypt), JWT issuance & validation, API key generation & hashing |
| `internal/storage` | `bench_test.go` | File write (`Put`) and read (`Get`) throughput; deduplication fast-path |
| `internal/api` | `bench_test.go` | Full HTTP round-trips: login, document upload, document list, document download |

---

## Running the benchmarks

### All benchmarks (recommended)

```bash
make bench
```

This runs:

```bash
go test ./... -bench=. -benchmem -benchtime=3s -run='^$'
```

- `-bench=.` — run every benchmark function
- `-benchmem` — report heap allocations per operation
- `-benchtime=3s` — collect at least 3 seconds of samples per benchmark (more stable than the default 1s)
- `-run='^$'` — skip all unit tests (benchmark-only pass)

### Single package

```bash
go test ./internal/auth/... -bench=. -benchmem -benchtime=3s -run='^$'
go test ./internal/storage/... -bench=. -benchmem -benchtime=3s -run='^$'
go test ./internal/api/... -bench=. -benchmem -benchtime=3s -run='^$'
```

### Single benchmark

```bash
go test ./internal/auth/... -bench=BenchmarkNewJWT -benchmem -benchtime=5s -run='^$'
```

### Comparing two commits (requires `benchstat`)

```bash
# Install benchstat once
go install golang.org/x/perf/cmd/benchstat@latest

# Capture baseline (e.g. on main)
git checkout main
go test ./... -bench=. -benchmem -benchtime=3s -run='^$' | tee bench-main.txt

# Capture candidate (e.g. on a feature branch)
git checkout my-branch
go test ./... -bench=. -benchmem -benchtime=3s -run='^$' | tee bench-branch.txt

# Compare
benchstat bench-main.txt bench-branch.txt
```

---

## Benchmark descriptions

### `internal/auth`

| Benchmark | What it measures | Notes |
|-----------|-----------------|-------|
| `BenchmarkHashPassword_DefaultCost` | bcrypt at cost 12 (production default) | Expect ~100–300 ms/op; varies heavily by CPU |
| `BenchmarkHashPassword_MinCost` | bcrypt at cost 4 (minimum) | Useful for isolating CPU scaling from I/O |
| `BenchmarkCheckPassword` | bcrypt comparison | Same cost constraint as `MinCost` variant |
| `BenchmarkNewJWT` | HMAC-SHA256 JWT signing | Should be sub-microsecond |
| `BenchmarkParseJWT` | HMAC-SHA256 JWT verification + claims decode | Should be sub-microsecond |
| `BenchmarkGenerateAPIKey` | CSPRNG key generation + SHA-256 hash | Measures `crypto/rand` + hash cost |
| `BenchmarkHashAPIKey` | SHA-256 of an opaque API key string | Hot path on every API-key-authenticated request |

> **Note:** `TestMain` in `internal/auth` sets `BCryptCost = bcrypt.MinCost` for all
> `_test` builds. `BenchmarkHashPassword_DefaultCost` explicitly overrides this back
> to 12 so the production cost is still observable.

### `internal/storage`

| Benchmark | What it measures | Notes |
|-----------|-----------------|-------|
| `BenchmarkPut/1KB` | Write 1 KB unique content | Includes SHA-256 + disk write |
| `BenchmarkPut/64KB` | Write 64 KB unique content | |
| `BenchmarkPut/1MB` | Write 1 MB unique content | |
| `BenchmarkPut/16MB` | Write 16 MB unique content | |
| `BenchmarkGet/1KB` | Open + close a 1 KB stored file | Body not fully read (open cost only) |
| `BenchmarkGet/64KB` | Open + close a 64 KB stored file | |
| `BenchmarkGet/1MB` | Open + close a 1 MB stored file | |
| `BenchmarkGet/16MB` | Open + close a 16 MB stored file | |
| `BenchmarkPut_Dedup` | Repeated write of identical 1 MB content | Measures dedup fast-path (stat + early return) |
| `BenchmarkPut_sizes/size=1KB` … | Same as `BenchmarkPut` in table form | Useful with `-benchmem` for side-by-side comparison |

### `internal/api`

All HTTP benchmarks spin up a full `httptest.Server` backed by an in-memory
SQLite database. They measure end-to-end latency including routing, auth
middleware, database queries, and file I/O.

| Benchmark | What it measures | Notes |
|-----------|-----------------|-------|
| `BenchmarkLogin` | `POST /api/v1/auth/login` | bcrypt at MinCost in test builds |
| `BenchmarkDocumentUpload/1KB` | Multipart upload of a 1 KB file | Includes MIME sniff, SHA-256, disk write, DB insert |
| `BenchmarkDocumentUpload/64KB` | Multipart upload of a 64 KB file | |
| `BenchmarkDocumentUpload/1MB` | Multipart upload of a 1 MB file | |
| `BenchmarkDocumentList/10docs` | `GET .../documents` with 10 rows | |
| `BenchmarkDocumentList/100docs` | `GET .../documents` with 100 rows | |
| `BenchmarkDocumentDownload/1KB` | Full streaming download of a 1 KB file | Body fully drained |
| `BenchmarkDocumentDownload/1MB` | Full streaming download of a 1 MB file | |

---

## Baseline results template

Run `make bench` and paste the output here whenever a significant change is made.
Record the Go version, OS, and hardware so numbers are comparable.

```
Date:        YYYY-MM-DD
Go version:  go version go1.XX.Y <os/arch>
OS/hardware: <e.g. macOS 15 / Apple M3 Pro>
Commit:      <git short hash>

goos: <darwin|linux|windows>
goarch: <amd64|arm64>

--- internal/auth ---
BenchmarkHashPassword_DefaultCost-N      <n>    <ns/op>    <B/op>    <allocs/op>
BenchmarkHashPassword_MinCost-N          <n>    <ns/op>    <B/op>    <allocs/op>
BenchmarkCheckPassword-N                 <n>    <ns/op>    <B/op>    <allocs/op>
BenchmarkNewJWT-N                        <n>    <ns/op>    <B/op>    <allocs/op>
BenchmarkParseJWT-N                      <n>    <ns/op>    <B/op>    <allocs/op>
BenchmarkGenerateAPIKey-N                <n>    <ns/op>    <B/op>    <allocs/op>
BenchmarkHashAPIKey-N                    <n>    <ns/op>    <B/op>    <allocs/op>

--- internal/storage ---
BenchmarkPut/1KB-N                       <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkPut/64KB-N                      <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkPut/1MB-N                       <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkPut/16MB-N                      <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkGet/1KB-N                       <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkGet/64KB-N                      <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkGet/1MB-N                       <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkGet/16MB-N                      <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkPut_Dedup-N                     <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>

--- internal/api ---
BenchmarkLogin-N                         <n>    <ns/op>    <B/op>    <allocs/op>
BenchmarkDocumentUpload/1KB-N            <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkDocumentUpload/64KB-N           <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkDocumentUpload/1MB-N            <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkDocumentList/10docs-N           <n>    <ns/op>    <B/op>    <allocs/op>
BenchmarkDocumentList/100docs-N          <n>    <ns/op>    <B/op>    <allocs/op>
BenchmarkDocumentDownload/1KB-N          <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
BenchmarkDocumentDownload/1MB-N          <n>    <ns/op>    <MB/s>    <B/op>    <allocs/op>
```

---

## Interpreting results

**`ns/op`** — nanoseconds per benchmark iteration. Lower is better.

**`MB/s`** — throughput reported when `b.SetBytes` is called (storage and upload/download benchmarks). Higher is better.

**`B/op`** — heap bytes allocated per iteration. Lower is better; zero allocation is ideal for hot paths.

**`allocs/op`** — number of heap allocations per iteration. Each allocation adds GC pressure. Aim to reduce this on critical paths.

### Red flags

- `BenchmarkHashPassword_DefaultCost` drops below 50 ms/op — check that `BCryptCost` wasn't accidentally left at `MinCost` in a production build.
- `BenchmarkDocumentDownload` allocations increase significantly — a new middleware or response wrapper may be copying the body unnecessarily.
- `BenchmarkDocumentList/100docs` latency spikes — check for N+1 query patterns introduced in store changes.
- Any benchmark regresses by more than 10% — run `benchstat` against the previous baseline to confirm and investigate.
