package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
	"gopkg.in/yaml.v3"
)

// openAPIDocument is the minimal OpenAPI shape these drift tests inspect.
// Properties and required are compared against the Go wire structs so a spec
// field that the runtime never emits (or a runtime field the spec never
// documented) fails here instead of drifting silently.
type openAPIDocument struct {
	Components struct {
		Schemas map[string]openAPISchema `yaml:"schemas"`
	} `yaml:"components"`
}

type openAPISchema struct {
	Required   []string                 `yaml:"required"`
	Properties map[string]openAPISchema `yaml:"properties"`
	// Enum stays untyped: polymorphic schemas mix strings with null/arrays,
	// and only string members are compared in the panelAccess lock.
	Enum []any `yaml:"enum"`
}

func loadOpenAPISchemas(t *testing.T) map[string]openAPISchema {
	t.Helper()
	body, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc openAPIDocument
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	if len(doc.Components.Schemas) == 0 {
		t.Fatal("openapi.yaml has no component schemas")
	}
	return doc.Components.Schemas
}

// goJSONFieldSets flattens a wire struct (including embedded structs such as
// client.Client inside client.View) into the set of JSON property names it can
// emit and the subset that is always emitted (no omitempty). Fields without a
// json tag surface under their Go name so a missing tag fails the comparison.
func goJSONFieldSets(typ reflect.Type) (props, required map[string]bool) {
	props = map[string]bool{}
	required = map[string]bool{}
	var walk func(reflect.Type)
	walk = func(st reflect.Type) {
		for i := 0; i < st.NumField(); i++ {
			f := st.Field(i)
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			if f.Anonymous && tag == "" {
				if f.Type.Kind() == reflect.Struct {
					walk(f.Type)
					continue
				}
			}
			if f.PkgPath != "" && !f.Anonymous {
				continue // unexported: never serialized
			}
			name := tag
			omitempty := false
			if idx := strings.IndexByte(tag, ','); idx >= 0 {
				name = tag[:idx]
				for _, opt := range strings.Split(tag[idx+1:], ",") {
					if opt == "omitempty" {
						omitempty = true
					}
				}
			}
			if name == "" {
				name = f.Name
			}
			props[name] = true
			if !omitempty {
				required[name] = true
			}
		}
	}
	walk(typ)
	return props, required
}

// assertSchemaMatchesGoWire locks an OpenAPI object schema to a Go wire
// struct: property names must be exactly the Go JSON names, and `required`
// must be exactly the non-omitempty fields (every field the runtime always
// emits must be documented as always present).
func assertSchemaMatchesGoWire(t *testing.T, schemas map[string]openAPISchema, name string, typ reflect.Type) {
	t.Helper()
	schema, ok := schemas[name]
	if !ok {
		t.Fatalf("OpenAPI schema %s not found", name)
	}
	goProps, goRequired := goJSONFieldSets(typ)

	specProps := map[string]bool{}
	for prop := range schema.Properties {
		specProps[prop] = true
	}
	for prop := range goProps {
		if !specProps[prop] {
			t.Errorf("%s: Go emits %q but OpenAPI does not document it", name, prop)
		}
	}
	for prop := range specProps {
		if !goProps[prop] {
			t.Errorf("%s: OpenAPI documents %q but the Go wire struct never emits it", name, prop)
		}
	}

	specRequired := map[string]bool{}
	for _, req := range schema.Required {
		specRequired[req] = true
	}
	for field := range goRequired {
		if !specRequired[field] {
			t.Errorf("%s: Go always emits %q (no omitempty) but it is not in OpenAPI required", name, field)
		}
	}
	for field := range specRequired {
		if !goRequired[field] {
			t.Errorf("%s: OpenAPI requires %q but the Go wire struct marks it omitempty or absent", name, field)
		}
	}
}

// TestOpenAPIClientViewMatchesGoWireFields is the schema↔Go lock for the
// client read model. It exists because the spec documented hasCreds while the
// wire emits hasCredentials (issue #792) and omitted the always-present fields
// from required (issue #794).
func TestOpenAPIClientViewMatchesGoWireFields(t *testing.T) {
	schemas := loadOpenAPISchemas(t)
	assertSchemaMatchesGoWire(t, schemas, "ClientView", reflect.TypeOf(client.View{}))

	view := schemas["ClientView"]
	if _, ok := view.Properties["hasCredentials"]; !ok {
		t.Error("ClientView must document hasCredentials (the actual wire name)")
	}
	if _, ok := view.Properties["hasCreds"]; ok {
		t.Error("ClientView documents hasCreds, which the runtime never emits")
	}
}

