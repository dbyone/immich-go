package vectordb

import (
	"context"
	"sort"
	"time"

	"immich-go/internal/crypto"
	"immich-go/internal/ml"
)

// RankByCosine sorts (id, vector) pairs by cosine similarity to query,
// descending — the Go-side fallback of the SQL ranking path.
func RankByCosine[T any](query []float32, items []T, extract func(T) (string, []float32)) []SmartHit {
	type pair struct {
		hit SmartHit
	}
	pairs := make([]pair, 0, len(items))
	for _, it := range items {
		id, vec := extract(it)
		pairs = append(pairs, pair{SmartHit{AssetID: id, Score: ml.CosineSimilarity(query, vec)}})
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].hit.Score > pairs[j].hit.Score })
	out := make([]SmartHit, len(pairs))
	for i, p := range pairs {
		out[i] = p.hit
	}
	return out
}

// DBSCAN labels points with cluster ids; -1 means noise. Distance is
// cosine distance (1 - similarity) and eps is that distance threshold.
// This mirrors the upstream facial-recognition clustering semantics:
// dense groups of face embeddings become people, lone faces stay
// unassigned.
func DBSCAN(vectors [][]float32, eps float64, minPts int) []int {
	n := len(vectors)
	labels := make([]int, n)
	for i := range labels {
		labels[i] = -1 // noise until proven otherwise
	}
	visited := make([]bool, n)
	neighbours := func(p int) []int {
		var out []int
		for q := 0; q < n; q++ {
			if q == p {
				out = append(out, q) // self counts toward density
				continue
			}
			if 1-ml.CosineSimilarity(vectors[p], vectors[q]) <= eps {
				out = append(out, q)
			}
		}
		return out
	}

	cluster := 0
	for p := 0; p < n; p++ {
		if visited[p] {
			continue
		}
		visited[p] = true
		nbrs := neighbours(p)
		if len(nbrs) < minPts {
			continue // stays noise
		}
		labels[p] = cluster
		// Seed expansion (queue-based, borders join but do not expand).
		queue := append([]int{}, nbrs...)
		for len(queue) > 0 {
			q := queue[0]
			queue = queue[1:]
			if labels[q] == -1 {
				labels[q] = cluster
			}
			if visited[q] {
				continue
			}
			visited[q] = true
			qNbrs := neighbours(q)
			if len(qNbrs) >= minPts { // core point -> expand
				queue = append(queue, qNbrs...)
			}
		}
		cluster++
	}
	return labels
}

// ClusterFaces runs DBSCAN over the owner's face embeddings, assigns
// person ids (reusing existing persons for continuity) and persists the
// results. Returns the number of people found.
func (s *Store) ClusterFaces(ctx context.Context, ownerID string, maxDistance float64, minFaces int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if maxDistance <= 0 {
		maxDistance = 0.5
	}
	if minFaces <= 0 {
		minFaces = 3
	}

	faces, err := s.LoadFaces(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	if len(faces) == 0 {
		return 0, s.recountPersons(ctx, ownerID)
	}

	vectors := make([][]float32, len(faces))
	for i, f := range faces {
		vectors[i] = f.Vec
	}
	labels := DBSCAN(vectors, maxDistance, minFaces)

	// Group faces by cluster label, keeping any pre-existing person id for
	// stable identity across runs.
	type clusterGroup struct {
		personID string
		faces    []int // indices into faces
	}
	groups := map[int]*clusterGroup{}
	for i, label := range labels {
		if label < 0 {
			continue
		}
		g, ok := groups[label]
		if !ok {
			g = &clusterGroup{}
			groups[label] = g
		}
		if faces[i].PersonID != "" && g.personID == "" {
			g.personID = faces[i].PersonID
		}
		g.faces = append(g.faces, i)
	}

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Clear every assignment first, then write the new clusters. Old
	// person ids were already captured above, so identity stays stable;
	// persons whose clusters dissolved are removed by recountPersons.
	if _, err := tx.ExecContext(ctx,
		`UPDATE face_search SET person_id = NULL WHERE owner_id = ?`, ownerID); err != nil {
		return 0, err
	}

	for _, g := range groups {
		if g.personID == "" {
			g.personID = crypto.NewUUID()
		}
		thumbnail := faces[g.faces[0]].AssetID
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO person (id, owner_id, name, is_hidden, is_favorite, face_count, thumbnail_asset_id, created_at, updated_at)
			VALUES (?, ?, '', FALSE, FALSE, ?, ?, ?, ?)
			ON CONFLICT (id) DO UPDATE SET
				face_count = excluded.face_count,
				thumbnail_asset_id = excluded.thumbnail_asset_id,
				updated_at = excluded.updated_at`,
			g.personID, ownerID, len(g.faces), thumbnail, now, now); err != nil {
			return 0, err
		}
		for _, i := range g.faces {
			if _, err := tx.ExecContext(ctx,
				`UPDATE face_search SET person_id = ? WHERE asset_id = ? AND face_idx = ?`,
				g.personID, faces[i].AssetID, faces[i].FaceIdx); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	if err := s.recountPersons(ctx, ownerID); err != nil {
		return 0, err
	}
	return len(groups), nil
}

// recountPersons keeps person.face_count in sync and removes empty
// unnamed people left behind by re-clustering.
func (s *Store) recountPersons(ctx context.Context, ownerID string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE person SET face_count =
			(SELECT COUNT(*) FROM face_search f WHERE f.person_id = person.id)
		WHERE owner_id = ?`, ownerID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM person
		WHERE owner_id = ? AND face_count = 0 AND name = '' AND is_favorite = FALSE`,
		ownerID)
	return err
}

// DetectDuplicates finds groups of visually near-identical assets by
// comparing CLIP embeddings: pairs with cosine distance below
// maxDistance are merged into groups via union-find. This replaces the
// upstream duplicateDetection queue backed by pgvector.
func (s *Store) DetectDuplicates(ctx context.Context, ownerID string, maxDistance float64) ([][]string, error) {
	if maxDistance <= 0 {
		maxDistance = 0.01
	}

	type pair struct{ a, b string }
	var pairs []pair

	if s.hasCosineFn {
		rows, err := s.db.QueryContext(ctx, `
			SELECT a.asset_id, b.asset_id
			FROM smart_search a
			JOIN smart_search b
			  ON a.owner_id = b.owner_id AND a.asset_id < b.asset_id
			WHERE a.owner_id = ?
			  AND 1.0 - array_cosine_similarity(a.embedding, b.embedding) < ?`,
			ownerID, maxDistance)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var p pair
			if err := rows.Scan(&p.a, &p.b); err != nil {
				return nil, err
			}
			pairs = append(pairs, p)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	} else {
		entries, err := s.loadSmart(ctx, ownerID)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(entries))
		for id := range entries {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				d := 1 - ml.CosineSimilarity(entries[ids[i]], entries[ids[j]])
				if d < maxDistance {
					pairs = append(pairs, pair{ids[i], ids[j]})
				}
			}
		}
	}

	// Union-find over the pair graph.
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		if p, ok := parent[x]; !ok || p == x {
			parent[x] = x
			return x
		}
		root := find(parent[x])
		parent[x] = root
		return root
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	for _, p := range pairs {
		union(p.a, p.b)
	}

	groups := map[string][]string{}
	for id := range parent {
		groups[find(id)] = append(groups[find(id)], id)
	}

	var out [][]string
	for _, members := range groups {
		if len(members) >= 2 {
			sort.Strings(members)
			out = append(out, members)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out, nil
}
