package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func doRaw(t *testing.T, h http.Handler, method, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

// TestEmbeddedWebUI pins the SPA hosting contract: the embedded immich
// web build is served at the root, deep links fall back to index.html,
// and /api keeps its JSON 404 semantics.
func TestEmbeddedWebUI(t *testing.T) {
	h := newTestServer(t)

	// Root serves the SPA entry point.
	res := doRaw(t, h, http.MethodGet, "/")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /: %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(strings.ToLower(string(body)), "<!doctype html") {
		t.Fatalf("GET / did not return the SPA entry point: %.120s", body)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET / content-type = %q", ct)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("index must revalidate, cache-control = %q", cc)
	}

	// A real asset from the build tree serves with immutable caching.
	res = doRaw(t, h, http.MethodGet, "/favicon.ico")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /favicon.ico: %d (embedded assets missing?)", res.StatusCode)
	}

	// Deep links fall back to index.html for client-side routing.
	res = doRaw(t, h, http.MethodGet, "/photos")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /photos: %d", res.StatusCode)
	}
	body, _ = io.ReadAll(res.Body)
	if !strings.Contains(strings.ToLower(string(body)), "<!doctype html") {
		t.Fatal("SPA fallback missing on deep link")
	}

	// The API namespace keeps JSON 404s — the SPA never shadows it.
	res = doRaw(t, h, http.MethodGet, "/api/definitely-not-a-route")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /api/...: %d", res.StatusCode)
	}
	body, _ = io.ReadAll(res.Body)
	if !strings.Contains(string(body), "Route not found") {
		t.Fatalf("API 404 body changed: %.120s", body)
	}

	// Non-GET on the SPA surface is rejected.
	res = doRaw(t, h, http.MethodPost, "/photos")
	if res.StatusCode == http.StatusOK {
		t.Fatal("POST to a client route must not serve HTML")
	}
}
