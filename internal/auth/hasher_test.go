package auth_test

import (
	"strings"
	"testing"

	"github.com/technosupport/ts-vms/internal/auth"
)

func TestHashPassword(t *testing.T) {
	password := "correct-horse-battery-staple"

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("Expected argon2id prefix, got %s", hash)
	}

	match, err := auth.CheckPassword(password, hash)
	if err != nil {
		t.Errorf("CheckPassword returned error: %v", err)
	}
	if !match {
		t.Errorf("Password did not match hash")
	}

	match, err = auth.CheckPassword("wrong-password", hash)
	if err != nil {
		t.Errorf("CheckPassword returned error: %v", err)
	}
	if match {
		t.Errorf("Wrong password matched hash")
	}
}
