package middleware_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "github.com/go-chi/chi/v5/middleware"
	actual "goforge.dev/gpchi/middleware"
)

type writerObservation struct {
	status        int
	bytes         int
	body          string
	tee           string
	unwraps       bool
	hasFlusher    bool
	hasHijacker   bool
	hasReaderFrom bool
}

func observeWriter(
	recorder *httptest.ResponseRecorder,
	wrap func(http.ResponseWriter, int) interface {
		http.ResponseWriter
		Status() int
		BytesWritten() int
		Tee(io.Writer)
		Unwrap() http.ResponseWriter
		Discard()
	},
	discard bool,
) writerObservation {
	writer := wrap(recorder, 1)
	var tee bytes.Buffer
	writer.Tee(&tee)
	writer.WriteHeader(http.StatusEarlyHints)
	writer.WriteHeader(http.StatusCreated)
	if discard {
		writer.Discard()
	}
	_, _ = writer.Write([]byte("body"))
	_, hasFlusher := writer.(http.Flusher)
	_, hasHijacker := writer.(http.Hijacker)
	_, hasReaderFrom := writer.(io.ReaderFrom)
	return writerObservation{
		status: writer.Status(), bytes: writer.BytesWritten(),
		body: recorder.Body.String(), tee: tee.String(),
		unwraps:    writer.Unwrap() == recorder,
		hasFlusher: hasFlusher, hasHijacker: hasHijacker, hasReaderFrom: hasReaderFrom,
	}
}

func TestWrapResponseWriterDifferential(t *testing.T) {
	actualWrap := func(w http.ResponseWriter, protocol int) interface {
		http.ResponseWriter
		Status() int
		BytesWritten() int
		Tee(io.Writer)
		Unwrap() http.ResponseWriter
		Discard()
	} {
		return actual.NewWrapResponseWriter(w, protocol)
	}
	upstreamWrap := func(w http.ResponseWriter, protocol int) interface {
		http.ResponseWriter
		Status() int
		BytesWritten() int
		Tee(io.Writer)
		Unwrap() http.ResponseWriter
		Discard()
	} {
		return upstream.NewWrapResponseWriter(w, protocol)
	}
	for _, discard := range []bool{false, true} {
		got := observeWriter(httptest.NewRecorder(), actualWrap, discard)
		want := observeWriter(httptest.NewRecorder(), upstreamWrap, discard)
		if got != want {
			t.Fatalf("discard=%v: observation = %#v, upstream %#v", discard, got, want)
		}
	}
}

func TestWrapResponseWriterImplicitStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		new  func(http.ResponseWriter, int) interface {
			http.ResponseWriter
			Status() int
			BytesWritten() int
		}
	}{
		{"goforge", func(w http.ResponseWriter, p int) interface {
			http.ResponseWriter
			Status() int
			BytesWritten() int
		} {
			return actual.NewWrapResponseWriter(w, p)
		}},
		{"upstream", func(w http.ResponseWriter, p int) interface {
			http.ResponseWriter
			Status() int
			BytesWritten() int
		} {
			return upstream.NewWrapResponseWriter(w, p)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := tc.new(httptest.NewRecorder(), 1)
			_, _ = writer.Write([]byte("abc"))
			if writer.Status() != http.StatusOK || writer.BytesWritten() != 3 {
				t.Fatalf("status/bytes = %d/%d", writer.Status(), writer.BytesWritten())
			}
		})
	}
}
