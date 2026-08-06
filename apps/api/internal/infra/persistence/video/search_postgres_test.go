package infravideo_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainsearch "github.com/shiyudesu/frux/internal/domain/search"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infraaccount "github.com/shiyudesu/frux/internal/infra/persistence/account"
	infravideo "github.com/shiyudesu/frux/internal/infra/persistence/video"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type failingAdminAuditWriter struct{}

func (failingAdminAuditWriter) AppendInTransaction(
	context.Context,
	*gorm.DB,
	*domainadminaudit.Fact,
) error {
	return errors.New("audit unavailable")
}

func (failingAdminAuditWriter) RecordCommittedWrite(*domainadminaudit.Fact) {}

func TestPostgresPublicSearchRankingVisibilityAndLiteralWildcards(t *testing.T) {
	db := openSearchPostgres(t)
	now := time.Date(2026, 8, 4, 5, 0, 0, 0, time.UTC)
	users := []infraaccount.UserModel{
		{ID: 1, Account: "author", Password: "hash", Nickname: "Author", Status: 1, Role: "user", UpdatedAt: now},
		{ID: 2, Account: "alice", Password: "hash", Nickname: "Exact", Status: 1, Role: "user", UpdatedAt: now},
		{ID: 3, Account: "alice-two", Password: "hash", Nickname: "Account prefix", Status: 1, Role: "user", UpdatedAt: now.Add(-time.Minute)},
		{ID: 4, Account: "other-one", Password: "hash", Nickname: "Alice Nick", Status: 1, Role: "user", UpdatedAt: now.Add(-2 * time.Minute)},
		{ID: 5, Account: "xxalicexx", Password: "hash", Nickname: "Account contains", Status: 1, Role: "user", UpdatedAt: now.Add(-3 * time.Minute)},
		{ID: 6, Account: "other-two", Password: "hash", Nickname: "The Alice Person", Status: 1, Role: "user", UpdatedAt: now.Add(-4 * time.Minute)},
		{ID: 7, Account: "literal%_\\user", Password: "hash", Nickname: "Literal", Status: 1, Role: "user", UpdatedAt: now},
		{ID: 8, Account: "literalxxzuser", Password: "hash", Nickname: "Wildcard decoy", Status: 1, Role: "user", UpdatedAt: now},
		{ID: 9, Account: "alice-frozen", Password: "hash", Nickname: "Alice Frozen", Status: 2, Role: "user", UpdatedAt: now.Add(time.Hour)},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed accounts: %v", err)
	}

	published := now
	videos := []infravideo.VideoModel{
		searchVideoModel(101, "cat", "exact", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published),
		searchVideoModel(102, "Cat videos", "prefix", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published.Add(-time.Minute)),
		searchVideoModel(103, "My cAt video", "contains", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusLegacyReady, published.Add(-2*time.Minute)),
		searchVideoModel(104, "Other", "A CAT appears here", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published.Add(-3*time.Minute)),
		searchVideoModel(105, "cat private", "hidden", domainvideo.StatusPublished, domainvideo.VisibilityPrivate, domainmedia.MediaStatusReady, published.Add(time.Hour)),
		searchVideoModel(106, "cat offline", "hidden", domainvideo.StatusOffline, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published.Add(time.Hour)),
		searchVideoModel(107, "cat processing", "hidden", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusProcessing, published.Add(time.Hour)),
		searchVideoModel(108, `100%_off\sale`, "literal", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published),
		searchVideoModel(109, "100XXoffZsale", "wildcard decoy", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, published),
	}
	if err := db.Create(&videos).Error; err != nil {
		t.Fatalf("seed videos: %v", err)
	}

	videoRepo := infravideo.New(db)
	videoItems, err := videoRepo.SearchVideos(context.Background(), "CAT", nil, 20)
	if err != nil {
		t.Fatalf("search videos: %v", err)
	}
	if got := videoSearchIDs(videoItems); fmt.Sprint(got) != "[101 102 103 104]" {
		t.Fatalf("video relevance/visibility order = %v, want [101 102 103 104]", got)
	}
	for index, want := range []int{1, 2, 3, 4} {
		if videoItems[index].Relevance != want {
			t.Fatalf("video %d relevance = %d, want %d", videoItems[index].ID, videoItems[index].Relevance, want)
		}
	}
	literalVideos, err := videoRepo.SearchVideos(context.Background(), `100%_off\sale`, nil, 20)
	if err != nil {
		t.Fatalf("search literal video wildcards: %v", err)
	}
	if got := videoSearchIDs(literalVideos); fmt.Sprint(got) != "[108]" {
		t.Fatalf("literal video wildcard search = %v, want [108]", got)
	}

	userRepo := infraaccount.New(db)
	userItems, err := userRepo.SearchUsers(context.Background(), "ALICE", nil, 20)
	if err != nil {
		t.Fatalf("search users: %v", err)
	}
	if got := userSearchIDs(userItems); fmt.Sprint(got) != "[2 3 4 5 6]" {
		t.Fatalf("user relevance/status order = %v, want [2 3 4 5 6]", got)
	}
	for index, want := range []int{1, 2, 3, 4, 5} {
		if userItems[index].Relevance != want {
			t.Fatalf("user %d relevance = %d, want %d", userItems[index].ID, userItems[index].Relevance, want)
		}
	}
	literalUsers, err := userRepo.SearchUsers(context.Background(), `literal%_\user`, nil, 20)
	if err != nil {
		t.Fatalf("search literal user wildcards: %v", err)
	}
	if got := userSearchIDs(literalUsers); fmt.Sprint(got) != "[7]" {
		t.Fatalf("literal user wildcard search = %v, want [7]", got)
	}
}

