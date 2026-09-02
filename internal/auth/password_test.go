package auth

import "testing"

func TestHashAndCheck(t *testing.T) {
	h, err := HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "secret") {
		t.Fatal("should match")
	}
	if CheckPassword(h, "nope") {
		t.Fatal("should not match")
	}
}
