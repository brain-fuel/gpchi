package middleware

import (
	"net/http"
	"strconv"
	"time"
)

type ThrottleOpts struct {
	RetryAfterFn   func(ctxDone bool) time.Duration
	Limit          int
	BacklogLimit   int
	BacklogTimeout time.Duration
	StatusCode     int
}

func Throttle(limit int) func(http.Handler) http.Handler {
	return ThrottleWithOpts(ThrottleOpts{Limit: limit, BacklogTimeout: 60 * time.Second})
}

func ThrottleBacklog(limit, backlogLimit int, backlogTimeout time.Duration) func(http.Handler) http.Handler {
	return ThrottleWithOpts(ThrottleOpts{
		Limit: limit, BacklogLimit: backlogLimit, BacklogTimeout: backlogTimeout,
	})
}

func ThrottleWithOpts(options ThrottleOpts) func(http.Handler) http.Handler {
	if options.Limit < 1 {
		panic("chi/middleware: Throttle expects limit > 0")
	}
	if options.BacklogLimit < 0 {
		panic("chi/middleware: Throttle expects backlogLimit to be positive")
	}
	status := options.StatusCode
	if status == 0 {
		status = http.StatusTooManyRequests
	}
	processing := make(chan struct{}, options.Limit)
	backlog := make(chan struct{}, options.Limit+options.BacklogLimit)
	for index := 0; index < cap(backlog); index++ {
		backlog <- struct{}{}
		if index < options.Limit {
			processing <- struct{}{}
		}
	}

	reject := func(w http.ResponseWriter, contextDone bool, message string) {
		if options.RetryAfterFn != nil {
			seconds := int(options.RetryAfterFn(contextDone).Seconds())
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
		}
		http.Error(w, message, status)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
				reject(w, true, "Context was canceled.")
				return
			case reservation := <-backlog:
				defer func() { backlog <- reservation }()
			default:
				reject(w, false, "Server capacity exceeded.")
				return
			}

			select {
			case token := <-processing:
				defer func() { processing <- token }()
				next.ServeHTTP(w, r)
				return
			default:
			}

			timer := time.NewTimer(options.BacklogTimeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				reject(w, false, "Timed out while waiting for a pending request to complete.")
			case <-r.Context().Done():
				reject(w, true, "Context was canceled.")
			case token := <-processing:
				defer func() { processing <- token }()
				next.ServeHTTP(w, r)
			}
		})
	}
}
