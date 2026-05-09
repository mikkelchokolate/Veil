package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type ManagementStateCodec struct{}

func NewManagementStateCodec() ManagementStateCodec { return ManagementStateCodec{} }

func (ManagementStateCodec) Decode(body []byte) (managementSnapshot, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var snapshot managementSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return managementSnapshot{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return managementSnapshot{}, errors.New("state body must contain a single JSON value")
		}
		return managementSnapshot{}, err
	}
	return snapshot, nil
}

func (ManagementStateCodec) Encode(snapshot managementSnapshot) ([]byte, error) {
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func managementStateDecodeError(err error) error {
	if err == nil {
		return nil
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
