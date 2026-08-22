package api

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestProductionPasswordHasherUsesDefaultCost(t *testing.T) {
	hasher, ok := productionPasswordHasher().(bcryptPasswordHasher)
	if !ok {
		t.Fatalf("unexpected production hasher type %T", productionPasswordHasher())
	}
	if hasher.cost != bcrypt.DefaultCost {
		t.Fatalf("production bcrypt cost=%d, want %d", hasher.cost, bcrypt.DefaultCost)
	}
}
