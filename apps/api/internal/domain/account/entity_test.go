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
