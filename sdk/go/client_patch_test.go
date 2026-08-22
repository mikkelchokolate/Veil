package veilclient

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClientPatchRequestDistinguishesOmittedNullAndValue(t *testing.T) {
	request := ClientPatchRequest{Version: 7}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"email"`) {
		t.Fatalf("zero-value nullable field was not omitted: %s", body)
	}

	request.Email.SetNull()
	body, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"email":null`) {
		t.Fatalf("explicit null was not encoded: %s", body)
	}

	request.Email.Set("owner@example.test")
	body, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"email":"owner@example.test"`) {
		t.Fatalf("supplied value was not encoded: %s", body)
	}
}
