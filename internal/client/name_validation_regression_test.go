package client

import (
	"errors"
	"strings"
	"testing"
)

func TestClientDisplayNameValidation(t *testing.T) {
	valid := []string{"Alice", "Иван Петров", "山田太郎"}
	for _, name := range valid {
		if err := validate(Client{Name: name, QuotaResetPolicy: ResetNever}); err != nil {
			t.Errorf("valid name %q: %v", name, err)
		}
	}
	invalid := []string{" Alice", "Alice\nAdmin", "a\u202eb", strings.Repeat("界", 129), string([]byte{0xff, 0xfe})}
	for _, name := range invalid {
		if err := validate(Client{Name: name, QuotaResetPolicy: ResetNever}); !errors.Is(err, ErrValidation) {
			t.Errorf("name %q: got %v, want ErrValidation", name, err)
		}
	}
}
