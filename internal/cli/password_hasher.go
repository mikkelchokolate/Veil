package cli

import "golang.org/x/crypto/bcrypt"

type PasswordHasher interface {
	Hash([]byte) ([]byte, error)
}

type bcryptPasswordHasher struct{ cost int }

func (h bcryptPasswordHasher) Hash(password []byte) ([]byte, error) {
	return bcrypt.GenerateFromPassword(password, h.cost)
}

type RootOptions struct {
	PasswordHasher PasswordHasher
}

func productionRootOptions() RootOptions {
	return RootOptions{PasswordHasher: bcryptPasswordHasher{cost: 10}}
}
