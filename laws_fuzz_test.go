package chi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	upstream "github.com/go-chi/chi/v5"
	stdroute "goforge.dev/goplus/std/http/route"
)

func FuzzPatternParser(f *testing.F) {
	for _, seed := range []string{"/", "/health", "/users/{id}", "/files/*", `/orders/{id:[0-9]+}`, "bad", "/{x}/{x}"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, pattern string) {
		segments, names, err := parsePattern(pattern)
		if err != nil {
			return
		}
		if len(pattern) == 0 || pattern[0] != '/' {
			t.Fatalf("accepted malformed pattern %q", pattern)
		}
		if len(names) > len(segments) {
			t.Fatalf("names=%d segments=%d", len(names), len(segments))
		}
		for _, name := range names {
			if name == "" {
				t.Fatal("empty parameter name")
			}
		}
	})
}

func FuzzParameterRouteDifferential(f *testing.F) {
	for _, seed := range []string{"alice", "42", "a-b", "hello%20world"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if value == "" || len(value) > 128 || strings.Contains(value, "/") {
			t.Skip()
		}
		path := "/users/" + url.PathEscape(value)
		ours, theirs := NewRouter(), upstream.NewRouter()
		ours.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, URLParam(r, "id")) })
		theirs.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, upstream.URLParam(r, "id")) })
		gotCode, gotBody := exercise(ours, http.MethodGet, path)
		wantCode, wantBody := exercise(theirs, http.MethodGet, path)
		if gotCode != wantCode || gotBody != wantBody {
			t.Fatalf("%q = (%d,%q), upstream (%d,%q)", path, gotCode, gotBody, wantCode, wantBody)
		}
	})
}

func TestCompiledSnapshotConcurrentServingAndRegistrationRaceFree(t *testing.T) {
	router := NewRouter()
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	snapshot, conflicts := router.Compile()
	if len(conflicts) != 0 {
		t.Fatal(conflicts)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				recorder := httptest.NewRecorder()
				snapshot.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
				if recorder.Code != http.StatusNoContent {
					t.Errorf("status=%d", recorder.Code)
					return
				}
				router.Get(fmt.Sprintf("/dynamic/%d/%d", id, i), func(http.ResponseWriter, *http.Request) {})
			}
		}(worker)
	}
	wg.Wait()
	if code, _ := exercise(snapshot, http.MethodGet, "/health"); code != http.StatusNoContent {
		t.Fatalf("published snapshot changed: %d", code)
	}
}

func TestMetadataAndRoutesDoNotExposeSnapshotStorage(t *testing.T) {
	router := NewRouter()
	router.Get("/users/{id}", func(http.ResponseWriter, *http.Request) {})
	snapshot, conflicts := router.Compile()
	if len(conflicts) != 0 {
		t.Fatal(conflicts)
	}
	metadata := snapshot.Metadata()
	metadata[0].ParamNames[0] = "mutated"
	if snapshot.Metadata()[0].ParamNames[0] != "id" {
		t.Fatal("metadata alias")
	}
	routes := snapshot.Routes()
	routes[0].Handlers[http.MethodGet] = nil
	if snapshot.Routes()[0].Handlers[http.MethodGet] == nil {
		t.Fatal("route handler map alias")
	}
}

func TestCapabilityMiddlewareBridge(t *testing.T) {
	router := NewRouter()
	middleware := stdroute.NewMiddleware(stdroute.WriteHeadersID(), stdroute.WriteHeaders{}, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Capability", "headers")
			next.ServeHTTP(w, r)
		})
	})
	router.UseCapability(middleware)
	router.Get("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Header().Get("X-Capability") != "headers" {
		t.Fatal("capability middleware not applied")
	}
}
