package identity

import "testing"

func TestPasswordHashAndPolicy(t *testing.T) {
	password := "Sufficiently.Strong-2026"
	if err := ValidatePassword(password, "operator", "operator@example.test"); err != nil {
		t.Fatal(err)
	}
	encoded, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := VerifyPassword(encoded, password)
	if err != nil || !valid {
		t.Fatalf("valid password rejected: valid=%v err=%v", valid, err)
	}
	valid, err = VerifyPassword(encoded, "wrong-password")
	if err != nil || valid {
		t.Fatalf("wrong password accepted: valid=%v err=%v", valid, err)
	}
}

func TestPasswordRejectsIdentityAndWeakValues(t *testing.T) {
	for _, password := range []string{"short", "password1234", "operator-secure-2026", "mailbox-secure-2026"} {
		if err := ValidatePassword(password, "operator", "mailbox@example.test"); err == nil {
			t.Fatalf("weak password %q accepted", password)
		}
	}
}
