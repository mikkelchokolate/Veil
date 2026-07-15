package api

import (
	"errors"
	"unicode/utf8"
)

const (
	panelPasswordMinCharacters = 12
	panelPasswordMaxBytes      = 72
)

var (
	errPanelPasswordTooShort = errors.New("password must be at least 12 characters")
	errPanelPasswordTooLong  = errors.New("password must be at most 72 UTF-8 bytes")
)

func validatePanelPassword(password string) error {
	if utf8.RuneCountInString(password) < panelPasswordMinCharacters {
		return errPanelPasswordTooShort
	}
	if len(password) > panelPasswordMaxBytes {
		return errPanelPasswordTooLong
	}
	return nil
}
