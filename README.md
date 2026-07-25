# GoForge Chi migration facade

`goforge.dev/gpchi` is a bounded compatibility facade for
`github.com/go-chi/chi/v5` v5.3.1
(`8b258c7bb28f97a5f2a856ff7ef962578fec9215`, MIT).

Registration looks familiar, but `Compile` produces an immutable route
snapshot with structured conflict diagnostics and flat `RouteInfo` metadata.
Exact routes are directly indexed; dynamic routes are parsed once and ordered
deterministically. `Snapshot.Resolve` returns exhaustive matched,
method-mismatch, or missing outcomes. The snapshot is also an ordinary
`net/http.Handler`.

The router and middleware are authored in 14 `.gp` files. `MatchOutcome` is a
Go+ enum, so semantic resolution is exhaustive in authored code while the
generated variants remain usable from ordinary Go. Checked `*_gp.go` files are
reproducible build artifacts:

```sh
go generate ./...
go tool goplus gen -check ./...
```

The Go+-authored `std/http/route` layer adds `Pattern[p]`, `Request[p]`,
`ParamKey[T,p]`, `Handler[p]`, indexed route sets, and capability-indexed
middleware. `Mux.Typed` and `Mux.UseCapability` are the migration bridges.

See `COMPATIBILITY.md`, `PERFORMANCE.md`, and the reproducible
`API_MANIFEST.csv`. The corrected inventory contains 127 consumer-nameable
declarations: all 127 declarations are implemented with zero deferred manifest
rows. Behavioral and performance release gates remain open. The
implementation is independently structured; no upstream implementation source
was copied. `UPSTREAM_LICENSE` preserves Chi's copyright and MIT notice.

## Reproduce the inventory

```sh
upstream_root=$(go env GOMODCACHE)/github.com/go-chi/chi/v5@v5.3.1
go run ./internal/cmd/apimanifest "$upstream_root" API_MANIFEST.csv
```
