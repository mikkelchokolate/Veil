package backupsftp

import (
	"bytes"
	"encoding/json"
)

func marshalConfig(config Config) ([]byte, error) {
	body, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func unmarshalConfig(body []byte) (*Config, error) {
	config := &Config{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(config); err != nil {
		return nil, err
	}
	return config, nil
}

func marshalStatus(status Status) ([]byte, error) {
	body, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func unmarshalStatus(body []byte, status *Status) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(status)
}
