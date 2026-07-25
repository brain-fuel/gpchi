package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
)

type requestIDKey int

const RequestIDKey requestIDKey = 0

var RequestIDHeader = "X-Request-Id"

var (
	requestIDPrefix  = newRequestIDPrefix()
	requestIDCounter atomic.Uint64
)

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = fmt.Sprintf("%s-%06d", requestIDPrefix, NextRequestID())
		}
		ctx := context.WithValue(r.Context(), RequestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetReqID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(RequestIDKey).(string)
	return id
}

func NextRequestID() uint64 {
	return requestIDCounter.Add(1)
}

func newRequestIDPrefix() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "localhost"
	}
	var random [12]byte
	encoded := ""
	for len(encoded) < 10 {
		_, _ = rand.Read(random[:])
		encoded = base64.StdEncoding.EncodeToString(random[:])
		encoded = strings.NewReplacer("+", "", "/", "").Replace(encoded)
	}
	return hostname + "/" + encoded[:10]
}
