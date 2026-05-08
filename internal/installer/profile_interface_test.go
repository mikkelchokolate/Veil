package installer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRURecommendedProfileInputDoesNotExposeProtocolInstallPlanning(t *testing.T) {
	inputType := reflect.TypeOf(RURecommendedInput{})
	for _, field := range []string{"Stack", "Port", "Availability", "RandomPort"} {
		if _, ok := inputType.FieldByName(field); ok {
			t.Fatalf("RURecommendedInput should not expose protocol install planning field %s", field)
		}
	}
}

func TestRURecommendedProfileDoesNotExposeSharedProxyPortPlan(t *testing.T) {
	profileType := reflect.TypeOf(RURecommendedProfile{})
	if _, ok := profileType.FieldByName("PortPlan"); ok {
		t.Fatal("RURecommendedProfile should not expose install-time shared proxy port plan")
	}
}

func TestRURecommendedInstallInputDoesNotExposeProtocolStackSelection(t *testing.T) {
	inputType := reflect.TypeOf(RURecommendedInstallInput{})
	if _, ok := inputType.FieldByName("Stack"); ok {
		t.Fatalf("RURecommendedInstallInput should not expose protocol stack selection")
	}
}

func TestInstallerPackageDoesNotKeepLegacyProtocolArtifactBuilders(t *testing.T) {
	forbiddenTypes := map[string]bool{
		"RURecommendedNaiveArtifacts":    true,
		"RURecommendedHysteriaArtifacts": true,
		"ruRecommendedNaiveArtifacts":    true,
		"ruRecommendedHysteriaArtifacts": true,
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
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				if forbiddenTypes[typeSpec.Name.Name] {
					t.Fatalf("legacy install-time protocol artifact builder type %s remains in %s", typeSpec.Name.Name, name)
				}
			}
		}
	}
}
