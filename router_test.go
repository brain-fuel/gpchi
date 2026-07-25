package chi

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	upstream "github.com/go-chi/chi/v5"
	stdroute "goforge.dev/goplus/std/http/route"
)

func exercise(handler http.Handler, method, path string) (int, string) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	handler.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.String()
}

func installCorpus(ours *Mux, theirs *upstream.Mux) {
	ours.Get("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "healthy") })
	theirs.Get("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "healthy") })
	ours.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, URLParam(r, "id")) })
	theirs.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, upstream.URLParam(r, "id")) })
	ours.Get(`/orders/{id:[0-9]+}`, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, URLParam(r, "id")) })
	theirs.Get(`/orders/{id:[0-9]+}`, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, upstream.URLParam(r, "id")) })
	ours.Get("/files/*", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, URLParam(r, "*")) })
	theirs.Get("/files/*", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, upstream.URLParam(r, "*")) })
	ours.Post("/users", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
	theirs.Post("/users", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
}

func TestRoutingCorpusDifferential(t *testing.T) {
	ours, theirs := NewRouter(), upstream.NewRouter()
	installCorpus(ours, theirs)
	for _, test := range []struct{ method, path string }{
		{http.MethodGet, "/health"}, {http.MethodHead, "/health"},
		{http.MethodGet, "/users/alice"}, {http.MethodGet, "/orders/42"},
		{http.MethodGet, "/orders/nope"}, {http.MethodGet, "/files/a/b/c"},
		{http.MethodPost, "/users"}, {http.MethodDelete, "/users"}, {http.MethodGet, "/missing"},
	} {
		gotCode, gotBody := exercise(ours, test.method, test.path)
		wantCode, wantBody := exercise(theirs, test.method, test.path)
		if gotCode != wantCode || gotBody != wantBody {
			t.Errorf("%s %s = (%d,%q), upstream (%d,%q)", test.method, test.path, gotCode, gotBody, wantCode, wantBody)
		}
	}
}

func FuzzRoutingDifferential(f *testing.F) {
	for _, seed := range []struct {
		shape  uint8
		method uint8
		value  string
	}{
		{0, 0, "alice"},
		{1, 0, "42"},
		{1, 0, "not-a-number"},
		{2, 0, "a/b c"},
		{3, 1, "users"},
		{4, 2, "missing"},
	} {
		f.Add(seed.shape, seed.method, seed.value)
	}

	f.Fuzz(func(t *testing.T, shape, methodIndex uint8, value string) {
		ours, theirs := NewRouter(), upstream.NewRouter()
		installCorpus(ours, theirs)
		methods := [...]string{
			http.MethodGet,
			http.MethodPost,
			http.MethodHead,
			http.MethodPut,
			http.MethodDelete,
		}
		method := methods[int(methodIndex)%len(methods)]
		escaped := url.PathEscape(value)
		var path string
		switch int(shape) % 5 {
		case 0:
			path = "/users/" + escaped
		case 1:
			path = "/orders/" + escaped
		case 2:
			path = "/files/" + escaped + "/tail"
		case 3:
			path = "/" + escaped
		default:
			path = "/missing/" + escaped
		}
		gotCode, gotBody := exercise(ours, method, path)
		wantCode, wantBody := exercise(theirs, method, path)
		if gotCode != wantCode || gotBody != wantBody {
			t.Fatalf(
				"%s %s = %d/%q, upstream %d/%q",
				method, path, gotCode, gotBody, wantCode, wantBody,
			)
		}
	})
}

func TestMiddlewareWithRouteAndNetHTTPInterop(t *testing.T) {
	ours := NewRouter()
	var calls atomic.Int32
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("X-Middleware", "yes")
			next.ServeHTTP(w, r)
		})
	}
	ours.Use(middleware)
	ours.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-Inline", "yes"); next.ServeHTTP(w, r) })
	}).Get("/inline", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
	ours.Route("/api", func(r Router) {
		r.Get("/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "v1") })
	})
	server := httptest.NewServer(ours)
	defer server.Close()
	response, err := http.Get(server.URL + "/inline")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "ok" || response.Header.Get("X-Middleware") != "yes" || response.Header.Get("X-Inline") != "yes" {
		t.Fatalf("response = %q, headers=%v", body, response.Header)
	}
	if code, body := exercise(ours, http.MethodGet, "/api/version"); code != 200 || body != "v1" {
		t.Fatalf("grouped route = %d %q", code, body)
	}
	if calls.Load() != 2 {
		t.Fatalf("middleware calls = %d", calls.Load())
	}
}

func TestCompileReportsConflictsAndExportsMetadata(t *testing.T) {
	router := NewRouter()
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	router.Method(http.MethodGet, "/users/{id}", handler)
	router.Method(http.MethodGet, "/users/{name}", handler)
	snapshot, conflicts := router.Compile()
	if len(conflicts) != 1 || conflicts[0].Kind != AmbiguousRoute {
		t.Fatalf("conflicts = %#v", conflicts)
	}
	routes := snapshot.Metadata()
	if len(routes) != 2 || routes[0].Pattern != "/users/{id}" || fmt.Sprint(routes[0].ParamNames) != "[id]" {
		t.Fatalf("metadata = %#v", routes)
	}
}

func TestCustomNotFoundAndMethodNotAllowed(t *testing.T) {
	router := NewRouter()
	router.Get("/only", func(http.ResponseWriter, *http.Request) {})
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "gone", http.StatusGone) })
	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "wrong", http.StatusTeapot) })
	if code, body := exercise(router, http.MethodGet, "/missing"); code != http.StatusGone || !strings.Contains(body, "gone") {
		t.Fatalf("404 replacement = %d %q", code, body)
	}
	if code, body := exercise(router, http.MethodPost, "/only"); code != http.StatusTeapot || !strings.Contains(body, "wrong") {
		t.Fatalf("405 replacement = %d %q", code, body)
	}
}

