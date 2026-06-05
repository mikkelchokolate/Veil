package clientaccess

import "testing"

func TestClientLinksResponseFinalizerSetsCountAndAllowsEmptyLinks(t *testing.T) {
	response, err := NewClientLinksResponseFinalizer().Finalize(ClientLinksResponse{Links: []ClientLink{{Name: "alice"}, {Name: "bob"}}})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if response.Count != 2 {
		t.Fatalf("count = %d", response.Count)
	}
	empty, err := NewClientLinksResponseFinalizer().Finalize(ClientLinksResponse{})
	if err != nil {
		t.Fatalf("empty client links response should be valid for domainless setups: %v", err)
	}
	if empty.Count != 0 || len(empty.Artifacts) != 0 {
		t.Fatalf("empty response = %+v", empty)
	}
}
