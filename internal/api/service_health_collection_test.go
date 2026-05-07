package api

import "testing"

func TestServiceHealthCollectionChecksOnlySuccessfulNamedActions(t *testing.T) {
	var checked []string
	collection := NewServiceHealthCollection(func(name string) ServiceHealthResult {
		checked = append(checked, name)
		return ServiceHealthResult{Name: name, Healthy: true}
	})
	checks := collection.Check([]ServiceActionResult{
		{Name: "caddy", Success: true},
		{Name: "", Success: true},
		{Name: "hysteria2", Success: false},
		{Name: "sing-box", Success: true},
	})
	if len(checks) != 2 || checks[0].Name != "caddy" || checks[1].Name != "sing-box" {
		t.Fatalf("checks = %+v", checks)
	}
	if len(checked) != 2 || checked[0] != "caddy" || checked[1] != "sing-box" {
		t.Fatalf("checked = %+v", checked)
	}
}