func TestMethodNotAllowedHandlerDifferential(t *testing.T) {
	assertHandler := func(t *testing.T, ours, theirs http.Handler) {
		t.Helper()
		ourRecorder := httptest.NewRecorder()
		theirRecorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/missing", nil)
		ours.ServeHTTP(ourRecorder, request)
		theirs.ServeHTTP(theirRecorder, request.Clone(request.Context()))
		if ourRecorder.Code != theirRecorder.Code ||
			ourRecorder.Body.String() != theirRecorder.Body.String() ||
			fmt.Sprint(ourRecorder.Header()) != fmt.Sprint(theirRecorder.Header()) {
			t.Fatalf(
				"handler response=%d/%q/%v, upstream=%d/%q/%v",
				ourRecorder.Code, ourRecorder.Body.String(), ourRecorder.Header(),
				theirRecorder.Code, theirRecorder.Body.String(), theirRecorder.Header(),
			)
		}
	}

	ours := NewMux()
	theirs := upstream.NewMux()
	assertHandler(t, ours.MethodNotAllowedHandler(), theirs.MethodNotAllowedHandler())

	custom := func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "custom", http.StatusTeapot)
	}
	ours.MethodNotAllowed(custom)
	theirs.MethodNotAllowed(custom)
	assertHandler(t, ours.MethodNotAllowedHandler(), theirs.MethodNotAllowedHandler())
}

func TestDefaultMethodNotAllowedResponseDifferential(t *testing.T) {
	ours, theirs := NewRouter(), upstream.NewRouter()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		ours.Method(method, "/resource", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		theirs.Method(method, "/resource", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	}
	ourRecorder := httptest.NewRecorder()
	theirRecorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/resource", nil)
	ours.ServeHTTP(ourRecorder, request)
	theirs.ServeHTTP(theirRecorder, request.Clone(request.Context()))
	ourAllowed := ourRecorder.Header().Values("Allow")
	theirAllowed := theirRecorder.Header().Values("Allow")
	sort.Strings(ourAllowed)
	sort.Strings(theirAllowed)
	if ourRecorder.Code != theirRecorder.Code ||
		fmt.Sprint(ourAllowed) != fmt.Sprint(theirAllowed) {
		t.Fatalf(
			"default 405 = %d/%v, upstream %d/%v",
			ourRecorder.Code, ourAllowed,
			theirRecorder.Code, theirAllowed,
		)
	}
}

func TestScopedFallbackMiddlewareDifferential(t *testing.T) {
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Scoped-Fallback", "yes")
			next.ServeHTTP(w, r)
		})
	}
	notFound := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "scoped missing", http.StatusNotFound)
	}
	ours, theirs := NewRouter(), upstream.NewRouter()
	ours.With(middleware).NotFound(notFound)
	theirs.With(middleware).NotFound(notFound)

	ourRecorder := httptest.NewRecorder()
	theirRecorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	ours.ServeHTTP(ourRecorder, request)
	theirs.ServeHTTP(theirRecorder, request.Clone(request.Context()))
	if ourRecorder.Code != theirRecorder.Code ||
		ourRecorder.Body.String() != theirRecorder.Body.String() ||
		ourRecorder.Header().Get("X-Scoped-Fallback") != theirRecorder.Header().Get("X-Scoped-Fallback") {
		t.Fatalf(
			"scoped fallback = %d/%q/%q, upstream %d/%q/%q",
			ourRecorder.Code, ourRecorder.Body.String(), ourRecorder.Header().Get("X-Scoped-Fallback"),
			theirRecorder.Code, theirRecorder.Body.String(), theirRecorder.Header().Get("X-Scoped-Fallback"),
		)
	}
}

