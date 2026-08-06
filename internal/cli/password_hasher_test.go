package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

func newFastRootCommand(version string) *cobra.Command {
	return NewRootCommandWithOptions(version, RootOptions{PasswordHasher: bcryptPasswordHasher{cost: bcrypt.MinCost}})
}

func TestProductionRootUsesBcryptCost10(t *testing.T) {
	hasher, ok := productionRootOptions().PasswordHasher.(bcryptPasswordHasher)
	if !ok || hasher.cost != 10 {
		t.Fatalf("production password policy = %#v, want bcrypt cost 10", productionRootOptions().PasswordHasher)
	}
}
