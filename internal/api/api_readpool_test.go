package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"immich-go/internal/config"
)

// TestFileBackedReadPoolSmoke runs the whole app against a file-backed
// DuckDB with a separate read pool: writes must be immediately visible to
// reader connections, and concurrent reads must not deadlock behind the
// writer.
// tempDirWinSafe replaces t.TempDir when the directory holds a DuckDB
// file: database/sql closes busy connections asynchronously, so on
// Windows the file may stay locked for a moment after App.Close. Removal
// retries in the background instead of failing the test.
func tempDirWinSafe(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "immich-readpool-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Logf("could not remove temp dir (still locked): %s", dir)
	})
	return dir
}

func TestFileBackedReadPoolSmoke(t *testing.T) {
	dir := tempDirWinSafe(t)
	h, a := newTestServerApp(t, func(cfg *config.Config) {
		cfg.DuckDBPath = filepath.Join(dir, "immich.duckdb")
		cfg.DuckDBReaders = 4
	})
	token := loginForTest(t, h, "pool@t.c")

	ids := make([]string, 5)
	for i := range ids {
		ids[i] = uploadForTest(t, h, token, testJPEG(t, i+1), "p.jpg")
	}

	// Concurrent reads while a write stream runs. Paced (not a spin
	// loop): the goal is proving the pools never deadlock and that reads
	// keep working during writes, not surviving a load test.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				req := httptest.NewRequest(http.MethodGet, "/api/timeline/buckets", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != 200 {
					t.Errorf("concurrent read failed: %d", rec.Code)
					return
				}
				select {
				case <-stop:
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}()
	}
	_ = a // app handle kept for future seeding
	// Mark favorites through the writer pool while readers spin.
	for _, id := range ids {
		code, _ := doJSON(t, h, http.MethodPut, "/api/assets/"+id, token, map[string]any{"isFavorite": true})
		if code != 200 {
			t.Fatalf("update during concurrent reads: %d", code)
		}
	}
	close(stop)
	wg.Wait()

	// Read-your-writes on the read pool.
	_, body := doJSON(t, h, http.MethodPost, "/api/search/metadata", token, map[string]any{"isFavorite": true})
	if n := len(asMap(t, body)["assets"].([]any)); n != 5 {
		t.Fatalf("favorites after concurrent phase: %d", n)
	}
}