func TestMountedFallbackInheritanceDifferential(t *testing.T) {
	ours, theirs := NewRouter(), upstream.NewRouter()
	ourCustom, theirCustom := NewRouter(), upstream.NewRouter()
	ourInherited, theirInherited := NewRouter(), upstream.NewRouter()

	ours.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "root missing", http.StatusGone)
	})
	theirs.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "root missing", http.StatusGone)
	})
	ours.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "root method", http.StatusTeapot)
	})
	theirs.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "root method", http.StatusTeapot)
	})

	ourCustom.Get("/resource", func(http.ResponseWriter, *http.Request) {})
	theirCustom.Get("/resource", func(http.ResponseWriter, *http.Request) {})
	ourCustom.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "child missing", http.StatusNotFound)
	})
	theirCustom.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "child missing", http.StatusNotFound)
	})
	ourCustom.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "child method", http.StatusMethodNotAllowed)
	})
	theirCustom.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "child method", http.StatusMethodNotAllowed)
	})
	ourInherited.Get("/resource", func(http.ResponseWriter, *http.Request) {})
	theirInherited.Get("/resource", func(http.ResponseWriter, *http.Request) {})

	ours.Mount("/custom", ourCustom)
	theirs.Mount("/custom", theirCustom)
	ours.Mount("/inherited", ourInherited)
	theirs.Mount("/inherited", theirInherited)

	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/custom/missing"},
		{http.MethodPost, "/custom/resource"},
		{http.MethodGet, "/inherited/missing"},
		{http.MethodPost, "/inherited/resource"},
	} {
		ourRecorder := httptest.NewRecorder()
		theirRecorder := httptest.NewRecorder()
		request := httptest.NewRequest(test.method, test.path, nil)
		ours.ServeHTTP(ourRecorder, request)
		theirs.ServeHTTP(theirRecorder, request.Clone(request.Context()))
		if ourRecorder.Code != theirRecorder.Code || ourRecorder.Body.String() != theirRecorder.Body.String() {
			t.Errorf(
				"%s %s = %d/%q, upstream %d/%q",
				test.method, test.path, ourRecorder.Code, ourRecorder.Body.String(),
				theirRecorder.Code, theirRecorder.Body.String(),
			)
		}
	}
}

func TestRouteScopedFallbackDifferential(t *testing.T) {
	ours, theirs := NewRouter(), upstream.NewRouter()
	ours.Route("/api", func(router Router) {
		router.Get("/resource", func(http.ResponseWriter, *http.Request) {})
		router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "api missing", http.StatusGone)
		})
		router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "api method", http.StatusTeapot)
		})
	})
	theirs.Route("/api", func(router upstream.Router) {
		router.Get("/resource", func(http.ResponseWriter, *http.Request) {})
		router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "api missing", http.StatusGone)
		})
		router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "api method", http.StatusTeapot)
		})
	})

	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/missing"},
		{http.MethodPost, "/api/resource"},
		{http.MethodGet, "/outside"},
	} {
		ourRecorder := httptest.NewRecorder()
		theirRecorder := httptest.NewRecorder()
		request := httptest.NewRequest(test.method, test.path, nil)
		ours.ServeHTTP(ourRecorder, request)
		theirs.ServeHTTP(theirRecorder, request.Clone(request.Context()))
		if ourRecorder.Code != theirRecorder.Code || ourRecorder.Body.String() != theirRecorder.Body.String() {
			t.Errorf(
				"%s %s = %d/%q, upstream %d/%q",
				test.method, test.path, ourRecorder.Code, ourRecorder.Body.String(),
				theirRecorder.Code, theirRecorder.Body.String(),
			)
		}
	}
}

