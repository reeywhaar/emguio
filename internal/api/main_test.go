package api

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"emguio/internal/store"
)

// The suite signs in dozens of times and is not testing bcrypt.
func TestMain(m *testing.M) {
	store.SetBcryptCost(bcrypt.MinCost)
	os.Exit(m.Run())
}
