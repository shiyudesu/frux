package domainaccount

import "testing"

func TestAccountNormalization(t *testing.T) {
	user, err := New("  Alice  ", "CaseSensitivePassword", "Alice Nickname")
	if err != nil {
		t.Fatalf("new user: %v", err)
	}
	if user.Account != "alice" {
		t.Fatalf("expected canonical account, got %q", user.Account)
	}
	if user.Nickname != "Alice Nickname" {
		t.Fatalf("nickname changed during account normalization: %q", user.Nickname)
	}
	if err := user.Authenticate("CaseSensitivePassword"); err != nil {
		t.Fatalf("authenticate original password: %v", err)
	}
	if err := user.Authenticate("casesensitivepassword"); err == nil {
		t.Fatal("expected password matching to remain case-sensitive")
	}

	restored := RestoreUser(1, "  ALICE  ", user.Password, "Alice Nickname", "", "", StatusNormal, RoleUser)
	if restored.Account != "alice" {
		t.Fatalf("expected restored canonical account, got %q", restored.Account)
	}
}