func TestGlobalMiddlewareRunsBeforeRouting(t *testing.T) {
	router := NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Middleware", "ran")
			if RouteContext(r.Context()) == nil {
				t.Fatal("global middleware received no route context")
			}
			next.ServeHTTP(w, r)
		})
	})
	router.Get("/found", func(w http.ResponseWriter, r *http.Request) {
		if RouteContext(r.Context()) == nil {
			t.Fatal("exact route received no route context")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	for _, test := range []struct {
		path string
		code int
	}{
		{"/found", http.StatusNoContent},
		{"/missing", http.StatusNotFound},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != test.code || recorder.Header().Get("X-Middleware") != "ran" {
			t.Fatalf("%s: status/header = %d/%q", test.path, recorder.Code, recorder.Header().Get("X-Middleware"))
		}
	}
}

func TestMatchFindExactRoute(t *testing.T) {
	router := NewRouter()
	router.Get("/exact", func(http.ResponseWriter, *http.Request) {})
	context := NewRouteContext()
	if !router.Match(context, http.MethodGet, "/exact") {
		t.Fatal("Match rejected an exact route")
	}
	context = NewRouteContext()
	if pattern := router.Find(context, http.MethodGet, "/exact"); pattern != "/exact" {
		t.Fatalf("Find pattern = %q, want /exact", pattern)
	}
	if router.Match(NewRouteContext(), http.MethodPost, "/exact") {
		t.Fatal("Match accepted an unregistered exact method")
	}
}

func TestCompatibilityRegistrationBehavior(t *testing.T) {
	noOp := func(http.ResponseWriter, *http.Request) {}
	panicText := func(call func()) (text string) {
		defer func() {
			if recovered := recover(); recovered != nil {
				text = fmt.Sprint(recovered)
			}
		}()
		call()
		return ""
	}

	ours, theirs := NewRouter(), upstream.NewRouter()
	ours.Get("/", noOp)
	theirs.Get("/", noOp)
	middleware := func(next http.Handler) http.Handler { return next }
	if got, want := panicText(func() { ours.Use(middleware) }), panicText(func() { theirs.Use(middleware) }); got != want {
		t.Fatalf("late Use panic = %q, upstream %q", got, want)
	}

	ours, theirs = NewRouter(), upstream.NewRouter()
	if got, want := panicText(func() { ours.Method("lowercase", "/", http.HandlerFunc(noOp)) }),
		panicText(func() { theirs.Method("lowercase", "/", http.HandlerFunc(noOp)) }); got != want {
		t.Fatalf("unsupported method panic = %q, upstream %q", got, want)
	}

	if got, want := panicText(func() { ours.Get("/*/not-terminal", noOp) }),
		panicText(func() { theirs.Get("/*/not-terminal", noOp) }); (got == "") != (want == "") {
		t.Fatalf("wildcard panic = %q, upstream %q", got, want)
	}
}

func TestCustomMethodAndMethodPatternHandleDifferential(t *testing.T) {
	const customMethod = "GOFORGE_AUDIT"
	RegisterMethod(customMethod)
	upstream.RegisterMethod(customMethod)
	ours, theirs := NewRouter(), upstream.NewRouter()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "custom")
	})
	ours.Method(customMethod, "/custom", handler)
	theirs.Method(customMethod, "/custom", handler)
	ours.Handle("GET /combined", handler)
	theirs.Handle("GET /combined", handler)
	for _, tc := range []struct{ method, path string }{
		{customMethod, "/custom"},
		{http.MethodGet, "/combined"},
	} {
		gotCode, gotBody := exercise(ours, tc.method, tc.path)
		wantCode, wantBody := exercise(theirs, tc.method, tc.path)
		if gotCode != wantCode || gotBody != wantBody {
			t.Fatalf("%s %s = %d/%q, upstream %d/%q", tc.method, tc.path, gotCode, gotBody, wantCode, wantBody)
		}
	}
}

func TestEmptyParameterDifferential(t *testing.T) {
	ours, theirs := NewRouter(), upstream.NewRouter()
	ours.Get("/users/{x}/{y}/{z}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s-%s-%s", URLParam(r, "x"), URLParam(r, "y"), URLParam(r, "z"))
	})
	theirs.Get("/users/{x}/{y}/{z}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s-%s-%s", upstream.URLParam(r, "x"), upstream.URLParam(r, "y"), upstream.URLParam(r, "z"))
	})
	gotCode, gotBody := exercise(ours, http.MethodGet, "/users///c")
	wantCode, wantBody := exercise(theirs, http.MethodGet, "/users///c")
	if gotCode != wantCode || gotBody != wantBody {
		t.Fatalf("empty params = %d/%q, upstream %d/%q", gotCode, gotBody, wantCode, wantBody)
	}
}

