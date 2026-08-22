package api

import (
	"encoding/json"
	"testing"
)

func responseErrorMessage(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v; body=%s", err, body)
	}
	return envelope.Error.Message
}
