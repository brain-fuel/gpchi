package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "github.com/go-chi/chi/v5/middleware"
	actual "goforge.dev/gpchi/middleware"
)

func TestPatternDifferential(t *testing.T) {
	patterns := []string{"exact", "*", "prefix-*", "*-suffix", "pre-*-post", "**"}
	values := []string{"", "exact", "prefix-value", "value-suffix", "pre-value-post", "pre-post"}
	for _, expression := range patterns {
		for _, value := range values {
			got := actual.NewPattern(expression).Match(value)
			want := upstream.NewPattern(expression).Match(value)
			if got != want {
				t.Fatalf("pattern %q value %q = %v, upstream %v", expression, value, got, want)
			}
		}
	}
}

func marker(value string) func(http.Handler) http.Handler {
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, value)
		})
	}
}

func upstreamMarker(value string) func(http.Handler) http.Handler {
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, value)
		})
	}
}

func TestHeaderRouterDifferential(t *testing.T) {
	actualRouter := actual.RouteHeaders().
		Route("Host", "api.example.com", marker("exact")).
		RouteAny("X-Mode", []string{"preview-*", "test"}, marker("any")).
		RouteDefault(marker("default"))
	upstreamRouter := upstream.RouteHeaders().
		Route("Host", "api.example.com", upstreamMarker("exact")).
		RouteAny("X-Mode", []string{"preview-*", "test"}, upstreamMarker("any")).
		RouteDefault(upstreamMarker("default"))

	tests := []struct {
		name    string
		headers map[string]string
	}{
		{"exact", map[string]string{"Host": "API.EXAMPLE.COM"}},
		{"any wildcard", map[string]string{"X-Mode": "PREVIEW-ONE"}},
		{"default", map[string]string{"X-Other": "value"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				for key, value := range tc.headers {
					r.Header.Set(key, value)
				}
				return r
			}
			compareMiddleware(t, request, actualRouter.Handler, upstreamRouter.Handler, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "next")
			})
		})
	}
}

func TestHeaderRouteIsMatchDifferential(t *testing.T) {
	actualRoute := actual.HeaderRoute{MatchAny: []actual.Pattern{actual.NewPattern("one"), actual.NewPattern("two-*")}}
	upstreamRoute := upstream.HeaderRoute{MatchAny: []upstream.Pattern{upstream.NewPattern("one"), upstream.NewPattern("two-*")}}
	for _, value := range []string{"one", "two-value", "three"} {
		if got, want := actualRoute.IsMatch(value), upstreamRoute.IsMatch(value); got != want {
			t.Fatalf("value %q = %v, upstream %v", value, got, want)
		}
	}
}

func TestPageRouteAndNewDifferential(t *testing.T) {
	page := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "page")
	})
	for _, path := range []string{"/ABOUT", "/other"} {
		request := func() *http.Request {
			return httptest.NewRequest(http.MethodGet, path, nil)
		}
		compareMiddleware(t, request, actual.PageRoute("/about", page), upstream.PageRoute("/about", page), func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "next")
		})
	}
	compareMiddleware(t, func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "/", nil)
	}, actual.New(page), upstream.New(page), func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "unreachable")
	})
}