func TestAmbiguousParameterRegistrationDifferential(t *testing.T) {
	install := func(register func(string, http.HandlerFunc), param func(*http.Request, string) string) {
		register("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "first:"+param(r, "id"))
		})
		register("/users/{name}", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "second:"+param(r, "name"))
		})
	}
	ours, theirs := NewRouter(), upstream.NewRouter()
	install(ours.Get, URLParam)
	install(theirs.Get, upstream.URLParam)
	wantCode, wantBody := exercise(theirs, http.MethodGet, "/users/alice")
	gotCode, gotBody := exercise(ours, http.MethodGet, "/users/alice")
	if gotCode != wantCode || gotBody != wantBody {
		t.Fatalf("ambiguous params = %d/%q, upstream %d/%q", gotCode, gotBody, wantCode, wantBody)
	}
}

func TestNestedMountDifferential(t *testing.T) {
	ours, theirs := NewRouter(), upstream.NewRouter()
	ourNested, theirNested := NewRouter(), upstream.NewRouter()
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Nested", "yes")
			next.ServeHTTP(w, r)
		})
	}
	ourNested.Use(middleware)
	theirNested.Use(middleware)
	ourNested.Get("/", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "root") })
	theirNested.Get("/", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "root") })
	ourNested.Get("/item/{id}", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, URLParam(r, "id")) })
	theirNested.Get("/item/{id}", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, upstream.URLParam(r, "id")) })
	ours.Mount("/api", ourNested)
	theirs.Mount("/api", theirNested)

	for _, path := range []string{"/api", "/api/", "/api/item/42"} {
		got, want := httptest.NewRecorder(), httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		ours.ServeHTTP(got, request)
		theirs.ServeHTTP(want, request.Clone(request.Context()))
		if got.Code != want.Code || got.Body.String() != want.Body.String() ||
			got.Header().Get("X-Nested") != want.Header().Get("X-Nested") {
			t.Fatalf("%s = %d/%q/%q, upstream %d/%q/%q", path,
				got.Code, got.Body.String(), got.Header().Get("X-Nested"),
				want.Code, want.Body.String(), want.Header().Get("X-Nested"))
		}
	}
	var gotRoutes, wantRoutes []string
	if err := Walk(ours, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		gotRoutes = append(gotRoutes, method+" "+route)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := upstream.Walk(theirs, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		wantRoutes = append(wantRoutes, method+" "+route)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(gotRoutes)
	sort.Strings(wantRoutes)
	if fmt.Sprint(gotRoutes) != fmt.Sprint(wantRoutes) {
		t.Fatalf("mounted Walk = %v, upstream %v", gotRoutes, wantRoutes)
	}
}

func TestMountPanicsDifferential(t *testing.T) {
	panicText := func(call func()) (text string) {
		defer func() {
			if recovered := recover(); recovered != nil {
				text = fmt.Sprint(recovered)
			}
		}()
		call()
		return ""
	}
	for _, tc := range []struct {
		name string
		ours func()
		want func()
	}{
		{"nil", func() { NewRouter().Mount("/api", nil) }, func() { upstream.NewRouter().Mount("/api", nil) }},
		{"duplicate", func() {
			router := NewRouter()
			router.Mount("/api", http.NotFoundHandler())
			router.Mount("/api", http.NotFoundHandler())
		}, func() {
			router := upstream.NewRouter()
			router.Mount("/api", http.NotFoundHandler())
			router.Mount("/api", http.NotFoundHandler())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := panicText(tc.ours), panicText(tc.want); got != want {
				t.Fatalf("panic = %q, upstream %q", got, want)
			}
		})
	}
}

func TestDeepNestedMountDifferential(t *testing.T) {
	ours, theirs := NewRouter(), upstream.NewRouter()
	ourAPI, theirAPI := NewRouter(), upstream.NewRouter()
	ourVersion, theirVersion := NewRouter(), upstream.NewRouter()

	ourVersion.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s|%s", URLParam(r, "id"), RouteContext(r.Context()).RoutePattern())
	})
	theirVersion.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s|%s", upstream.URLParam(r, "id"), upstream.RouteContext(r.Context()).RoutePattern())
	})
	ourAPI.Mount("/v1", ourVersion)
	theirAPI.Mount("/v1", theirVersion)
	ours.Mount("/api", ourAPI)
	theirs.Mount("/api", theirAPI)

	gotCode, gotBody := exercise(ours, http.MethodGet, "/api/v1/items/42")
	wantCode, wantBody := exercise(theirs, http.MethodGet, "/api/v1/items/42")
	if gotCode != wantCode || gotBody != wantBody {
		t.Fatalf("deep mount = %d/%q, upstream %d/%q", gotCode, gotBody, wantCode, wantBody)
	}
}

