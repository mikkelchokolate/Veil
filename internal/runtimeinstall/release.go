package runtimeinstall

import (
	"encoding/json"
	"fmt"
)

func parseRelease(body []byte) (*Release, error) {
	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parse release JSON: %w", err)
	}
	return &release, nil
}
