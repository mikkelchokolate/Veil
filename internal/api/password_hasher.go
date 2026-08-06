package api

import "golang.org/x/crypto/bcrypt"

type PasswordHasher interface {
	Hash([]byte) ([]byte, error)
}

type bcryptPasswordHasher struct{ cost int }

func (h bcryptPasswordHasher) Hash(password []byte) ([]byte, error) {
	return bcrypt.GenerateFromPassword(password, h.cost)
}

func productionPasswordHasher() PasswordHasher {
	return bcryptPasswordHasher{cost: bcrypt.DefaultCost}
}
