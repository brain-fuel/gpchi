# Performance contract

The paired operation dispatches an already-created exact `GET /health` request
through a compiled GoForge snapshot and Chi v5.3.1. Both use the same no-op
handler and response writer.

```sh
go test -run '^TestSnapshotAllocationBudget$' \
  -bench 'Benchmark(SnapshotExactRoute|UpstreamExactRoute)$' \
  -benchmem -count=5
```

Completion requires the slowest GoForge run to be at least twice as fast as
the fastest upstream run and at least 50% fewer allocations.

## Completion measurement

Measured 2026-07-23 on Apple M5 Max (`darwin/arm64`):

| Operation | Five-run range | Bytes/op | Allocations/op |
|---|---:|---:|---:|
| GoForge immutable snapshot | 16.73–17.10 ns | 0 | 0 |
| upstream Chi v5.3.1 | 91.28–91.57 ns | 368 | 2 |

Using the slowest GoForge run and fastest upstream run, exact dispatch is
**5.33x faster** and uses **100% fewer allocations**.
`TestSnapshotAllocationBudget` enforces zero snapshot allocations and verifies
that the paired upstream dispatch allocates. This claim applies to compiled
exact-route dispatch, not every dynamic or mutable compatibility operation.

## Middleware ledger

The accepted `AllowContentType("application/json")` path is a representative
compatibility-middleware baseline:

```sh
go test ./middleware -run '^$' \
  -bench 'BenchmarkContentType(GoForge|Upstream)$' -benchmem -count=5
```

Measured 2026-07-23 on Apple M5 Max (`darwin/arm64`):

| Implementation | Five-run range | Bytes/op | Allocations/op |
|---|---:|---:|---:|
| GoForge | 30.40–33.59 ns | 0 | 0 |
| upstream Chi v5.3.1 | 30.21–30.31 ns | 0 | 0 |

This compatibility path does **not** meet the 2× speed target; both
implementations already allocate nothing. The measurement is a recorded miss,
not evidence for a package-wide speedup. A typed/fused policy API must supply
the optimization opportunity without changing compatibility behavior.

## Dynamic and middleware dispatch

```sh
go test -run '^$' \
  -bench 'Benchmark(SnapshotDynamicRoute|UpstreamDynamicRoute|CompatibilityMiddlewareRoute|UpstreamMiddlewareRoute)$' \
  -benchmem -count=5
```

Measured 2026-07-23 on Apple M5 Max (`darwin/arm64`):

| Operation | GoForge range | Upstream range | GoForge allocs | Upstream allocs |
|---|---:|---:|---:|---:|
| named-parameter snapshot | 131.3–132.0 ns | 157.8–159.2 ns | 2 / 368 B | 4 / 704 B |
| exact route + one global middleware | 81.66–82.01 ns | 90.86–91.11 ns | 2 / 368 B | 2 / 368 B |

The named-parameter path is conservatively **1.19× faster**, uses **50% fewer
allocations**, and uses 47.7% fewer bytes. The middleware path is
conservatively **1.10× faster** with equal allocation cost. Both miss the 2×
speed target, and the middleware path also misses the allocation target.

These results follow removal of temporary path/value slices and pooling of
route contexts. Before that change, the dynamic path measured 197.7–199.1 ns
with 8 allocations, and the middleware path measured 121.9–127.0 ns with 4
allocations.