// TestOpenAPIBindingCapabilityMatchesGoWireFields locks the capability schema
// to the full emitted field set — trafficAccounting, quotaEnforcement,
// credentialKinds and expirationEnforcement were previously undocumented
// (issue #793).
func TestOpenAPIBindingCapabilityMatchesGoWireFields(t *testing.T) {
	schemas := loadOpenAPISchemas(t)
	assertSchemaMatchesGoWire(t, schemas, "BindingCapability", reflect.TypeOf(client.BindingCapability{}))
}

// TestOpenAPIExpirationEnforcementMatchesGoWireFields locks the expiry
// enforcement read model (issue #794).
func TestOpenAPIExpirationEnforcementMatchesGoWireFields(t *testing.T) {
	schemas := loadOpenAPISchemas(t)
	assertSchemaMatchesGoWire(t, schemas, "ExpirationEnforcement", reflect.TypeOf(client.ExpirationEnforcement{}))
}

// TestOpenAPIPanelAccessEnumsMatchValidationAllowlist locks every panelAccess
// enum in the spec to the values production validation accepts. The empty
// string was previously a documented enum member, so the generated SDK's
// Valid() accepted "" as a normal value even though it is only a write-side
// keep-current sentinel (issue #795).
func TestOpenAPIPanelAccessEnumsMatchValidationAllowlist(t *testing.T) {
	schemas := loadOpenAPISchemas(t)
	want := []string{"caddy", "direct", "local"}
	found := 0
	for name, schema := range schemas {
		prop, ok := schema.Properties["panelAccess"]
		if !ok || len(prop.Enum) == 0 {
			continue
		}
		found++
		var got []string
		for _, value := range prop.Enum {
			if s, ok := value.(string); ok {
				got = append(got, s)
			} else {
				got = append(got, "<non-string>")
			}
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s.panelAccess enum = %v, want exactly %v (no '' sentinel)", name, prop.Enum, want)
		}
	}
	if found == 0 {
		t.Fatal("no panelAccess enums found; the spec may have lost the property entirely")
	}
}

// TestSetupStatusNeverEmitsEmptyPanelAccess guards the runtime side of the
// enum lock: the documented enum has no ”, so the handler must never emit it.
func TestSetupStatusNeverEmitsEmptyPanelAccess(t *testing.T) {
	state := &managementState{
		setupAllowed: true,
		settings:     Settings{PanelListen: "127.0.0.1:2096"},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	rec := httptest.NewRecorder()

	state.handleSetupStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response SetupStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.PanelAccess != "local" {
		t.Fatalf("panelAccess=%q; unset must surface as the effective \"local\" mode", response.PanelAccess)
	}
}

// TestRedoclyDisabledRulesAllowlistIsLocked prevents the lint gate from being
// weakened silently: .redocly.yaml extends the recommended ruleset and may
// disable only the explicitly allowlisted rules (issue #794). Enabling a rule
// or removing an allowlist entry are both fine — adding a new "off" entry
// fails this test.
func TestRedoclyDisabledRulesAllowlistIsLocked(t *testing.T) {
	body, err := os.ReadFile("../../.redocly.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Extends []string          `yaml:"extends"`
		Rules   map[string]string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("parse .redocly.yaml: %v", err)
	}
	if !reflect.DeepEqual(cfg.Extends, []string{"recommended"}) {
		t.Fatalf(".redocly.yaml extends = %v, must extend recommended", cfg.Extends)
	}
	allowedOff := map[string]bool{
		"operation-operationId":  true,
		"operation-4xx-response": true,
		"tag-description":        true,
	}
	for rule, setting := range cfg.Rules {
		if setting != "off" {
			t.Errorf("rule %s is %q; only literal 'off' entries belong in the allowlist", rule, setting)
			continue
		}
		if !allowedOff[rule] {
			t.Errorf("rule %s is disabled but not allowlisted — enable it or justify and extend the test", rule)
		}
	}
}
