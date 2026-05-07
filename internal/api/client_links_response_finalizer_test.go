package api

import "testing"

func TestClientLinksResponseFinalizerSetsCountAndRejectsEmptyLinks(t *testing.T) {
	response, err := NewClientLinksResponseFinalizer().Finalize(ClientLinksResponse{Links: []ClientLink{{Name: "alice"}, {Name: "bob"}}})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if response.Count != 2 {
		t.Fatalf("count = %d", response.Count)
	}
	_, err = NewClientLinksResponseFinalizer().Finalize(ClientLinksResponse{})
	if err == nil || err.Error() != "no enabled client links are available" {
		t.Fatalf("err = %v", err)
	}
}
