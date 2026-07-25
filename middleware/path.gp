package middleware

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	chi "goforge.dev/gpchi"
)

func Heartbeat(endpoint string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
				strings.EqualFold(r.URL.Path, endpoint) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func StripSlashes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeContext := chi.RouteContext(r.Context())
		routePath := r.URL.Path
		if routeContext != nil && routeContext.RoutePath != "" {
			routePath = routeContext.RoutePath
		}
		if len(routePath) > 1 && strings.HasSuffix(routePath, "/") {
			routePath = strings.TrimSuffix(routePath, "/")
			if routeContext == nil {
				r.URL.Path = routePath
			} else {
				routeContext.RoutePath = routePath
			}
		}
		next.ServeHTTP(w, r)
	})
}

func RedirectSlashes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routePath := r.URL.Path
		if routeContext := chi.RouteContext(r.Context()); routeContext != nil && routeContext.RoutePath != "" {
			routePath = routeContext.RoutePath
		}
		if len(routePath) > 1 && strings.HasSuffix(routePath, "/") {
			target := "/" + strings.Trim(strings.ReplaceAll(routePath, `\`, "/"), "/")
			if r.URL.RawQuery != "" {
				target = fmt.Sprintf("%s?%s", target, r.URL.RawQuery)
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func StripPrefix(prefix string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.StripPrefix(prefix, next)
	}
}

func CleanPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeContext := chi.RouteContext(r.Context())
		if routeContext.RoutePath == "" {
			routePath := r.URL.Path
			if r.URL.RawPath != "" {
				routePath = r.URL.RawPath
			}
			routeContext.RoutePath = path.Clean(routePath)
		}
		next.ServeHTTP(w, r)
	})
}

func GetHead(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			routeContext := chi.RouteContext(r.Context())
			routePath := routeContext.RoutePath
			if routePath == "" {
				routePath = r.URL.Path
				if r.URL.RawPath != "" {
					routePath = r.URL.RawPath
				}
			}
			lookahead := chi.NewRouteContext()
			if !routeContext.Routes.Match(lookahead, http.MethodHead, routePath) {
				routeContext.RouteMethod = http.MethodGet
				routeContext.RoutePath = routePath
				next.ServeHTTP(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type urlFormatKey struct{}

var URLFormatCtxKey = &urlFormatKey{}

func URLFormat(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeContext := chi.RouteContext(r.Context())
		routePath := r.URL.Path
		if routeContext != nil && routeContext.RoutePath != "" {
			routePath = routeContext.RoutePath
		}
		format := ""
		if strings.IndexByte(routePath, '.') > 0 {
			slash := strings.LastIndexByte(routePath, '/')
			dot := strings.LastIndexByte(routePath[slash:], '.')
			if dot > 0 {
				dot += slash
				format = routePath[dot+1:]
				if routeContext != nil {
					routeContext.RoutePath = routePath[:dot]
				}
			}
		}
		ctx := context.WithValue(r.Context(), URLFormatCtxKey, format)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
