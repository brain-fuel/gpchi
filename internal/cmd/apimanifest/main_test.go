package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestOnlyConsumerNameableReceiversArePublic(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package fixture
type Public struct{}
type private struct{}
func (Public) Kept() {}
func (private) Excluded() {}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	var receivers []string
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || !ast.IsExported(function.Name.Name) {
			continue
		}
		receiver := receiverName(function.Recv.List[0].Type)
		if ast.IsExported(receiver) {
			receivers = append(receivers, receiver)
		}
	}
	if len(receivers) != 1 || receivers[0] != "Public" {
		t.Fatalf("public receivers = %v", receivers)
	}
}

func TestExcludedDirectories(t *testing.T) {
	for _, name := range []string{".git", "vendor", "internal", "testdata", "_examples"} {
		if !excludedDir(name) {
			t.Errorf("%q was not excluded", name)
		}
	}
	for _, name := range []string{"root", "middleware", "docgen"} {
		if excludedDir(name) {
			t.Errorf("%q was excluded", name)
		}
	}
}
