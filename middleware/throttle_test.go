package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	upstream "github.com/go-chi/chi/v5/middleware"
	actual "goforge.dev/gpchi/middleware"
)

type throttleResult struct {
	status     int
	body       string
	retryAfter string
}

func exerciseCapacityThrottle(middleware func(http.Handler) http.Handler) throttleResult {
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))
	firstDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		close(firstDone)
	}()
	<-entered
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	close(release)
	<-firstDone
	return throttleResult{recorder.Code, recorder.Body.String(), recorder.Header().Get("Retry-After")}
}

func TestThrottleCapacityDifferential(t *testing.T) {
	retry := func(bool) time.Duration { return 3 * time.Second }
	got := exerciseCapacityThrottle(actual.ThrottleWithOpts(actual.ThrottleOpts{
		Limit: 1, RetryAfterFn: retry,
	}))
	want := exerciseCapacityThrottle(upstream.ThrottleWithOpts(upstream.ThrottleOpts{
		Limit: 1, RetryAfterFn: retry,
	}))
	if got != want {
		t.Fatalf("capacity result = %#v, upstream %#v", got, want)
	}
}

func exerciseBacklogTimeout(middleware func(http.Handler) http.Handler) throttleResult {
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))
	firstDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		close(firstDone)
	}()
	<-entered
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	close(release)
	<-firstDone
	return throttleResult{recorder.Code, recorder.Body.String(), recorder.Header().Get("Retry-After")}
}

func TestThrottleBacklogTimeoutDifferential(t *testing.T) {
	got := exerciseBacklogTimeout(actual.ThrottleBacklog(1, 1, time.Millisecond))
	want := exerciseBacklogTimeout(upstream.ThrottleBacklog(1, 1, time.Millisecond))
	if got != want {
		t.Fatalf("timeout result = %#v, upstream %#v", got, want)
	}
}

func TestThrottleCanceledContextDifferential(t *testing.T) {
	run := func(middleware func(http.Handler) http.Handler) throttleResult {
		entered := make(chan struct{})
		release := make(chan struct{})
		handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(entered)
			<-release
		}))
		firstDone := make(chan struct{})
		go func() {
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
			close(firstDone)
		}()
		<-entered
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		close(release)
		<-firstDone
		return throttleResult{recorder.Code, recorder.Body.String(), recorder.Header().Get("Retry-After")}
	}
	retry := func(canceled bool) time.Duration {
		if canceled {
			return 7 * time.Second
		}
		return time.Second
	}
	got := run(actual.ThrottleWithOpts(actual.ThrottleOpts{Limit: 1, RetryAfterFn: retry}))
	want := run(upstream.ThrottleWithOpts(upstream.ThrottleOpts{Limit: 1, RetryAfterFn: retry}))
	if got != want {
		t.Fatalf("canceled result = %#v, upstream %#v", got, want)
	}
}

func TestThrottleOptionPanicsDifferential(t *testing.T) {
	tests := []struct {
		name     string
		actual   func()
		upstream func()
	}{
		{"zero limit", func() { actual.Throttle(0) }, func() { upstream.Throttle(0) }},
		{"negative backlog", func() { actual.ThrottleBacklog(1, -1, time.Second) }, func() { upstream.ThrottleBacklog(1, -1, time.Second) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			panicText := func(call func()) (text string) {
				defer func() { text, _ = recover().(string) }()
				call()
				return ""
			}
			if got, want := panicText(tc.actual), panicText(tc.upstream); got != want || got == "" {
				t.Fatalf("panic = %q, upstream %q", got, want)
			}
		})
	}
}