func TestPostgresAdminVideoSearchFiltersAndStableOrder(t *testing.T) {
	db := openSearchPostgres(t)
	now := time.Date(2026, 8, 6, 5, 0, 0, 0, time.UTC)
	if err := db.Create(&infraaccount.UserModel{
		ID: 21, Account: "operator-target", Password: "hash", Nickname: "Target",
		Status: 1, Role: "user", UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	videos := []infravideo.VideoModel{
		searchVideoModel(201, "Policy match newest", "review", domainvideo.StatusRejected, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, now),
		searchVideoModel(202, "Policy match older", "review", domainvideo.StatusRejected, domainvideo.VisibilityPrivate, domainmedia.MediaStatusReady, now.Add(-time.Minute)),
		searchVideoModel(203, "Policy published", "review", domainvideo.StatusPublished, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, now.Add(-2*time.Minute)),
		searchVideoModel(204, "Deleted policy", "review", domainvideo.StatusDeleted, domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, now.Add(time.Minute)),
	}
	for index := range videos {
		videos[index].AuthorID = 21
		videos[index].Version = index + 1
	}
	if err := db.Create(&videos).Error; err != nil {
		t.Fatalf("seed admin videos: %v", err)
	}
	repository := infravideo.New(db)
	from, to := now.Add(-time.Hour), now.Add(time.Hour)
	items, err := repository.ListAdminVideos(context.Background(), domainvideo.AdminVideoQuery{
		Status: domainvideo.StatusRejected, AuthorID: 21, Keyword: "policy",
		CreatedFrom: &from, CreatedTo: &to, Limit: 10,
	})
	if err != nil {
		t.Fatalf("admin search: %v", err)
	}
	if len(items) != 2 || items[0].ID != 201 || items[1].ID != 202 {
		t.Fatalf("admin search order = %#v", items)
	}
	if items[0].Version != 1 || items[1].Version != 2 {
		t.Fatalf("admin versions = %d,%d", items[0].Version, items[1].Version)
	}
	cursorItems, err := repository.ListAdminVideos(context.Background(), domainvideo.AdminVideoQuery{
		AuthorID: 21, CreatedFrom: &from, CreatedTo: &to, Limit: 10,
		Cursor: &domainvideo.AdminVideoCursor{CreatedAt: items[0].CreatedAt, VideoID: items[0].ID},
	})
	if err != nil {
		t.Fatalf("admin cursor search: %v", err)
	}
	for _, item := range cursorItems {
		if item.ID == 201 || item.ID == 204 {
			t.Fatalf("cursor/deleted item leaked: %d", item.ID)
		}
	}
}

func TestPostgresAdminTransitionRollsBackWhenAuditFails(t *testing.T) {
	db := openSearchPostgres(t)
	if err := db.AutoMigrate(
		&infravideo.EnforcementActionModel{},
		&infravideo.AdminTransitionIntentModel{},
	); err != nil {
		t.Fatalf("migrate admin transition tables: %v", err)
	}
	now := time.Date(2026, 8, 6, 6, 0, 0, 0, time.UTC)
	video := searchVideoModel(
		301, "Published", "", domainvideo.StatusPublished,
		domainvideo.VisibilityPublic, domainmedia.MediaStatusReady, now,
	)
	video.Version = 4
	if err := db.Create(&video).Error; err != nil {
		t.Fatalf("seed transition video: %v", err)
	}
	fact, err := domainadminaudit.NewFact(domainadminaudit.FactInput{
		ActorID: 9, Permission: domainaccount.PermissionContentEnforce,
		Action:     domainadminaudit.ActionContentEnforce,
		TargetType: domainadminaudit.TargetVideo, TargetID: "301",
		Outcome:   domainadminaudit.OutcomeSuccess,
		RequestID: "audit-0123456789abcdef0123456789abcdef",
		Detail: map[string]string{
			"http_method": "POST", "previous_status": "published",
			"new_status": "offline", "reason_code": "policy_violation",
			"route": "/api/admin/videos/:videoId/enforcement",
		},
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("build audit fact: %v", err)
	}
	repository := infravideo.New(db, infravideo.WithAdminAuditWriter(failingAdminAuditWriter{}))
	_, err = repository.CommitAdminTransition(context.Background(), domainvideo.AdminTransitionCommand{
		VideoID: 301, ActorID: 9, ExpectedVersion: 4,
		Transition: domainvideo.LifecycleTakeOffline,
		ReasonCode: domainvideo.EnforcementReasonPolicy, OccurredAt: now,
	}, fact)
	if err == nil {
		t.Fatal("expected audit failure")
	}
	var current infravideo.VideoModel
	if err := db.Where("id = ?", 301).Take(&current).Error; err != nil {
		t.Fatalf("reload video: %v", err)
	}
	if current.Status != domainvideo.StatusPublished || current.Version != 4 {
		t.Fatalf("video changed despite rollback: status=%d version=%d", current.Status, current.Version)
	}
	var actionCount, outboxCount int64
	if err := db.Model(&infravideo.EnforcementActionModel{}).Count(&actionCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&infravideo.AdminTransitionIntentModel{}).Count(&outboxCount).Error; err != nil {
		t.Fatal(err)
	}
	if actionCount != 0 || outboxCount != 0 {
		t.Fatalf("rollback counts action=%d outbox=%d", actionCount, outboxCount)
	}
}

func openSearchPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL search integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_search_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})
	sqlDB, err := sql.Open("pgx", searchPostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	if err := db.AutoMigrate(&infraaccount.UserModel{}, &infravideo.VideoModel{}, &infravideo.VideoStatModel{}); err != nil {
		t.Fatalf("migrate search tables: %v", err)
	}
	return db
}

func searchVideoModel(id int64, title, description string, status int, visibility, mediaStatus string, publishedAt time.Time) infravideo.VideoModel {
	return infravideo.VideoModel{
		ID: id, AuthorID: 1, Title: title, Description: description,
		MediaURL: fmt.Sprintf("https://example.com/%d.mp4", id),
		CoverURL: fmt.Sprintf("https://example.com/%d.jpg", id),
		Status:   status, Visibility: visibility, MediaStatus: mediaStatus, Version: 1,
		PublishedAt: &publishedAt, CreatedAt: publishedAt, UpdatedAt: publishedAt,
	}
}

func videoSearchIDs(items []*domainsearch.VideoIndexItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func userSearchIDs(items []*domainsearch.UserIndexItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func searchPostgresDSNWithSchema(dsn, schema string) string {
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