func TestRoutePatternNormalizationDifferential(t *testing.T) {
	patterns := [][]string{
		{"/v1/*", "/resources/*", "/{resource_id}"},
		{"/v1/*", "/resources/*", "/*", "/{resource_id}"},
		{"/v1/*", "/resources/*", "/*", "/*", "/*", "/{resource_id}/*"},
		{"/v1/*", "/resources/*", "/*special_path/*", "/with_asterisks*/*", "/{resource_id}"},
		{"/"},
	}
	for _, routePatterns := range patterns {
		ours := &Context{RoutePatterns: append([]string(nil), routePatterns...)}
		theirs := &upstream.Context{RoutePatterns: append([]string(nil), routePatterns...)}
		if got, want := ours.RoutePattern(), theirs.RoutePattern(); got != want {
			t.Errorf("%v: RoutePattern = %q, upstream %q", routePatterns, got, want)
		}
	}
	var ours *Context
	var theirs *upstream.Context
	if got, want := ours.RoutePattern(), theirs.RoutePattern(); got != want {
		t.Errorf("nil RoutePattern = %q, upstream %q", got, want)
	}
}

func TestEscapedURLParametersDifferential(t *testing.T) {
	ours, theirs := NewRouter(), upstream.NewRouter()
	const pattern = "/api/{identifier}/{region}/{size}/{rotation}/*"
	ours.Get(pattern, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s|%s|%s|%s|%s",
			URLParam(r, "identifier"),
			URLParam(r, "region"),
			URLParam(r, "size"),
			URLParam(r, "rotation"),
			URLParam(r, "*"),
		)
	})
	theirs.Get(pattern, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s|%s|%s|%s|%s",
			upstream.URLParam(r, "identifier"),
			upstream.URLParam(r, "region"),
			upstream.URLParam(r, "size"),
			upstream.URLParam(r, "rotation"),
			upstream.URLParam(r, "*"),
		)
	})

	path := "/api/http:%2f%2fexample.com%2fimage.png/full/max/0/color%20profile.png"
	gotCode, gotBody := exercise(ours, http.MethodGet, path)
	wantCode, wantBody := exercise(theirs, http.MethodGet, path)
	if gotCode != wantCode || gotBody != wantBody {
		t.Fatalf("escaped parameters = %d/%q, upstream %d/%q", gotCode, gotBody, wantCode, wantBody)
	}
}

func TestStandardPathValueDifferential(t *testing.T) {
	tests := []struct {
		method  string
		pattern string
		path    string
		keys    []string
	}{
		{http.MethodGet, "/hubs/{hubID}", "/hubs/392", []string{"hubID"}},
		{http.MethodPost, "/users/{userID}/conversations/{conversationID}", "/users/Gojo/conversations/2948", []string{"userID", "conversationID"}},
		{http.MethodPost, "/users/{userID}/friends/*", "/users/Gojo/friends/all-of-them/and/more", []string{"userID", "*"}},
	}
	for _, test := range tests {
		ours, theirs := NewRouter(), upstream.NewRouter()
		render := func(w http.ResponseWriter, r *http.Request) {
			values := make([]string, len(test.keys))
			for index, key := range test.keys {
				values[index] = r.PathValue(key)
			}
			_, _ = io.WriteString(w, strings.Join(values, " "))
		}
		ours.Method(test.method, test.pattern, http.HandlerFunc(render))
		theirs.Method(test.method, test.pattern, http.HandlerFunc(render))
		gotCode, gotBody := exercise(ours, test.method, test.path)
		wantCode, wantBody := exercise(theirs, test.method, test.path)
		if gotCode != wantCode || gotBody != wantBody {
			t.Errorf(
				"%s %s = %d/%q, upstream %d/%q",
				test.method, test.path, gotCode, gotBody, wantCode, wantBody,
			)
		}
	}
}

