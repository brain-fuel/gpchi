package middleware

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

type EncoderFunc func(io.Writer, int) io.Writer

type resetWriter interface {
	io.Writer
	Reset(io.Writer)
}

type Compressor struct {
	encoders           map[string]EncoderFunc
	pools              map[string]*sync.Pool
	allowedTypes       map[string]struct{}
	allowedWildcards   map[string]struct{}
	encodingPrecedence []string
	level              int
}

var defaultCompressibleTypes = []string{
	"text/html", "text/css", "text/plain", "text/javascript",
	"application/javascript", "application/x-javascript", "application/json",
	"application/atom+xml", "application/rss+xml", "application/xml", "text/xml",
	"image/svg+xml",
}

func Compress(level int, types ...string) func(http.Handler) http.Handler {
	return NewCompressor(level, types...).Handler
}

func NewCompressor(level int, types ...string) *Compressor {
	compressor := &Compressor{
		encoders: make(map[string]EncoderFunc), pools: make(map[string]*sync.Pool),
		allowedTypes: make(map[string]struct{}), allowedWildcards: make(map[string]struct{}),
		level: level,
	}
	if len(types) == 0 {
		types = defaultCompressibleTypes
	}
	for _, contentType := range types {
		trimmed := strings.TrimSuffix(contentType, "/*")
		if strings.Contains(trimmed, "*") {
			panic(fmt.Sprintf("middleware/compress: Unsupported content-type wildcard pattern '%s'. Only '/*' supported", contentType))
		}
		if wildcard, ok := strings.CutSuffix(contentType, "/*"); ok {
			compressor.allowedWildcards[wildcard] = struct{}{}
		} else {
			compressor.allowedTypes[contentType] = struct{}{}
		}
	}
	compressor.SetEncoder("deflate", func(w io.Writer, level int) io.Writer {
		writer, _ := flate.NewWriter(w, level)
		return writer
	})
	compressor.SetEncoder("gzip", func(w io.Writer, level int) io.Writer {
		writer, _ := gzip.NewWriterLevel(w, level)
		return writer
	})
	return compressor
}

func (compressor *Compressor) SetEncoder(name string, makeEncoder EncoderFunc) {
	name = strings.ToLower(name)
	if name == "" {
		panic("the encoding can not be empty")
	}
	if makeEncoder == nil {
		panic("attempted to set a nil encoder function")
	}
	delete(compressor.pools, name)
	delete(compressor.encoders, name)
	if probe := makeEncoder(io.Discard, compressor.level); probe != nil {
		if _, resettable := probe.(resetWriter); resettable {
			compressor.pools[name] = &sync.Pool{New: func() any {
				return makeEncoder(io.Discard, compressor.level)
			}}
		}
	}
	if _, pooled := compressor.pools[name]; !pooled {
		compressor.encoders[name] = makeEncoder
	}
	for index, encoding := range compressor.encodingPrecedence {
		if encoding == name {
			compressor.encodingPrecedence = append(compressor.encodingPrecedence[:index], compressor.encodingPrecedence[index+1:]...)
			break
		}
	}
	compressor.encodingPrecedence = append([]string{name}, compressor.encodingPrecedence...)
}

func (compressor *Compressor) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoder, encoding, cleanup := compressor.encoder(r.Header.Get("Accept-Encoding"), w)
		defer cleanup()
		writer := &compressionResponseWriter{
			ResponseWriter: w, output: w, allowedTypes: compressor.allowedTypes,
			wildcards: compressor.allowedWildcards, encoding: encoding,
		}
		if encoder != nil {
			writer.output = encoder
		}
		defer writer.Close()
		next.ServeHTTP(writer, r)
	})
}

func (compressor *Compressor) encoder(header string, destination io.Writer) (io.Writer, string, func()) {
	accepted := strings.Split(strings.ToLower(header), ",")
	for _, name := range compressor.encodingPrecedence {
		matched := false
		for _, candidate := range accepted {
			if strings.Contains(candidate, name) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if pool := compressor.pools[name]; pool != nil {
			writer := pool.Get().(resetWriter)
			writer.Reset(destination)
			return writer, name, func() { pool.Put(writer) }
		}
		if makeEncoder := compressor.encoders[name]; makeEncoder != nil {
			return makeEncoder(destination, compressor.level), name, func() {}
		}
	}
	return nil, "", func() {}
}

type compressionResponseWriter struct {
	http.ResponseWriter
	output       io.Writer
	allowedTypes map[string]struct{}
	wildcards    map[string]struct{}
	encoding     string
	wroteHeader  bool
	compressible bool
}

func (writer *compressionResponseWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		writer.ResponseWriter.WriteHeader(status)
		return
	}
	writer.wroteHeader = true
	if writer.Header().Get("Content-Encoding") == "" {
		contentType, _, _ := strings.Cut(writer.Header().Get("Content-Type"), ";")
		_, exact := writer.allowedTypes[contentType]
		major, _, slash := strings.Cut(contentType, "/")
		_, wildcard := writer.wildcards[major]
		if writer.encoding != "" && (exact || slash && wildcard) {
			writer.compressible = true
			writer.Header().Set("Content-Encoding", writer.encoding)
			writer.Header().Add("Vary", "Accept-Encoding")
			writer.Header().Del("Content-Length")
		}
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *compressionResponseWriter) Write(body []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.destination().Write(body)
}

func (writer *compressionResponseWriter) destination() io.Writer {
	if writer.compressible {
		return writer.output
	}
	return writer.ResponseWriter
}

func (writer *compressionResponseWriter) Flush() {
	if flusher, ok := writer.destination().(http.Flusher); ok {
		flusher.Flush()
	}
	if flusher, ok := writer.destination().(interface{ Flush() error }); ok {
		_ = flusher.Flush()
		if underlying, ok := writer.ResponseWriter.(http.Flusher); ok {
			underlying.Flush()
		}
	}
}

func (writer *compressionResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := writer.destination().(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, errors.New("chi/middleware: http.Hijacker is unavailable on the writer")
}

func (writer *compressionResponseWriter) Push(target string, options *http.PushOptions) error {
	if pusher, ok := writer.destination().(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return errors.New("chi/middleware: http.Pusher is unavailable on the writer")
}

func (writer *compressionResponseWriter) Close() error {
	if closer, ok := writer.destination().(io.WriteCloser); ok {
		return closer.Close()
	}
	return errors.New("chi/middleware: io.WriteCloser is unavailable on the writer")
}

func (writer *compressionResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}
