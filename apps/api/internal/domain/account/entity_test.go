package domainaccount

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

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

func TestNewPasswordPolicyAndLegacyAuthentication(t *testing.T) {
	if _, err := New("short", "1234567", "Short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("short password error = %v", err)
	}
	if _, err := New("long", strings.Repeat("界", 25), "Long"); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("long password error = %v", err)
	}
	user, err := New("valid", "Password123!", "Valid")
	if err != nil {
		t.Fatal(err)
	}
	if user.AuthVersion != DefaultAuthVersion {
		t.Fatalf("auth version = %d", user.AuthVersion)
	}

	legacyHash, err := bcrypt.GenerateFromPassword([]byte("short"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	legacy := RestoreUser(9, "legacy", string(legacyHash), "Legacy", "", "", StatusNormal, RoleUser)
	if err := legacy.Authenticate("short"); err != nil {
		t.Fatalf("legacy authenticate: %v", err)
	}
}

func TestPreparePasswordChange(t *testing.T) {
	user, err := New("alice", "Password123!", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	user.ID = 42
	if _, err := user.PreparePasswordChange("wrong", "Replacement123!"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong current password error = %v", err)
	}
	if _, err := user.PreparePasswordChange("Password123!", "Password123!"); !errors.Is(err, ErrPasswordUnchanged) {
		t.Fatalf("unchanged password error = %v", err)
	}
	change, err := user.PreparePasswordChange("Password123!", "Replacement123!")
	if err != nil {
		t.Fatal(err)
	}
	if change.UserID != 42 || change.CurrentAuthVersion != 1 || change.NextAuthVersion != 2 {
		t.Fatalf("unexpected change: %+v", change)
	}
	restored := RestoreUserWithDashboardAuthVersion(
		user.ID, user.Account, change.NewPassword, user.Nickname, "", "", 0,
		StatusNormal, RoleUser, change.NextAuthVersion, 0, 0, 0, 0, 0,
	)
	if err := restored.Authenticate("Replacement123!"); err != nil {
		t.Fatalf("authenticate replacement: %v", err)
	}
}

func TestProfileGenderAndPrivacyValidation(t *testing.T) {
	user := RestoreUser(1, "alice", "hash", "Alice", "", "", StatusNormal, RoleUser)
	gender := GenderOther
	if err := user.UpdateProfileWithGender(nil, nil, nil, &gender); err != nil {
		t.Fatalf("update gender: %v", err)
	}
	if user.Gender != GenderOther {
		t.Fatalf("unexpected gender: %d", user.Gender)
	}
	invalid := 9
	if err := user.UpdateProfileWithGender(nil, nil, nil, &invalid); err != ErrInvalidGender {
		t.Fatalf("expected invalid gender, got %v", err)
	}

	setting, err := NewDefaultProfileSetting(1)
	if err != nil {
		t.Fatalf("default setting: %v", err)
	}
	if setting.LikedVisibility != ProfileVisibilityPrivate || setting.FavoriteVisibility != ProfileVisibilityPrivate {
		t.Fatalf("unexpected privacy defaults: %+v", setting)
	}
	public := ProfileVisibilityPublic
	if err := setting.Update(&public, nil); err != nil {
		t.Fatalf("update visibility: %v", err)
	}
	invalidVisibility := "friends"
	if err := setting.Update(nil, &invalidVisibility); err != ErrInvalidProfileVisibility {
		t.Fatalf("expected invalid visibility, got %v", err)
	}
}
