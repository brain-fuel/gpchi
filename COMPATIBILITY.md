# Compatibility contract

The pinned baseline is Chi v5.3.1. `API_MANIFEST.csv` inventories 127
consumer-nameable public declarations across the root and middleware package.
All 127 declarations are implemented; the generated manifest has zero deferred
rows.

The inventory excludes `_examples` and exported methods on unexported receiver
types. Example programs are conformance fixtures, not importable public API,
and consumers cannot name private receiver types.

## Supported behavior

- `net/http.Handler` interoperability and standard method helpers.
- Static, `{name}`, `{name:regexp}`, `*`, and `{name:*}` patterns, plus
  embedded/composite parameters such as `{id}.json`, `{id}:{op}`, and
  regex-constrained literal suffixes.
- Chi-compatible parameter lookup through `URLParam` and `RouteContext`.
- Standard-library `Request.PathValue` population for named and wildcard
  parameters.
- Global and inline middleware, groups, route prefixes, and mounts.
- Custom 404 and 405 handlers; method-aware matching and finding.
- Default 405 `Allow` headers and inherited parent 404/405 behavior across
  mounted routers, with explicit child fallbacks taking precedence.
- Prefix-scoped 404/405 handlers registered inside `Route` subrouters.
- `Chain`, route traversal, route context reset, and custom method registration.
- Middle-wildcard route-pattern normalization and inline middleware metadata
  during `Walk`.
- Immutable compilation, structured duplicate/ambiguity diagnostics, and owned
  flat metadata for documentation generation.
- Exhaustive `Snapshot.Resolve` outcomes for matched routes, method mismatch,
  and missing routes.
- `MethodNotAllowedHandler()` with its externally callable zero-argument
  signature, including configured and default responders.
- Request ID propagation and generation, policy-driven client IP extraction,
  legacy real-IP normalization, panic recovery, and deadline-driven 504
  responses at `goforge.dev/gpchi/middleware`.
- Content type, encoding, and charset policies; Basic authentication; response
  headers; context values; body-size limiting; cache suppression; path
  rewriting; conditional middleware; and Sunset metadata.
- Heartbeats, path cleaning, prefix/slash handling, HEAD-to-GET fallback, and
  URL-format extraction.
- Header routing, page interception, response-writer instrumentation,
  concurrency throttling, profiling endpoints, early not-found suppression,
  gzip/deflate and custom compression, and request logging.

The differential corpus covers static, parameter, regex, wildcard, method,
404/405, grouping, middleware, and real `httptest.Server` cases.

## Explicit differences and release gates

- The declaration-level compatibility surface is complete. Release still
  requires the full differential, race, fuzz, and performance matrix; zero
  deferred rows alone are not treated as behavioral proof.
- `RealIP` is deprecated, as it is upstream: it trusts attacker-controlled
  forwarded headers unless deployment infrastructure overwrites them.
  Capability-indexed trusted-proxy policy is the semantic replacement.
- `Compile` diagnoses ambiguous parameter shapes for checked immutable
  snapshots. The mutable compatibility mux preserves upstream replacement
  behavior: the later handler and parameter name win.
- Dynamic routing uses deterministic specificity ordering rather than exposing
  Chi's private radix-tree structure.
- Global middleware now runs before route selection and on 404/405 paths, with
  a route context already installed. The optimized immutable `Snapshot` API
  retains a context-free exact-route fast path when called directly.
- The typed standard core intentionally accepts static, named, and terminal
  wildcard patterns. Regex patterns remain in the compatibility facade until a
  typed regex/refinement contract is available.

Registration compatibility includes late-middleware panics, supported/custom
method validation, terminal-wildcard validation, empty parameter values,
method-prefixed `Handle` patterns, nested mount path shifting, duplicate/nil
mount panics, mounted route traversal, and escaped URL parameters with encoded
slashes and wildcard tails. Differential fuzzing covers method, route-shape,
escaped-value, and missing-path combinations; its first discovered regression
proved that a terminal slash must not satisfy a final named parameter even
though Chi permits empty internal parameter segments.