func TestCompositeSegmentParametersDifferential(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		keys    []string
	}{
		{"/articles/{id}.json", "/articles/42.json", []string{"id"}},
		{"/articles/{id}:{op}", "/articles/42:delete", []string{"id", "op"}},
		{"/article/@{user}", "/article/@gopher", []string{"user"}},
		{"/archive/{month}-{day}-{year}", "/archive/07-23-2026", []string{"month", "day", "year"}},
		{"/foo-{suffix:[a-z]{2,3}}.json", "/foo-api.json", []string{"suffix"}},
	}
	for _, test := range tests {
		ours, theirs := NewRouter(), upstream.NewRouter()
		ourHandler := func(w http.ResponseWriter, r *http.Request) {
			values := make([]string, len(test.keys))
			for index, key := range test.keys {
				values[index] = URLParam(r, key)
			}
			_, _ = io.WriteString(w, strings.Join(values, "|"))
		}
		theirHandler := func(w http.ResponseWriter, r *http.Request) {
			values := make([]string, len(test.keys))
			for index, key := range test.keys {
				values[index] = upstream.URLParam(r, key)
			}
			_, _ = io.WriteString(w, strings.Join(values, "|"))
		}
		ours.Get(test.pattern, ourHandler)
		theirs.Get(test.pattern, theirHandler)
		gotCode, gotBody := exercise(ours, http.MethodGet, test.path)
		wantCode, wantBody := exercise(theirs, http.MethodGet, test.path)
		if gotCode != wantCode || gotBody != wantBody {
			t.Errorf(
				"%s on %s = %d/%q, upstream %d/%q",
				test.pattern, test.path, gotCode, gotBody, wantCode, wantBody,
			)
		}
	}
}

func TestCompositeSegmentCapturePlan(t *testing.T) {
	segments, names, err := parsePattern("/archive/{month}-{day}-{year}")
	if err != nil {
		t.Fatal(err)
	}
	ctx := NewRouteContext()
	route := compiledRoute{segments: segments}
	if !matchRoute(route, "/archive/07-23-2026", ctx) ||
		fmt.Sprint(names) != "[month day year]" ||
		fmt.Sprint(ctx.URLParams.Keys) != "[month day year]" ||
		fmt.Sprint(ctx.URLParams.Values) != "[07 23 2026]" {
		t.Fatalf(
			"names=%v keys=%v values=%v segment=%#v",
			names, ctx.URLParams.Keys, ctx.URLParams.Values, segments[1],
		)
	}
}

func TestCompositeSegmentPrecedenceDifferential(t *testing.T) {
	ours, theirs := NewRouter(), upstream.NewRouter()
	routes := []struct {
		pattern string
		label   string
	}{
		{"/articles/{id}", "plain"},
		{"/articles/{id}.json", "json"},
		{"/articles/{id}:{op}", "operation"},
		{"/articles/static.json", "static"},
		{"/article/@{user}", "mention"},
	}
	for _, route := range routes {
		label := route.label
		ours.Get(route.pattern, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, label)
		})
		theirs.Get(route.pattern, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, label)
		})
	}
	for _, path := range []string{
		"/articles/42",
		"/articles/42.json",
		"/articles/42:delete",
		"/articles/static.json",
		"/article/@gopher",
		"/article/gopher",
	} {
		gotCode, gotBody := exercise(ours, http.MethodGet, path)
		wantCode, wantBody := exercise(theirs, http.MethodGet, path)
		if gotCode != wantCode || gotBody != wantBody {
			t.Errorf("%s = %d/%q, upstream %d/%q", path, gotCode, gotBody, wantCode, wantBody)
		}
	}
}

func TestMatchPopulatesRouteParameters(t *testing.T) {
	router := NewRouter()
	router.Get("/teams/{team}/users/{user}", func(http.ResponseWriter, *http.Request) {})
	ctx := new(Context)
	if !router.Match(ctx, http.MethodGet, "/teams/red/users/alice") {
		t.Fatal("no match")
	}
	if ctx.URLParam("team") != "red" || ctx.URLParam("user") != "alice" {
		t.Fatalf("params = %#v", ctx.URLParams)
	}
}

func TestExhaustiveMatchOutcomes(t *testing.T) {
	router := NewRouter()
	router.Get("/users/{id}", func(http.ResponseWriter, *http.Request) {})
	snapshot, conflicts := router.Compile()
	if len(conflicts) != 0 {
		t.Fatal(conflicts)
	}
	matched, ok := snapshot.Resolve(http.MethodGet, "/users/42").(MatchedRoute)
	if !ok || matched.Params["id"] != "42" || matched.Route.Pattern != "/users/{id}" {
		t.Fatalf("matched = %#v", matched)
	}
	mismatch, ok := snapshot.Resolve(http.MethodPost, "/users/42").(MethodMismatch)
	if !ok || fmt.Sprint(mismatch.Allowed) != "[GET]" {
		t.Fatalf("mismatch = %#v", mismatch)
	}
	if _, ok := snapshot.Resolve(http.MethodGet, "/missing").(RouteMissing); !ok {
		t.Fatal("missing path did not produce RouteMissing")
	}
}

