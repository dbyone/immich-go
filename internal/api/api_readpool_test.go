package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"immich-go/internal/config"
)

// TestFileBackedReadPoolSmoke runs the whole app against a file-backed
// DuckDB with a separate read pool: writes must be immediately visible to
// reader connections, and concurrent reads must not deadlock behind the
// writer.
func TestFileBackedReadPoolSmoke(t *testing.T) {
	dir := t.TempDir()
	h, a := newTestServerApp(t, func(cfg *config.Config) {
		cfg.DuckDBPath = filepath.Join(dir, "immich.duckdb")
		cfg.DuckDBReaders = 4
	})
	token := loginForTest(t, h, "pool@t.c")

	ids := make([]string, 5)
	for i := range ids {
		ids[i] = uploadForTest(t, h, token, testJPEG(t, i+1), "p.jpg")
	}

	// Concurrent reads while a write stream runs.
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
