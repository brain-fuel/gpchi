// Command apimanifest inventories a pinned go-chi/chi checkout.
package main

import (
	"encoding/csv"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type symbol struct{ pkg, kind, name, file string }

var compatible = map[string]bool{
	"Mux": true, "Router": true, "Routes": true, "Middlewares": true, "Context": true, "RouteParams": true, "ChainHandler": true, "WalkFunc": true,
	"NewRouter": true, "NewMux": true, "Chain": true, "URLParam": true, "URLParamFromCtx": true, "RouteContext": true, "NewRouteContext": true, "RegisterMethod": true, "Walk": true, "RouteCtxKey": true,
	"ServeHTTP": true, "Use": true, "Handle": true, "HandleFunc": true, "Method": true, "MethodFunc": true, "Connect": true, "Delete": true, "Get": true, "Head": true, "Options": true, "Patch": true, "Post": true, "Put": true, "Query": true, "Trace": true, "NotFound": true, "MethodNotAllowed": true, "With": true, "Group": true, "Route": true, "Mount": true, "Match": true, "Find": true, "NotFoundHandler": true, "MethodNotAllowedHandler": true, "Reset": true, "RoutePattern": true, "Add": true, "Handler": true, "HandlerFunc": true,
}

var middlewareCompatible = map[string]bool{
	"AllowContentEncoding": true, "AllowContentType": true, "BasicAuth": true,
	"ClientIPFromHeader": true, "ClientIPFromRemoteAddr": true,
	"ClientIPFromXFF": true, "ClientIPFromXFFTrustedProxies": true,
	"ContentCharset": true, "GetClientIP": true, "GetClientIPAddr": true,
	"GetHead": true, "GetReqID": true, "Heartbeat": true, "Maybe": true, "NextRequestID": true,
	"NoCache": true, "PathRewrite": true, "PrintPrettyStack": true,
	"RealIP": true, "RedirectSlashes": true,
	"Recoverer": true, "RequestID": true, "RequestSize": true,
	"RequestIDHeader": true, "RequestIDKey": true, "CleanPath": true,
	"SetHeader": true, "StripPrefix": true, "StripSlashes": true,
	"Sunset": true, "Timeout": true, "URLFormat": true,
	"URLFormatCtxKey": true, "WithValue": true,
	"HeaderRoute": true, "HeaderRoute.IsMatch": true, "HeaderRouter": true,
	"HeaderRouter.Handler": true, "HeaderRouter.Route": true,
	"HeaderRouter.RouteAny": true, "HeaderRouter.RouteDefault": true,
	"New": true, "NewPattern": true, "PageRoute": true, "Pattern": true,
	"Pattern.Match": true, "RouteHeaders": true,
	"NewWrapResponseWriter": true, "WrapResponseWriter": true,
	"Throttle": true, "ThrottleBacklog": true, "ThrottleOpts": true,
	"ThrottleWithOpts": true,
	"IsTTY":            true, "Profiler": true, "SupressNotFound": true,
	"Compress": true, "Compressor": true, "Compressor.Handler": true,
	"Compressor.SetEncoder": true, "EncoderFunc": true, "NewCompressor": true,
	"DefaultLogFormatter": true, "DefaultLogFormatter.NewLogEntry": true,
	"DefaultLogger": true, "GetLogEntry": true, "LogEntry": true,
	"LogEntryCtxKey": true, "LogFormatter": true, "Logger": true,
	"LoggerInterface": true, "RequestLogger": true, "WithLogEntry": true,
}

func main() {
	if len(os.Args) != 3 {
		panic("usage: apimanifest UPSTREAM_ROOT OUTPUT.csv")
	}
	root, output := os.Args[1], os.Args[2]
	var symbols []symbol
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if excludedDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_example.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		if rel == "." {
			rel = "root"
		}
		add := func(kind, name string) {
			base := name
			if dot := strings.LastIndex(base, "."); dot >= 0 {
				base = base[dot+1:]
			}
			if ast.IsExported(base) {
				symbols = append(symbols, symbol{rel, kind, name, filepath.Base(path)})
			}
		}
		for _, declaration := range file.Decls {
			switch d := declaration.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					add("func", d.Name.Name)
				} else if ast.IsExported(d.Name.Name) {
					receiver := receiverName(d.Recv.List[0].Type)
					if ast.IsExported(receiver) {
						add("method", receiver+"."+d.Name.Name)
					}
				}
			case *ast.GenDecl:
				for _, specification := range d.Specs {
					switch spec := specification.(type) {
					case *ast.TypeSpec:
						add("type", spec.Name.Name)
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							add(strings.ToLower(d.Tok.String()), name.Name)
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	sort.Slice(symbols, func(i, j int) bool {
		a, b := symbols[i], symbols[j]
		if a.pkg != b.pkg {
			return a.pkg < b.pkg
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return a.kind < b.kind
	})
	file, err := os.Create(output)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	_ = writer.Write([]string{"package", "kind", "symbol", "source", "status", "destination_or_reason"})
	seen := map[string]bool{}
	for _, item := range symbols {
		key := item.pkg + "\x00" + item.kind + "\x00" + item.name
		if seen[key] {
			continue
		}
		seen[key] = true
		base := item.name
		if dot := strings.LastIndex(base, "."); dot >= 0 {
			base = base[dot+1:]
		}
		status, reason := "deferred", "outside compatibility tier 1"
		receiverOK := item.kind != "method" || strings.HasPrefix(item.name, "Mux.") || strings.HasPrefix(item.name, "Context.") || strings.HasPrefix(item.name, "RouteParams.") || strings.HasPrefix(item.name, "Middlewares.") || strings.HasPrefix(item.name, "ChainHandler.")
		if item.pkg == "root" && compatible[base] && receiverOK {
			status, reason = "compatible", "goforge.dev/gpchi"
		}
		if item.pkg == "middleware" {
			status, reason = "deferred-middleware", "capability-typed middleware requires a separate tier"
			if middlewareCompatible[item.name] {
				status, reason = "compatible", "goforge.dev/gpchi/middleware"
			}
		}
		if item.pkg == "docgen" {
			status, reason = "deferred-docgen", "use immutable RouteInfo metadata"
		}
		if err := writer.Write([]string{item.pkg, item.kind, item.name, item.file, status, reason}); err != nil {
			panic(err)
		}
	}
	if err := writer.Error(); err != nil {
		panic(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %d unique exported declarations\n", len(seen))
}

func excludedDir(name string) bool {
	return name == ".git" ||
		name == "vendor" ||
		name == "internal" ||
		name == "testdata" ||
		strings.HasPrefix(name, "_")
}

func receiverName(expression ast.Expr) string {
	switch x := expression.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return receiverName(x.X)
	case *ast.IndexExpr:
		return receiverName(x.X)
	case *ast.IndexListExpr:
		return receiverName(x.X)
	}
	return "?"
}