func TestTypedStandardRouteBridge(t *testing.T) {
	router := NewRouter()
	pattern := stdroute.MustPattern(17, "/typed/{name}")
	name := stdroute.NewParamKey(pattern, "name", func(raw string) (string, bool) { return raw, true })
	handler := stdroute.NewHandler(pattern, func(request stdroute.Request) {
		found := stdroute.Param(request, name).(stdroute.ParamFound[string])
		_, _ = io.WriteString(stdroute.Writer(request), found.Value)
	})
	router.Typed(http.MethodGet, pattern, handler)
	if code, body := exercise(router, http.MethodGet, "/typed/gopher"); code != 200 || body != "gopher" {
		t.Fatalf("typed route = %d %q", code, body)
	}
}

func TestCompatibilityUtilitiesDifferential(t *testing.T) {
	ours, theirs := NewRouter(), upstream.NewRouter()
	ours.Get("/users/{id}", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "user") })
	theirs.Get("/users/{id}", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "user") })
	if got, want := ours.Find(NewRouteContext(), http.MethodGet, "/users/42"), theirs.Find(upstream.NewRouteContext(), http.MethodGet, "/users/42"); got != want {
		t.Fatalf("Find = %q, upstream %q", got, want)
	}
	ours.Query("/search", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	theirs.Query("/search", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	if got, _ := exercise(ours, "QUERY", "/search"); got != http.StatusNoContent {
		t.Fatalf("QUERY status = %d", got)
	}

	nested := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, r.URL.Path) })
	ours.Mount("/assets", nested)
	theirs.Mount("/assets", nested)
	gotCode, gotBody := exercise(ours, http.MethodGet, "/assets/app.js")
	wantCode, wantBody := exercise(theirs, http.MethodGet, "/assets/app.js")
	if gotCode != wantCode || gotBody != wantBody {
		t.Fatalf("Mount = %d %q, upstream %d %q", gotCode, gotBody, wantCode, wantBody)
	}

	var walked []string
	if err := Walk(ours, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		walked = append(walked, method+" "+pattern)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var upstreamWalked []string
	if err := upstream.Walk(theirs, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		upstreamWalked = append(upstreamWalked, method+" "+pattern)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(walked)
	sort.Strings(upstreamWalked)
	if fmt.Sprint(walked) != fmt.Sprint(upstreamWalked) {
		t.Fatalf("Walk = %v, upstream %v", walked, upstreamWalked)
	}
}

func TestWalkInlineMiddlewareDifferential(t *testing.T) {
	middleware := func(next http.Handler) http.Handler { return next }
	handler := func(http.ResponseWriter, *http.Request) {}
	ours, theirs := NewRouter(), upstream.NewRouter()
	ours.With(middleware).Group(func(router Router) {
		router.Route("/foo", func(router Router) {
			router.Use(middleware)
			router.With(middleware).Post("/bar", handler)
		})
	})
	theirs.With(middleware).Group(func(router upstream.Router) {
		router.Route("/foo", func(router upstream.Router) {
			router.Use(middleware)
			router.With(middleware).Post("/bar", handler)
		})
	})

	var gotMethod, gotRoute string
	var gotCount int
	if err := Walk(ours, func(method, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		gotMethod, gotRoute, gotCount = method, route, len(middlewares)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var wantMethod, wantRoute string
	var wantCount int
	if err := upstream.Walk(theirs, func(method, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		wantMethod, wantRoute, wantCount = method, route, len(middlewares)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if gotMethod != wantMethod || gotRoute != wantRoute || gotCount != wantCount {
		t.Fatalf(
			"walk = %s %s/%d middleware, upstream %s %s/%d",
			gotMethod, gotRoute, gotCount, wantMethod, wantRoute, wantCount,
		)
	}
}

func TestMiddlewareChainOrderDifferential(t *testing.T) {
	wrap := func(label string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, label+">")
				next.ServeHTTP(w, r)
				_, _ = io.WriteString(w, "<"+label)
			})
		}
	}
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "endpoint") })
	ours, theirs := Chain(wrap("a"), wrap("b")).Handler(endpoint), upstream.Chain(wrap("a"), wrap("b")).Handler(endpoint)
	gotCode, gotBody := exercise(ours, http.MethodGet, "/")
	wantCode, wantBody := exercise(theirs, http.MethodGet, "/")
	if gotCode != wantCode || gotBody != wantBody {
		t.Fatalf("Chain = %d %q, upstream %d %q", gotCode, gotBody, wantCode, wantBody)
	}
}
