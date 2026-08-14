package migration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	infraaccount "github.com/shiyudesu/frux/internal/infra/persistence/account"
)

func TestAccountRefreshSessionPersistenceAndPasswordCAS(t *testing.T) {
	fixture := newPostgresFixture(t)
	db := fixture.openGORM(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := infraaccount.New(db)
	user, err := domainaccount.New("session-postgres", "Password123!", "Session")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	currentHash := strings.Repeat("a", 64)
	session, err := domainaccount.NewRefreshSession(
		"session-one", "family-one", user.ID, currentHash, user.AuthVersion,
		now, now.Add(time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateRefreshSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	var stored infraaccount.RefreshSessionModel
	if err := db.Where("id = ?", session.ID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SecretHash != currentHash ||
		strings.Contains(stored.SecretHash, "raw-refresh-secret") {
		t.Fatalf("stored refresh credential = %+v", stored)
	}

	nextHash := strings.Repeat("b", 64)
	rotatedAt := now.Add(time.Minute)
	rotated, err := repo.RotateRefreshSession(
		context.Background(),
		domainaccount.RotateRefreshSessionInput{
			SessionID: session.ID, SecretHash: currentHash,
			NewSecretHash: nextHash, RotatedAt: rotatedAt,
			PreviousGrace: 10 * time.Second,
		},
	)
	if err != nil || rotated == nil || rotated.Session.SecretHash != nextHash {
		t.Fatalf("rotate result=%+v err=%v", rotated, err)
	}
	superseded, err := repo.RotateRefreshSession(
		context.Background(),
		domainaccount.RotateRefreshSessionInput{
			SessionID: session.ID, SecretHash: currentHash,
			NewSecretHash: strings.Repeat("c", 64),
			RotatedAt:     rotatedAt.Add(5 * time.Second),
			PreviousGrace: 10 * time.Second,
		},
	)
	if err != nil || superseded == nil || !superseded.Superseded {
		t.Fatalf("superseded result=%+v err=%v", superseded, err)
	}
	thirdHash := strings.Repeat("c", 64)
	third, err := repo.RotateRefreshSession(
		context.Background(),
		domainaccount.RotateRefreshSessionInput{
			SessionID: session.ID, SecretHash: nextHash,
			NewSecretHash: thirdHash,
			RotatedAt:     rotatedAt.Add(6 * time.Second),
			PreviousGrace: 10 * time.Second,
		},
	)
	if err != nil || third == nil || third.Session.SecretHash != thirdHash {
		t.Fatalf("second rotation result=%+v err=%v", third, err)
	}
	replay, err := repo.RotateRefreshSession(
		context.Background(),
		domainaccount.RotateRefreshSessionInput{
			SessionID: session.ID, SecretHash: currentHash,
			NewSecretHash: strings.Repeat("d", 64),
			RotatedAt:     rotatedAt.Add(7 * time.Second),
			PreviousGrace: 10 * time.Second,
		},
	)
	if err != nil || replay == nil || !replay.ReplayFound {
		t.Fatalf("replay result=%+v err=%v", replay, err)
	}

	userTwo, err := domainaccount.New("password-postgres", "Password123!", "Password")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), userTwo); err != nil {
		t.Fatal(err)
	}
	oldSession, err := domainaccount.NewRefreshSession(
		"old-session", "old-family", userTwo.ID, strings.Repeat("e", 64),
		userTwo.AuthVersion, now, now.Add(time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateRefreshSession(context.Background(), oldSession); err != nil {
		t.Fatal(err)
	}
	change, err := userTwo.PreparePasswordChange("Password123!", "Replacement123!")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := domainaccount.NewRefreshSession(
		"replacement-session", "replacement-family", userTwo.ID,
		strings.Repeat("f", 64), change.NextAuthVersion, now, now.Add(time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplacePasswordAndSessions(
		context.Background(),
		domainaccount.ReplacePasswordAndSessionsInput{
			Change: *change, ReplacementSession: replacement, ChangedAt: now,
		},
	); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplacePasswordAndSessions(
		context.Background(),
		domainaccount.ReplacePasswordAndSessionsInput{
			Change: *change, ReplacementSession: replacement, ChangedAt: now,
		},
	); !errors.Is(err, domainaccount.ErrCredentialChanged) {
		t.Fatalf("second password replacement error = %v", err)
	}
	restored, err := repo.FindByID(context.Background(), userTwo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.AuthVersion != change.NextAuthVersion ||
		restored.Authenticate("Replacement123!") != nil {
		t.Fatalf("restored account = %+v", restored)
	}
	var oldStored infraaccount.RefreshSessionModel
	if err := db.Where("id = ?", oldSession.ID).Take(&oldStored).Error; err != nil {
		t.Fatal(err)
	}
	if oldStored.RevokedAt == nil ||
		oldStored.ReplacedBySessionID != replacement.ID {
		t.Fatalf("old session was not replaced: %+v", oldStored)
	}
}
