package chi

import (
	"context"
	"net/http"
)

// These assignments make tier-1 signature drift a compile-time failure.
var (
	_ Router                                                = NewRouter()
	_ Routes                                                = NewMux()
	_ func() *Mux                                           = NewRouter
	_ func() *Mux                                           = NewMux
	_ func(...func(http.Handler) http.Handler) Middlewares  = Chain
	_ func(*http.Request, string) string                    = URLParam
	_ func(context.Context, string) string                  = URLParamFromCtx
	_ func(context.Context) *Context                        = RouteContext
	_ func() *Context                                       = NewRouteContext
	_ func(string)                                          = RegisterMethod
	_ func(Routes, WalkFunc) error                          = Walk
	_ func(*Mux, http.ResponseWriter, *http.Request)        = (*Mux).ServeHTTP
	_ func(*Mux, ...func(http.Handler) http.Handler)        = (*Mux).Use
	_ func(*Mux, string, http.Handler)                      = (*Mux).Handle
	_ func(*Mux, string, http.HandlerFunc)                  = (*Mux).HandleFunc
	_ func(*Mux, string, string, http.Handler)              = (*Mux).Method
	_ func(*Mux, string, string, http.HandlerFunc)          = (*Mux).MethodFunc
	_ func(*Mux, string, http.HandlerFunc)                  = (*Mux).Get
	_ func(*Mux, string, http.HandlerFunc)                  = (*Mux).Post
	_ func(*Mux, string, http.HandlerFunc)                  = (*Mux).Query
	_ func(*Mux, ...func(http.Handler) http.Handler) Router = (*Mux).With
	_ func(*Mux, func(Router)) Router                       = (*Mux).Group
	_ func(*Mux, string, func(Router)) Router               = (*Mux).Route
	_ func(*Mux, string, http.Handler)                      = (*Mux).Mount
	_ func(*Mux) []Route                                    = (*Mux).Routes
	_ func(*Mux) Middlewares                                = (*Mux).Middlewares
	_ func(*Mux, *Context, string, string) bool             = (*Mux).Match
	_ func(*Mux, *Context, string, string) string           = (*Mux).Find
	_ func(*Context)                                        = (*Context).Reset
	_ func(*Context, string) string                         = (*Context).URLParam
	_ func(*Context) string                                 = (*Context).RoutePattern
	_ func(*RouteParams, string, string)                    = (*RouteParams).Add
	_ func(Middlewares, http.Handler) http.Handler          = Middlewares.Handler
	_ func(Middlewares, http.HandlerFunc) http.Handler      = Middlewares.HandlerFunc
)
