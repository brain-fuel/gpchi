package middleware_test

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "github.com/go-chi/chi/v5/middleware"
	actual "goforge.dev/gpchi/middleware"
)

type compressedObservation struct {
	status, encoding, vary, length, body string
}

func observeCompression(middleware func(http.Handler) http.Handler, accept, contentType string) compressedObservation {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Encoding", accept)
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", "11")
		_, _ = io.WriteString(w, "hello world")
	})).ServeHTTP(recorder, request)
	body := recorder.Body.Bytes()
	switch recorder.Header().Get("Content-Encoding") {
	case "gzip":
		reader, _ := gzip.NewReader(bytes.NewReader(body))
		body, _ = io.ReadAll(reader)
		_ = reader.Close()
	case "deflate":
		reader := flate.NewReader(bytes.NewReader(body))
		body, _ = io.ReadAll(reader)
		_ = reader.Close()
	}
	return compressedObservation{
		status:   recorder.Result().Status,
		encoding: recorder.Header().Get("Content-Encoding"),
		vary:     recorder.Header().Get("Vary"),
		length:   recorder.Header().Get("Content-Length"),
		body:     string(body),
	}
}

func TestCompressDifferential(t *testing.T) {
	tests := []struct {
		name, accept, content string
		types                 []string
	}{
		{"gzip precedence", "deflate, gzip", "application/json; charset=utf-8", nil},
		{"deflate", "deflate", "text/plain", nil},
		{"unsupported encoding", "br", "text/plain", nil},
		{"uncompressible type", "gzip", "image/png", nil},
		{"wildcard type", "gzip", "application/problem+json", []string{"application/*"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := observeCompression(actual.Compress(flate.DefaultCompression, tc.types...), tc.accept, tc.content)
			want := observeCompression(upstream.Compress(flate.DefaultCompression, tc.types...), tc.accept, tc.content)
			if got != want {
				t.Fatalf("compression = %#v, upstream %#v", got, want)
			}
		})
	}
}

type prefixWriter struct{ destination io.Writer }

func (writer prefixWriter) Write(body []byte) (int, error) {
	_, _ = io.WriteString(writer.destination, "encoded:")
	return writer.destination.Write(body)
}

func TestCompressorCustomEncoderDifferential(t *testing.T) {
	encoder := func(w io.Writer, _ int) io.Writer { return prefixWriter{destination: w} }
	actualCompressor := actual.NewCompressor(flate.DefaultCompression, "text/plain")
	actualCompressor.SetEncoder("custom", encoder)
	upstreamCompressor := upstream.NewCompressor(flate.DefaultCompression, "text/plain")
	upstreamCompressor.SetEncoder("custom", encoder)
	got := observeCompression(actualCompressor.Handler, "custom", "text/plain")
	want := observeCompression(upstreamCompressor.Handler, "custom", "text/plain")
	if got != want {
		t.Fatalf("custom compression = %#v, upstream %#v", got, want)
	}
}

func TestCompressorPanicsDifferential(t *testing.T) {
	panicText := func(call func()) (text string) {
		defer func() { text, _ = recover().(string) }()
		call()
		return ""
	}
	for _, tc := range []struct {
		name string
		got  func()
		want func()
	}{
		{"wildcard", func() { actual.NewCompressor(1, "*/json") }, func() { upstream.NewCompressor(1, "*/json") }},
		{"empty encoder", func() { actual.NewCompressor(1).SetEncoder("", prefixEncoder) }, func() { upstream.NewCompressor(1).SetEncoder("", prefixEncoder) }},
		{"nil encoder", func() { actual.NewCompressor(1).SetEncoder("x", nil) }, func() { upstream.NewCompressor(1).SetEncoder("x", nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := panicText(tc.got), panicText(tc.want); got != want || got == "" {
				t.Fatalf("panic = %q, upstream %q", got, want)
			}
		})
	}
}

func prefixEncoder(w io.Writer, _ int) io.Writer { return prefixWriter{destination: w} }
