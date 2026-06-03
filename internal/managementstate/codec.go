package managementstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type ManagementStateCodec struct{}

func NewManagementStateCodec() ManagementStateCodec { return ManagementStateCodec{} }

func (ManagementStateCodec) Decode(body []byte) (model.ManagementSnapshot, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var snapshot model.ManagementSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return model.ManagementSnapshot{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return model.ManagementSnapshot{}, errors.New("state body must contain a single JSON value")
		}
		return model.ManagementSnapshot{}, err
	}
	return snapshot, nil
}

func (ManagementStateCodec) Encode(snapshot model.ManagementSnapshot) ([]byte, error) {
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func DecodeError(err error) error {
	if err == nil {
		return nil
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
