package infracache

import (
	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type committedSnapshotFakeRedis struct {
	redis.Cmdable
	evalResult []any
	pExpireErr error
	zAddErr    error
}

func (r *committedSnapshotFakeRedis) Eval(context.Context, string, []string, ...any) *redis.Cmd {
	return redis.NewCmdResult(r.evalResult, nil)
}

func (r *committedSnapshotFakeRedis) PExpire(context.Context, string, time.Duration) *redis.BoolCmd {
	return redis.NewBoolResult(r.pExpireErr == nil, r.pExpireErr)
}

func (r *committedSnapshotFakeRedis) ZAdd(context.Context, string, ...redis.Z) *redis.IntCmd {
	return redis.NewIntResult(1, r.zAddErr)
}

func TestRecommendationSnapshotNearExpiryRetryUsesAuthoritativeExpiration(t *testing.T) {
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	nearExpiry := &applicationrecommendation.Snapshot{ExpiresAt: now.Add(75 * time.Millisecond)}

	remaining, err := snapshotCreateTTL(nearExpiry, 5*time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if remaining != 75*time.Millisecond {
		t.Fatalf("near-expiry retry TTL = %s, want 75ms", remaining)
	}

	validSessionExpiry := now.Add(5 * time.Minute)
	indexTTL := snapshotIndexTTL(validSessionExpiry.UnixMilli(), now)
	if indexTTL <= remaining {
		t.Fatalf("user index expiry would evict a valid session: index=%s retry=%s", indexTTL, remaining)
	}
}

func TestRecommendationSnapshotMaintenanceFailuresKeepCommittedSnapshot(t *testing.T) {
	snapshot := &applicationrecommendation.Snapshot{
		ID: "committed-snapshot", UserID: 7, Scene: "recommend", RequestID: "request", PolicyVersion: 1,
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	}
	content, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name       string
		pExpireErr error
		zAddErr    error
	}{
		{name: "request key expiry", pExpireErr: errors.New("request key expiry failed"), zAddErr: errors.New("user index update skipped")},
		{name: "user index", zAddErr: errors.New("user index update failed")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store := NewRecommendationSnapshotStore(&committedSnapshotFakeRedis{
				evalResult: []any{int64(1), string(content)},
				pExpireErr: testCase.pExpireErr,
				zAddErr:    testCase.zAddErr,
			})
			stored, created, err := store.CreateSnapshot(context.Background(), snapshot, time.Minute)
			if err != nil {
				t.Fatalf("committed snapshot was discarded after maintenance failure: %v", err)
			}
			if !created || stored == nil || stored.ID != snapshot.ID || stored.RequestID != snapshot.RequestID {
				t.Fatalf("unexpected authoritative snapshot result: created=%v snapshot=%#v", created, stored)
			}
		})
	}
}
