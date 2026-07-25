package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	upchi "github.com/go-chi/chi/v5"
	upstream "github.com/go-chi/chi/v5/middleware"
	actualchi "goforge.dev/gpchi"
	actual "goforge.dev/gpchi/middleware"
)

func TestProfilerDifferential(t *testing.T) {
	tests := []struct {
		path       string
		wantStatus int
		bodyPart   string
	}{
		{"/", http.StatusMovedPermanently, ""},
		{"/pprof/", http.StatusOK, "profile"},
		{"/pprof/goroutine?debug=1", http.StatusOK, "goroutine profile"},
		{"/vars", http.StatusOK, "cmdline"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			run := func(handler http.Handler) *httptest.ResponseRecorder {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
				return recorder
			}
			got, want := run(actual.Profiler()), run(upstream.Profiler())
			if got.Code != want.Code || got.Code != tc.wantStatus {
				t.Fatalf("status = %d, upstream %d, want %d", got.Code, want.Code, tc.wantStatus)
			}
			if tc.bodyPart != "" && (!strings.Contains(got.Body.String(), tc.bodyPart) ||
				!strings.Contains(want.Body.String(), tc.bodyPart)) {
				t.Fatalf("body lacks %q", tc.bodyPart)
			}
			for _, header := range []string{"Cache-Control", "Pragma", "X-Accel-Expires"} {
				if got.Header().Get(header) != want.Header().Get(header) {
					t.Fatalf("%s = %q, upstream %q", header, got.Header().Get(header), want.Header().Get(header))
				}
			}
		})
	}
}

func TestSupressNotFoundDifferential(t *testing.T) {
	actualRouter := actualchi.NewRouter()
	actualRouter.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(418)
		_, _ = io.WriteString(w, "missing")
	})
	actualRouter.Use(actual.SupressNotFound(actualRouter))
	actualRouter.Get("/found", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "found")
	})

	upstreamRouter := upchi.NewRouter()
	upstreamRouter.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(418)
		_, _ = io.WriteString(w, "missing")
	})
	upstreamRouter.Use(upstream.SupressNotFound(upstreamRouter))
	upstreamRouter.Get("/found", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "found")
	})

	for _, path := range []string{"/found", "/missing"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		got, want := httptest.NewRecorder(), httptest.NewRecorder()
		actualRouter.ServeHTTP(got, request)
		upstreamRouter.ServeHTTP(want, request.Clone(request.Context()))
		if got.Code != want.Code || got.Body.String() != want.Body.String() {
			t.Fatalf("%s: response = %d/%q, upstream %d/%q",
				path, got.Code, got.Body.String(), want.Code, want.Body.String())
		}
	}
}
