package infraaccount

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestFindAdminPrincipalByIDPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_admin_principal_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})

	sqlDB, err := sql.Open("pgx", accountPostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	if err := db.AutoMigrate(&UserModel{}); err != nil {
		t.Fatalf("migrate account table: %v", err)
	}

	users := []UserModel{
		{ID: 1, Account: "reviewer", Password: "hash", Nickname: "Reviewer", Status: domainaccount.StatusNormal, Role: domainaccount.RoleReviewer},
		{ID: 2, Account: "disabled", Password: "hash", Nickname: "Disabled", Status: 2, Role: domainaccount.RoleReviewer},
		{ID: 3, Account: "demoted", Password: "hash", Nickname: "Demoted", Status: domainaccount.StatusNormal, Role: domainaccount.RoleUser},
		{ID: 4, Account: "unknown", Password: "hash", Nickname: "Unknown", Status: domainaccount.StatusNormal, Role: "super-admin"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create accounts: %v", err)
	}

	repo := New(db)
	tests := []struct {
		name      string
		userID    int64
		status    int
		role      string
		canReview bool
		wantErr   error
	}{
		{name: "active", userID: 1, status: domainaccount.StatusNormal, role: domainaccount.RoleReviewer, canReview: true},
		{name: "disabled", userID: 2, status: 2, role: domainaccount.RoleReviewer},
		{name: "demoted", userID: 3, status: domainaccount.StatusNormal, role: domainaccount.RoleUser},
		{name: "unknown role", userID: 4, status: domainaccount.StatusNormal, role: "super-admin"},
		{name: "missing", userID: 999, wantErr: domainaccount.ErrUserNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			principal, err := repo.FindAdminPrincipalByID(context.Background(), tt.userID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("FindAdminPrincipalByID() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if principal.UserID != tt.userID || principal.Status != tt.status || principal.Role != tt.role {
				t.Fatalf("unexpected principal: %+v", principal)
			}
			if got := principal.HasPermission(domainaccount.PermissionReviewRead); got != tt.canReview {
				t.Fatalf("review permission = %v, want %v", got, tt.canReview)
			}
		})
	}
}

func accountPostgresDSNWithSchema(dsn, schema string) string {
	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		if err == nil {
			query := parsed.Query()
			query.Set("search_path", schema)
			query.Set("TimeZone", "UTC")
			parsed.RawQuery = query.Encode()
			return parsed.String()
		}
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema + " TimeZone=UTC"
}
