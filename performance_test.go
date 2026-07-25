package chi

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	upstream "github.com/go-chi/chi/v5"
)

type benchmarkWriter struct{ header http.Header }

func (w *benchmarkWriter) Header() http.Header          { return w.header }
func (*benchmarkWriter) Write(body []byte) (int, error) { return len(body), nil }
func (*benchmarkWriter) WriteHeader(int)                {}

var benchmarkCount atomic.Int64

func benchmarkRouters() (*Snapshot, http.Handler, *http.Request, *benchmarkWriter) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { benchmarkCount.Add(1) })
	ours := NewRouter()
	ours.Get("/health", handler)
	snapshot, conflicts := ours.Compile()
	if len(conflicts) != 0 {
		panic(conflicts[0])
	}
	theirs := upstream.NewRouter()
	theirs.Get("/health", handler)
	request := httptest.NewRequest(http.MethodGet, "http://example.test/health", nil)
	return snapshot, theirs, request, &benchmarkWriter{header: make(http.Header)}
}

func BenchmarkSnapshotExactRoute(b *testing.B) {
	ours, _, request, writer := benchmarkRouters()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ours.ServeHTTP(writer, request)
	}
}
func BenchmarkUpstreamExactRoute(b *testing.B) {
	_, theirs, request, writer := benchmarkRouters()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		theirs.ServeHTTP(writer, request)
	}
}

func TestSnapshotAllocationBudget(t *testing.T) {
	ours, theirs, request, writer := benchmarkRouters()
	ourAllocs := testing.AllocsPerRun(1000, func() { ours.ServeHTTP(writer, request) })
	upstreamAllocs := testing.AllocsPerRun(1000, func() { theirs.ServeHTTP(writer, request) })
	if ourAllocs != 0 {
		t.Fatalf("snapshot allocations = %v, want 0", ourAllocs)
	}
	if upstreamAllocs < 1 {
		t.Fatalf("upstream allocations = %v; reduction is not measurable", upstreamAllocs)
	}
}

func benchmarkDynamicRouters() (*Snapshot, http.Handler, *http.Request, *benchmarkWriter) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { benchmarkCount.Add(1) })
	ours := NewRouter()
	ours.Get("/users/{id}", handler)
	snapshot, conflicts := ours.Compile()
	if len(conflicts) != 0 {
		panic(conflicts[0])
	}
	theirs := upstream.NewRouter()
	theirs.Get("/users/{id}", handler)
	request := httptest.NewRequest(http.MethodGet, "http://example.test/users/42", nil)
	return snapshot, theirs, request, &benchmarkWriter{header: make(http.Header)}
}

func BenchmarkSnapshotDynamicRoute(b *testing.B) {
	ours, _, request, writer := benchmarkDynamicRouters()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ours.ServeHTTP(writer, request)
	}
}

func BenchmarkUpstreamDynamicRoute(b *testing.B) {
	_, theirs, request, writer := benchmarkDynamicRouters()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		theirs.ServeHTTP(writer, request)
	}
}

func benchmarkMiddlewareRouters() (http.Handler, http.Handler, *http.Request, *benchmarkWriter) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { benchmarkCount.Add(1) })
	noOp := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
	ours := NewRouter()
	ours.Use(noOp)
	ours.Get("/health", handler)
	theirs := upstream.NewRouter()
	theirs.Use(noOp)
	theirs.Get("/health", handler)
	request := httptest.NewRequest(http.MethodGet, "http://example.test/health", nil)
	return ours, theirs, request, &benchmarkWriter{header: make(http.Header)}
}

func BenchmarkCompatibilityMiddlewareRoute(b *testing.B) {
	ours, _, request, writer := benchmarkMiddlewareRouters()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ours.ServeHTTP(writer, request)
	}
}

func BenchmarkUpstreamMiddlewareRoute(b *testing.B) {
	_, theirs, request, writer := benchmarkMiddlewareRouters()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		theirs.ServeHTTP(writer, request)
	}
}
