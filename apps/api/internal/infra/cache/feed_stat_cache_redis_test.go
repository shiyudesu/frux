package infracache

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Exercise real WATCH/EXEC semantics when redis-server is installed. The child
// Redis is private to this test: no TCP listener, no persistence, no external DB.
func TestFeedStatRefreshRejectsStalePublicationRedisServer(t *testing.T) {
	if testing.Short() {
		t.Skip("real Redis subprocess test")
	}
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is not installed; miniredis regression still runs")
	}
	testFeedStatPublicationScenarios(t, func(t *testing.T) *redis.Options {
		// A short path avoids Unix socket path limits for long subtest names.
		dir, err := os.MkdirTemp("", "frux-stat-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		socket := filepath.Join(dir, "redis.sock")
		cmd := exec.Command(binary, "--port", "0", "--unixsocket", socket,
			"--unixsocketperm", "700", "--save", "", "--appendonly", "no", "--dir", dir)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Signal(os.Interrupt)
			timer := time.AfterFunc(2*time.Second, func() { _ = cmd.Process.Kill() })
			_ = cmd.Wait()
			timer.Stop()
		})
		options := &redis.Options{Network: "unix", Addr: socket, MaxRetries: -1}
		probeOptions := *options
		probe := redis.NewClient(&probeOptions)
		defer probe.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if err := probe.Ping(ctx).Err(); err == nil {
				return options
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				t.Fatal("private Redis did not become ready")
			}
		}
	})
}
