package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSettingsInterfaceDoesNotExposeStack(t *testing.T) {
	if _, ok := reflect.TypeOf(Settings{}).FieldByName("Stack"); ok {
		t.Fatalf("Settings should not expose removed stack field")
	}
}

func TestClientLinksResponseDoesNotExposeStack(t *testing.T) {
	if _, ok := reflect.TypeOf(ClientLinksResponse{}).FieldByName("Stack"); ok {
		t.Fatalf("ClientLinksResponse should not expose stack metadata; protocols are represented by Client links")
	}
}

func TestRemovedStackModulesDoNotReturn(t *testing.T) {
	forbiddenTypes := map[string]bool{
		"StackSelection":           true,
		"StackSelectionCatalog":    true,
		"StackSelectionValidation": true,
		"StackProtocolPolicy":      true,
		"ClientLinkStackPolicy":    true,
	}
	forbiddenFuncs := map[string]bool{
		"NewStackSelectionCatalog":           true,
		"NewStackSelectionValidation":        true,
		"NewStackProtocolPolicy":             true,
		"NewClientLinkStackPolicy":           true,
		"ValidateSettingsStackCompatibility": true,
		"NewLegacyCLICompatibility":          true,
		"RejectStackSelection":               true,
		"AcceptsSettingsStackJSON":           true,
		"IsPanelOnlyStack":                   true,
		"IsTrimmedPanelOnlyStack":            true,
		"stackIncludesProtocol":              true,
		"stackAllowsProtocol":                true,
		"panelStackOptionsHTML":              true,
		"panelSettingsStackOptionsHTML":      true,
		"normalizedSettingsStack":            true,
		"LegacySettingsStack":                true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			switch typedDecl := decl.(type) {
			case *ast.GenDecl:
				if typedDecl.Tok != token.TYPE {
					continue
				}
				for _, spec := range typedDecl.Specs {
					typeSpec := spec.(*ast.TypeSpec)
					if forbiddenTypes[typeSpec.Name.Name] {
						t.Fatalf("removed stack type %s remains in %s", typeSpec.Name.Name, name)
					}
				}
			case *ast.FuncDecl:
				if forbiddenFuncs[typedDecl.Name.Name] {
					t.Fatalf("removed stack function %s remains in %s", typedDecl.Name.Name, name)
				}
			}
		}
	}
}
