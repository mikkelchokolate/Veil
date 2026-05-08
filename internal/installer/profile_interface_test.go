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

func TestRURecommendedProfileDoesNotExposeProtocolInstallPlanning(t *testing.T) {
	profileType := reflect.TypeOf(RURecommendedProfile{})
	for _, field := range []string{"Stack", "InstallNaive", "InstallHysteria2", "InstallMieru", "PortPlan", "NaivePassword", "Hysteria2Password", "Hysteria2YAML", "NaiveClientURL", "Hysteria2ClientURI"} {
		if _, ok := profileType.FieldByName(field); ok {
			t.Fatalf("RURecommendedProfile should not expose install-time protocol planning field %s", field)
		}
	}
}

func TestRURecommendedInstallInputDoesNotExposeProtocolStackSelection(t *testing.T) {
	inputType := reflect.TypeOf(RURecommendedInstallInput{})
	if _, ok := inputType.FieldByName("Stack"); ok {
		t.Fatalf("RURecommendedInstallInput should not expose protocol stack selection")
	}
}

func TestInstallerPackageDoesNotKeepLegacyProtocolInstallPlanning(t *testing.T) {
	forbiddenTypes := map[string]bool{
		"RURecommendedNaiveArtifacts":    true,
		"RURecommendedHysteriaArtifacts": true,
		"ruRecommendedNaiveArtifacts":    true,
		"ruRecommendedHysteriaArtifacts": true,
		"RURecommendedPortPolicy":        true,
		"RURecommendedStackPolicy":       true,
		"SharedPortPlan":                 true,
		"PortAvailability":               true,
		"HysteriaBinaryAcquisition":      true,
		"MieruBinaryAcquisition":         true,
	}
	forbiddenFuncs := map[string]bool{
		"NewRURecommendedPortPolicy":   true,
		"NewRURecommendedStackPolicy":  true,
		"normalizeStack":               true,
		"PlanSharedPort":               true,
		"PlanStackPort":                true,
		"PlanExplicitStackPort":        true,
		"DetectPortAvailability":       true,
		"isTCPBusy":                    true,
		"isUDPBusy":                    true,
		"Hysteria2ReleaseAssetURL":     true,
		"hysteriaArch":                 true,
		"CaddyNaiveBuildHint":          true,
		"NewHysteriaBinaryAcquisition": true,
		"NewMieruBinaryAcquisition":    true,
	}
	forbiddenConsts := map[string]bool{
		"StackBoth":      true,
		"StackMieru":     true,
		"StackNaive":     true,
		"StackHysteria2": true,
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
				switch typedDecl.Tok {
				case token.TYPE:
					for _, spec := range typedDecl.Specs {
						typeSpec := spec.(*ast.TypeSpec)
						if forbiddenTypes[typeSpec.Name.Name] {
							t.Fatalf("legacy install-time protocol planning type %s remains in %s", typeSpec.Name.Name, name)
						}
					}
				case token.CONST:
					for _, spec := range typedDecl.Specs {
						valueSpec := spec.(*ast.ValueSpec)
						for _, constName := range valueSpec.Names {
							if forbiddenConsts[constName.Name] {
								t.Fatalf("legacy protocol stack constant %s remains in %s", constName.Name, name)
							}
						}
					}
				}
			case *ast.FuncDecl:
				if forbiddenFuncs[typedDecl.Name.Name] {
					t.Fatalf("legacy install-time protocol planning function %s remains in %s", typedDecl.Name.Name, name)
				}
			}
		}
	}
}
