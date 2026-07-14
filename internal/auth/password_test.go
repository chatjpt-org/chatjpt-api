package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("a secure test password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	matches, err := VerifyPassword(hash, "a secure test password")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !matches {
		t.Fatal("VerifyPassword() = false, want true")
	}

	matches, err = VerifyPassword(hash, "wrong password")
	if err != nil {
		t.Fatalf("VerifyPassword() with wrong password error = %v", err)
	}
	if matches {
		t.Fatal("VerifyPassword() = true, want false")
	}
}

func TestValidateUsername(t *testing.T) {
	validUsernames := []string{"arthur", "arthur-queiroz", "arthur_2005"}
	for _, username := range validUsernames {
		if err := ValidateUsername(username); err != nil {
			t.Errorf("ValidateUsername(%q) error = %v", username, err)
		}
	}

	invalidUsernames := []string{"ab", "Arthur", "arthur queiroz", "arthur@example.com"}
	for _, username := range invalidUsernames {
		if err := ValidateUsername(username); err == nil {
			t.Errorf("ValidateUsername(%q) error = nil, want error", username)
		}
	}
}
