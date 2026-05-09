package diagnostics

import "testing"

func TestDNSLookupResultMapsSuccessAndError(t *testing.T) {
	ok := NewDNSLookupResult("example.com", []string{"203.0.113.1"}, "", nil).Map()
	if ok["hostname"] != "example.com" || ok["addresses"] == nil || ok["error"] != nil {
		t.Fatalf("success map = %+v", ok)
	}
	err := NewDNSLookupResult("bad", nil, "", errTest("boom")).Map()
	if err["error"] != "boom" {
		t.Fatalf("error map = %+v", err)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
