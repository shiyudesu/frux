package infravideo_test

import (
	domainmedia "GCFeed/internal/domain/media"
	domainsearch "GCFeed/internal/domain/search"
	domainvideo "GCFeed/internal/domain/video"
	infraaccount "GCFeed/internal/infra/persistence/account"
	infravideo "GCFeed/internal/infra/persistence/video"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

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

func openSearchPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("GCFEED_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("GCFEED_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL search integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("gcfeed_search_test_%d", time.Now().UnixNano())
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
		Status:   status, Visibility: visibility, MediaStatus: mediaStatus,
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
