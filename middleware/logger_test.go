package middleware_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	upstream "github.com/go-chi/chi/v5/middleware"
	actual "goforge.dev/gpchi/middleware"
)

type capturedLog struct{ values []any }

func (logger *capturedLog) Print(values ...any) {
	logger.values = append(logger.values, values...)
}

func TestDefaultLogFormatterDifferential(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "https://example.com/path?q=1", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	actualLog, upstreamLog := &capturedLog{}, &capturedLog{}
	actualEntry := (&actual.DefaultLogFormatter{Logger: actualLog, NoColor: true}).NewLogEntry(request)
	upstreamEntry := (&upstream.DefaultLogFormatter{Logger: upstreamLog, NoColor: true}).NewLogEntry(request)
	elapsed := 250 * time.Millisecond
	actualEntry.Write(http.StatusCreated, 12, http.Header{}, elapsed, nil)
	upstreamEntry.Write(http.StatusCreated, 12, http.Header{}, elapsed, nil)
	if len(actualLog.values) != 1 || len(upstreamLog.values) != 1 ||
		actualLog.values[0] != upstreamLog.values[0] {
		t.Fatalf("log = %#v, upstream %#v", actualLog.values, upstreamLog.values)
	}
}

type recordingEntry struct {
	status, bytes int
	header        http.Header
	extra         any
	panicked      any
}

func (entry *recordingEntry) Write(status, bytes int, header http.Header, _ time.Duration, extra any) {
	entry.status, entry.bytes, entry.header, entry.extra = status, bytes, header, extra
}

func (entry *recordingEntry) Panic(value any, _ []byte) {
	entry.panicked = value
}

type recordingFormatter struct{ entry *recordingEntry }

func (formatter recordingFormatter) NewLogEntry(*http.Request) actual.LogEntry {
	return formatter.entry
}

func TestRequestLoggerAndContext(t *testing.T) {
	entry := &recordingEntry{}
	handler := actual.RequestLogger(recordingFormatter{entry})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if actual.GetLogEntry(r) != entry {
			t.Fatal("log entry missing from request context")
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("body"))
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if entry.status != http.StatusAccepted || entry.bytes != 4 || entry.header == nil || entry.extra != nil {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestRecovererUsesLogEntry(t *testing.T) {
	entry := &recordingEntry{}
	request := actual.WithLogEntry(httptest.NewRequest(http.MethodGet, "/", nil), entry)
	actual.Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})).ServeHTTP(httptest.NewRecorder(), request)
	if entry.panicked != "boom" {
		t.Fatalf("panic value = %#v", entry.panicked)
	}
}

func TestLoggerDelegatesToDefault(t *testing.T) {
	original := actual.DefaultLogger
	defer func() { actual.DefaultLogger = original }()
	called := false
	actual.DefaultLogger = func(next http.Handler) http.Handler {
		called = true
		return next
	}
	actual.Logger(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if !called {
		t.Fatal("Logger did not delegate to DefaultLogger")
	}
}

func TestLogEntryContextKeyString(t *testing.T) {
	if got, want := actual.LogEntryCtxKey.String(), upstream.LogEntryCtxKey.String(); got != want {
		t.Fatalf("key string = %q, upstream %q", got, want)
	}
	if bytes.Contains([]byte(actual.LogEntryCtxKey.String()), []byte("goforge")) {
		t.Fatal("compatibility key string leaked implementation name")
	}
}
