package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	upstream "github.com/go-chi/chi/v5/middleware"
	actual "goforge.dev/gpchi/middleware"
)

type observation struct {
	status  int
	body    string
	headers http.Header
}

func compareMiddleware(
	t *testing.T,
	makeRequest func() *http.Request,
	actualMiddleware,
	upstreamMiddleware func(http.Handler) http.Handler,
	handler func(http.ResponseWriter, *http.Request),
) {
	t.Helper()
	run := func(middleware func(http.Handler) http.Handler) observation {
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(handler)).ServeHTTP(recorder, makeRequest())
		return observation{recorder.Code, recorder.Body.String(), recorder.Header()}
	}
	got, want := run(actualMiddleware), run(upstreamMiddleware)
	if got.status != want.status || got.body != want.body || !headersEqual(got.headers, want.headers) {
		t.Fatalf("observation = %#v, upstream %#v", got, want)
	}
}

func headersEqual(left, right http.Header) bool {
	if len(left) != len(right) {
		return false
	}
	for key, values := range left {
		if strings.Join(values, "\x00") != strings.Join(right.Values(key), "\x00") {
			return false
		}
	}
	return true
}

func TestContentPolicyDifferential(t *testing.T) {
	tests := []struct {
		name     string
		request  func() *http.Request
		actual   func(http.Handler) http.Handler
		upstream func(http.Handler) http.Handler
	}{
		{
			"content type parameters",
			func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
				r.Header.Set("Content-Type", " Application/JSON ; charset=utf-8")
				return r
			},
			actual.AllowContentType("application/json"),
			upstream.AllowContentType("application/json"),
		},
		{
			"content encoding denied",
			func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("body"))
				r.Header.Set("Content-Encoding", "br")
				return r
			},
			actual.AllowContentEncoding("gzip"),
			upstream.AllowContentEncoding("gzip"),
		},
		{
			"charset accepted",
			func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("body"))
				r.Header.Set("Content-Type", "text/plain; charset=UTF-8")
				return r
			},
			actual.ContentCharset("utf-8"),
			upstream.ContentCharset("utf-8"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compareMiddleware(t, tc.request, tc.actual, tc.upstream, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
			})
		})
	}
}

func TestBasicAuthDifferential(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "accepted"}[authorized], func(t *testing.T) {
			request := func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				if authorized {
					r.SetBasicAuth("alice", "secret")
				} else {
					r.SetBasicAuth("alice", "wrong")
				}
				return r
			}
			credentials := map[string]string{"alice": "secret"}
			compareMiddleware(t, request, actual.BasicAuth("test", credentials), upstream.BasicAuth("test", credentials), func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
		})
	}
}

func TestTransformDifferential(t *testing.T) {
	key := struct{ name string }{"key"}
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name     string
		actual   func(http.Handler) http.Handler
		upstream func(http.Handler) http.Handler
		request  func() *http.Request
		handler  func(http.ResponseWriter, *http.Request)
	}{
		{"set header", actual.SetHeader("X-Test", "yes"), upstream.SetHeader("X-Test", "yes"), getRequest, writeRequestPath},
		{"with value", actual.WithValue(key, "value"), upstream.WithValue(key, "value"), getRequest, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, r.Context().Value(key).(string))
		}},
		{"path rewrite", actual.PathRewrite("/old", "/new"), upstream.PathRewrite("/old", "/new"), func() *http.Request { return httptest.NewRequest(http.MethodGet, "/old/path", nil) }, writeRequestPath},
		{"sunset", actual.Sunset(at, `</policy>; rel="sunset"`), upstream.Sunset(at, `</policy>; rel="sunset"`), getRequest, writeRequestPath},
		{"maybe true", actual.Maybe(actual.SetHeader("X-Maybe", "yes"), func(*http.Request) bool { return true }), upstream.Maybe(upstream.SetHeader("X-Maybe", "yes"), func(*http.Request) bool { return true }), getRequest, writeRequestPath},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compareMiddleware(t, tc.request, tc.actual, tc.upstream, tc.handler)
		})
	}
}

func TestNoCacheDifferential(t *testing.T) {
	request := func() *http.Request {
		r := getRequest()
		r.Header.Set("If-None-Match", `"cached"`)
		return r
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Header.Get("If-None-Match"))
	}
	compareMiddleware(t, request, actual.NoCache, upstream.NoCache, handler)
}

func TestRequestSizeDifferential(t *testing.T) {
	request := func() *http.Request {
		return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("too large"))
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		_, _ = io.WriteString(w, string(body)+"|"+err.Error())
	}
	compareMiddleware(t, request, actual.RequestSize(3), upstream.RequestSize(3), handler)
}

func getRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/path", nil)
}

func writeRequestPath(w http.ResponseWriter, r *http.Request) {
	_, _ = io.WriteString(w, r.URL.Path)
}

type discardResponseWriter struct {
	header http.Header
	status int
}

func (w *discardResponseWriter) Header() http.Header {
	return w.header
}

func (w *discardResponseWriter) Write(body []byte) (int, error) {
	return len(body), nil
}

func (w *discardResponseWriter) WriteHeader(status int) {
	w.status = status
}

func benchmarkContentType(b *testing.B, middleware func(http.Handler) http.Handler) {
	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	writer := &discardResponseWriter{header: make(http.Header)}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		handler.ServeHTTP(writer, request)
	}
}

func BenchmarkContentTypeGoForge(b *testing.B) {
	benchmarkContentType(b, actual.AllowContentType("application/json"))
}

func BenchmarkContentTypeUpstream(b *testing.B) {
	benchmarkContentType(b, upstream.AllowContentType("application/json"))
}
