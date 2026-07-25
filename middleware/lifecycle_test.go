package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	upstream "github.com/go-chi/chi/v5/middleware"
	actual "goforge.dev/gpchi/middleware"
)

func observe(middleware func(http.Handler) http.Handler, request *http.Request) (int, string) {
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.RemoteAddr))
	})).ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.String()
}

func TestRequestIDDifferential(t *testing.T) {
	const id = "external-request-id"
	for _, tc := range []struct {
		name string
		run  func(http.Handler) http.Handler
		get  func(context.Context) string
	}{
		{"goforge", actual.RequestID, actual.GetReqID},
		{"upstream", upstream.RequestID, upstream.GetReqID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("X-Request-Id", id)
			var got string
			tc.run(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = tc.get(r.Context())
			})).ServeHTTP(httptest.NewRecorder(), request)
			if got != id {
				t.Fatalf("request ID = %q, want %q", got, id)
			}
		})
	}
}

func TestGeneratedRequestIDShape(t *testing.T) {
	pattern := regexp.MustCompile(`^.+/[A-Za-z0-9]{10}-[0-9]{6,}$`)
	var got string
	actual.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = actual.GetReqID(r.Context())
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !pattern.MatchString(got) {
		t.Fatalf("generated request ID %q has unexpected shape", got)
	}
	if actual.GetReqID(nil) != "" || upstream.GetReqID(nil) != "" {
		t.Fatal("nil context must have no request ID")
	}
}

func TestRealIPDifferential(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		remote  string
	}{
		{"true client wins", map[string]string{"True-Client-IP": "192.0.2.1", "X-Real-IP": "192.0.2.2"}, "127.0.0.1:1234"},
		{"real IP", map[string]string{"X-Real-IP": "192.0.2.2"}, "127.0.0.1:1234"},
		{"xff leftmost", map[string]string{"X-Forwarded-For": "192.0.2.3, 192.0.2.4"}, "127.0.0.1:1234"},
		{"invalid unchanged", map[string]string{"X-Real-IP": "not-an-ip"}, "127.0.0.1:1234"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = tc.remote
			for key, value := range tc.headers {
				request.Header.Set(key, value)
			}
			upRequest := request.Clone(request.Context())
			upRequest.RemoteAddr = request.RemoteAddr
			_, got := observe(actual.RealIP, request)
			_, want := observe(upstream.RealIP, upRequest)
			if got != want {
				t.Fatalf("RemoteAddr = %q, upstream %q", got, want)
			}
		})
	}
}

func TestRecovererDifferential(t *testing.T) {
	for _, tc := range []struct {
		name string
		mw   func(http.Handler) http.Handler
	}{
		{"goforge", actual.Recoverer},
		{"upstream", upstream.Recoverer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tc.mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic(errors.New("boom"))
			})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d", recorder.Code)
			}
		})
	}
}

func TestRecovererPreservesAbortHandler(t *testing.T) {
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Fatal("ErrAbortHandler was not re-panicked")
		}
	}()
	actual.Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestTimeoutDifferential(t *testing.T) {
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	for _, tc := range []struct {
		name string
		mw   func(time.Duration) func(http.Handler) http.Handler
	}{
		{"goforge", actual.Timeout},
		{"upstream", upstream.Timeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tc.mw(time.Millisecond)(handler).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != http.StatusGatewayTimeout {
				t.Fatalf("status = %d", recorder.Code)
			}
		})
	}
}

func TestClientIPDifferential(t *testing.T) {
	type middlewareFactory func(http.Handler) http.Handler
	tests := []struct {
		name     string
		request  func() *http.Request
		actual   middlewareFactory
		upstream middlewareFactory
	}{
		{
			"trusted header last value",
			func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Add("CF-Connecting-IP", "192.0.2.1")
				r.Header.Add("CF-Connecting-IP", "::ffff:192.0.2.2")
				return r
			},
			actual.ClientIPFromHeader("CF-Connecting-IP"),
			upstream.ClientIPFromHeader("CF-Connecting-IP"),
		},
		{
			"xff skips trusted right side",
			func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set("X-Forwarded-For", "198.51.100.4, 10.0.0.2, 10.0.0.3")
				return r
			},
			actual.ClientIPFromXFF("10.0.0.0/8"),
			upstream.ClientIPFromXFF("10.0.0.0/8"),
		},
		{
			"xff fixed proxy count",
			func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set("X-Forwarded-For", "198.51.100.4, 10.0.0.2")
				return r
			},
			actual.ClientIPFromXFFTrustedProxies(2),
			upstream.ClientIPFromXFFTrustedProxies(2),
		},
		{
			"remote address",
			func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.RemoteAddr = "[::ffff:192.0.2.9]:443"
				return r
			},
			actual.ClientIPFromRemoteAddr,
			upstream.ClientIPFromRemoteAddr,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			readActual := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				if got := actual.GetClientIP(r.Context()); got != actual.GetClientIPAddr(r.Context()).String() {
					t.Fatalf("string IP %q differs from typed IP", got)
				}
			})
			var got, want string
			tc.actual(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				readActual.ServeHTTP(httptest.NewRecorder(), r)
				got = actual.GetClientIP(r.Context())
			})).ServeHTTP(httptest.NewRecorder(), tc.request())
			tc.upstream(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				want = upstream.GetClientIP(r.Context())
			})).ServeHTTP(httptest.NewRecorder(), tc.request())
			if got != want {
				t.Fatalf("client IP = %q, upstream %q", got, want)
			}
		})
	}
}

func TestClientIPFailsClosed(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Forwarded-For", "198.51.100.4, malformed, 10.0.0.3")
	actual.ClientIPFromXFF("10.0.0.0/8")(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if got := actual.GetClientIP(r.Context()); got != "" {
			t.Fatalf("client IP = %q after malformed chain", got)
		}
	})).ServeHTTP(httptest.NewRecorder(), request)
}
