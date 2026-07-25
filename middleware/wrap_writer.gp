package middleware

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

type WrapResponseWriter interface {
	http.ResponseWriter
	Status() int
	BytesWritten() int
	Tee(io.Writer)
	Unwrap() http.ResponseWriter
	Discard()
}

type responseWriter struct {
	http.ResponseWriter
	tee         io.Writer
	status      int
	bytes       int
	wroteHeader bool
	discard     bool
}

func (writer *responseWriter) WriteHeader(status int) {
	informational := status >= 100 && status <= 199 && status != http.StatusSwitchingProtocols
	if informational {
		if !writer.discard {
			writer.ResponseWriter.WriteHeader(status)
		}
		return
	}
	if writer.wroteHeader {
		return
	}
	writer.status = status
	writer.wroteHeader = true
	if !writer.discard {
		writer.ResponseWriter.WriteHeader(status)
	}
}

func (writer *responseWriter) Write(body []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	var count int
	var err error
	switch {
	case writer.discard && writer.tee != nil:
		count, err = writer.tee.Write(body)
	case writer.discard:
		count, err = io.Discard.Write(body)
	default:
		count, err = writer.ResponseWriter.Write(body)
		if writer.tee != nil {
			_, teeErr := writer.tee.Write(body[:count])
			if err == nil {
				err = teeErr
			}
		}
	}
	writer.bytes += count
	return count, err
}

func (writer *responseWriter) Status() int                 { return writer.status }
func (writer *responseWriter) BytesWritten() int           { return writer.bytes }
func (writer *responseWriter) Tee(destination io.Writer)   { writer.tee = destination }
func (writer *responseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
func (writer *responseWriter) Discard()                    { writer.discard = true }
func (writer *responseWriter) markFlushed()                { writer.wroteHeader = true }
func (writer *responseWriter) flush() {
	writer.markFlushed()
	writer.ResponseWriter.(http.Flusher).Flush()
}
func (writer *responseWriter) hijack() (net.Conn, *bufio.ReadWriter, error) {
	return writer.ResponseWriter.(http.Hijacker).Hijack()
}

type flushResponseWriter struct{ responseWriter }

func (writer *flushResponseWriter) Flush() { writer.flush() }

type hijackResponseWriter struct{ responseWriter }

func (writer *hijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return writer.hijack()
}

type flushHijackResponseWriter struct{ responseWriter }

func (writer *flushHijackResponseWriter) Flush() { writer.flush() }
func (writer *flushHijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return writer.hijack()
}

type fancyResponseWriter struct{ responseWriter }

func (writer *fancyResponseWriter) Flush() { writer.flush() }
func (writer *fancyResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return writer.hijack()
}
func (writer *fancyResponseWriter) ReadFrom(source io.Reader) (int64, error) {
	if writer.tee != nil || writer.discard {
		return io.Copy(&writer.responseWriter, source)
	}
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	count, err := writer.ResponseWriter.(io.ReaderFrom).ReadFrom(source)
	writer.bytes += int(count)
	return count, err
}

type http2ResponseWriter struct{ responseWriter }

func (writer *http2ResponseWriter) Flush() { writer.flush() }
func (writer *http2ResponseWriter) Push(target string, options *http.PushOptions) error {
	return writer.ResponseWriter.(http.Pusher).Push(target, options)
}

func NewWrapResponseWriter(writer http.ResponseWriter, protocolMajor int) WrapResponseWriter {
	base := responseWriter{ResponseWriter: writer}
	_, flushes := writer.(http.Flusher)
	if protocolMajor == 2 {
		_, pushes := writer.(http.Pusher)
		if flushes && pushes {
			return &http2ResponseWriter{base}
		}
	} else {
		_, hijacks := writer.(http.Hijacker)
		_, readsFrom := writer.(io.ReaderFrom)
		switch {
		case flushes && hijacks && readsFrom:
			return &fancyResponseWriter{base}
		case flushes && hijacks:
			return &flushHijackResponseWriter{base}
		case hijacks:
			return &hijackResponseWriter{base}
		}
	}
	if flushes {
		return &flushResponseWriter{base}
	}
	return &base
}
