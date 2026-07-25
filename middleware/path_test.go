package middleware_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	upchi "github.com/go-chi/chi/v5"
	upstream "github.com/go-chi/chi/v5/middleware"
	actualchi "goforge.dev/gpchi"
	actual "goforge.dev/gpchi/middleware"
)

func TestPathMiddlewareDifferential(t *testing.T) {
	tests := []struct {
		name     string
		request  func() *http.Request
		actual   func(http.Handler) http.Handler
		upstream func(http.Handler) http.Handler
	}{
		{"heartbeat", func() *http.Request { return httptest.NewRequest(http.MethodGet, "/PING", nil) }, actual.Heartbeat("/ping"), upstream.Heartbeat("/ping")},
		{"strip slashes", func() *http.Request { return httptest.NewRequest(http.MethodGet, "/one/two/", nil) }, actual.StripSlashes, upstream.StripSlashes},
		{"redirect slashes", func() *http.Request { return httptest.NewRequest(http.MethodGet, `/one\\two/?q=1`, nil) }, actual.RedirectSlashes, upstream.RedirectSlashes},
		{"strip prefix", func() *http.Request { return httptest.NewRequest(http.MethodGet, "/api/resource", nil) }, actual.StripPrefix("/api"), upstream.StripPrefix("/api")},
		{"url format", func() *http.Request { return httptest.NewRequest(http.MethodGet, "/article/1.json", nil) }, actual.URLFormat, upstream.URLFormat},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, r.URL.Path)
			}
			compareMiddleware(t, tc.request, tc.actual, tc.upstream, handler)
		})
	}
}

func TestURLFormatContextDifferential(t *testing.T) {
	var got, want string
	request := func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "/article/1.json", nil)
	}
	actual.URLFormat(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = r.Context().Value(actual.URLFormatCtxKey).(string)
	})).ServeHTTP(httptest.NewRecorder(), request())
	upstream.URLFormat(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		want, _ = r.Context().Value(upstream.URLFormatCtxKey).(string)
	})).ServeHTTP(httptest.NewRecorder(), request())
	if got != want {
		t.Fatalf("format = %q, upstream %q", got, want)
	}
}

func TestCleanPathDifferential(t *testing.T) {
	actualContext := actualchi.NewRouteContext()
	actualRequest := httptest.NewRequest(http.MethodGet, "/one//two", nil)
	actualRequest = actualRequest.WithContext(context.WithValue(actualRequest.Context(), actualchi.RouteCtxKey, actualContext))
	actual.CleanPath(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(httptest.NewRecorder(), actualRequest)

	upstreamContext := upchi.NewRouteContext()
	upstreamRequest := httptest.NewRequest(http.MethodGet, "/one//two", nil)
	upstreamRequest = upstreamRequest.WithContext(context.WithValue(upstreamRequest.Context(), upchi.RouteCtxKey, upstreamContext))
	upstream.CleanPath(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(httptest.NewRecorder(), upstreamRequest)

	if actualContext.RoutePath != upstreamContext.RoutePath {
		t.Fatalf("clean path = %q, upstream %q", actualContext.RoutePath, upstreamContext.RoutePath)
	}
}

func TestGetHeadDifferential(t *testing.T) {
	actualRouter := actualchi.NewRouter()
	actualRouter.Use(actual.GetHead)
	actualRouter.Get("/resource", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Handler", "get")
	})

	upstreamRouter := upchi.NewRouter()
	upstreamRouter.Use(upstream.GetHead)
	upstreamRouter.Get("/resource", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Handler", "get")
	})

	request := httptest.NewRequest(http.MethodHead, "/resource", nil)
	actualRecorder := httptest.NewRecorder()
	actualRouter.ServeHTTP(actualRecorder, request)
	upstreamRecorder := httptest.NewRecorder()
	upstreamRouter.ServeHTTP(upstreamRecorder, request.Clone(request.Context()))
	if actualRecorder.Code != upstreamRecorder.Code ||
		actualRecorder.Header().Get("X-Handler") != upstreamRecorder.Header().Get("X-Handler") {
		t.Fatalf("actual status/header = %d/%q, upstream %d/%q",
			actualRecorder.Code, actualRecorder.Header().Get("X-Handler"),
			upstreamRecorder.Code, upstreamRecorder.Header().Get("X-Handler"))
	}
}
