package vectordb

import (
	"context"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:", 3)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSmartSearchSQL(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	// Three distinct directions; query matches the first.
	vecs := map[string][]float32{
		"a": {1, 0, 0},
		"b": {0, 1, 0},
		"c": {0.5, 0.5, 0.7071},
	}
	for id, v := range vecs {
		if err := s.UpsertSmartSearch(ctx, id, "u1", "m", v); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	hits, err := s.SearchSmart(ctx, "u1", []float32{1, 0, 0}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 || hits[0].AssetID != "a" {
		t.Fatalf("unexpected ranking: %+v", hits)
	}
	if hits[0].Score < 0.999 {
		t.Fatalf("identical vectors should score ~1, got %f", hits[0].Score)
	}
	// Orthogonal vector must not appear in top results ranked first.
	if hits[1].AssetID == "b" && hits[1].Score != 0 {
		t.Fatalf("orthogonal score should be 0: %+v", hits[1])
	}

	// Owner isolation.
	hits, err = s.SearchSmart(ctx, "nobody", []float32{1, 0, 0}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("other owner should see no results: %+v", hits)
	}

	// Upsert overwrites.
	if err := s.UpsertSmartSearch(ctx, "a", "u1", "m", []float32{0, 1, 0}); err != nil {
		t.Fatal(err)
	}
	hits, _ = s.SearchSmart(ctx, "u1", []float32{0, 1, 0}, 2)
	if len(hits) != 2 { // a and b now share the same vector
		t.Fatalf("upsert did not overwrite: %+v", hits)
	}
	seen := map[string]bool{}
	for _, h := range hits {
		seen[h.AssetID] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("both a and b should match: %+v", hits)
	}

	if err := s.DeleteSmartSearch(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	smart, _, _, _ := s.Counts(ctx)
	if smart != 2 {
		t.Fatalf("expected 2 rows after delete, got %d", smart)
	}

	// Dimension guard.
	if err := s.UpsertSmartSearch(ctx, "bad", "u1", "m", []float32{1, 2}); err == nil {
		t.Fatal("dimension mismatch must be rejected")
	}
}

func TestDBSCAN(t *testing.T) {
	// Two tight groups plus a loner.
	vectors := [][]float32{
		{1, 0, 0}, {0.99, 0.02, 0}, {0.98, 0.01, 0.01}, // cluster A
		{0, 1, 0}, {0.01, 0.99, 0}, // cluster B
		{0, 0, 1}, // noise
	}
	labels := DBSCAN(vectors, 0.1, 2)
	if labels[0] != labels[1] || labels[1] != labels[2] {
		t.Fatalf("cluster A split: %v", labels)
	}
	if labels[3] != labels[4] {
		t.Fatalf("cluster B split: %v", labels)
	}
	if labels[0] == labels[3] {
		t.Fatalf("A and B merged: %v", labels)
	}
	if labels[5] != -1 {
		t.Fatalf("loner should be noise: %v", labels)
	}
}

func TestClusterFaces(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	// Faces of "Alice" on 4 assets, "Bob" on 3, one unknown on 1.
	alice := []float32{1, 0, 0}
	bob := []float32{0, 1, 0}
	uploadFaces := func(asset string, vec []float32) FaceRow {
		return FaceRow{AssetID: asset, Vec: vec}
	}
	aliceFaces := []FaceRow{
		uploadFaces("a1", alice), uploadFaces("a2", alice),
		uploadFaces("a3", alice), uploadFaces("a4", alice),
	}
	bobFaces := []FaceRow{
		uploadFaces("b1", bob), uploadFaces("b2", bob), uploadFaces("b3", bob),
	}
	if err := s.UpsertFaces(ctx, "u1", "a-assets", aliceFaces); err != nil {
		t.Fatal(err)
	}
	// Split across two upserts to exercise per-asset replacement.
	if err := s.UpsertFaces(ctx, "u1", "b-assets", bobFaces[:2]); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFaces(ctx, "u1", "b-extra", bobFaces[2:]); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFaces(ctx, "u1", "loner", []FaceRow{uploadFaces("l1", []float32{0, 0, 1})}); err != nil {
		t.Fatal(err)
	}

	people, err := s.ClusterFaces(ctx, "u1", 0.5, 3)
	if err != nil {
		t.Fatal(err)
	}
	if people != 2 {
		t.Fatalf("expected 2 people (Alice, Bob), got %d", people)
	}

	persons, err := s.ListPersons(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(persons) != 2 {
		t.Fatalf("expected 2 persons listed, got %+v", persons)
	}
	total := 0
	for _, p := range persons {
		total += p.FaceCount
	}
	if total != 7 {
		t.Fatalf("face counts should sum to 7, got %+v", persons)
	}

	// The loner stays unassigned.
	faces, _ := s.LoadFaces(ctx, "u1")
	for _, f := range faces {
		if f.AssetID == "l1" && f.PersonID != "" {
			t.Fatalf("loner was assigned a person: %+v", f)
		}
	}

	// Re-clustering is stable: same person ids survive.
	before := map[string]int{}
	for _, p := range persons {
		before[p.ID] = p.FaceCount
	}
	people, err = s.ClusterFaces(ctx, "u1", 0.5, 3)
	if err != nil {
		t.Fatal(err)
	}
	if people != 2 {
		t.Fatalf("recluster expected 2 people, got %d", people)
	}
	after, _ := s.ListPersons(ctx, "u1")
	for _, p := range after {
		if before[p.ID] != p.FaceCount {
			t.Fatalf("person %s changed face count %d -> %d", p.ID, before[p.ID], p.FaceCount)
		}
	}

	// Raising minFaces to 5 dissolves both clusters; empty unnamed persons
	// are cleaned up.
	if _, err := s.ClusterFaces(ctx, "u1", 0.5, 5); err != nil {
		t.Fatal(err)
	}
	persons, _ = s.ListPersons(ctx, "u1")
	if len(persons) != 0 {
		t.Fatalf("clusters below minFaces must be dissolved, got %+v", persons)
	}
}

func TestDetectDuplicates(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	// Two bursts of identical shots (small rotations) + one unique.
	bursts := [][]float32{
		{1, 0, 0}, {0.999, 0.01, 0}, {0.998, 0.02, 0}, // burst 1
		{0, 1, 0}, {0.01, 0.999, 0}, // burst 2
	}
	for i, v := range bursts {
		if err := s.UpsertSmartSearch(ctx, "asset-"+string(rune('a'+i)), "u1", "m", v); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpsertSmartSearch(ctx, "unique", "u1", "m", []float32{0, 0, 1}); err != nil {
		t.Fatal(err)
	}

	groups, err := s.DetectDuplicates(ctx, "u1", 0.05)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 duplicate groups, got %+v", groups)
	}
	sizes := map[int]int{}
	for _, g := range groups {
		sizes[len(g)]++
	}
	if sizes[2] != 1 || sizes[3] != 1 {
		t.Fatalf("group sizes wrong (want one 2-group and one 3-group): %+v", groups)
	}
	for _, g := range groups {
		for _, id := range g {
			if id == "unique" {
				t.Fatalf("unique asset must not be grouped: %+v", groups)
			}
		}
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	path := t.TempDir() + "/vectors.duckdb"
	s, err := Open(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.UpsertSmartSearch(ctx, "x", "u1", "m", []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(path, 3)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	hits, err := s2.SearchSmart(ctx, "u1", []float32{1, 0, 0}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].AssetID != "x" {
		t.Fatalf("vectors must survive reopen: %+v", hits)
	}
}

func TestGoFallbackRanking(t *testing.T) {
	// RankByCosine is the fallback path when array_cosine_similarity is
	// unavailable; validate ordering directly.
	type item struct {
		id  string
		vec []float32
	}
	items := []item{
		{"b", []float32{0, 1, 0}},
		{"a", []float32{1, 0, 0}},
	}
	hits := RankByCosine([]float32{1, 0, 0}, items, func(it item) (string, []float32) { return it.id, it.vec })
	if len(hits) != 2 || hits[0].AssetID != "a" || hits[1].Score != 0 {
		t.Fatalf("fallback ranking wrong: %+v", hits)
	}
}
