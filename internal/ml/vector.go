package ml

import (
	"encoding/json"
	"errors"
	"math"
)

// DecodeVector parses the embedding string produced by immich-machine-
// learning. Models serialize their float32 numpy arrays with orjson, which
// emits a plain JSON number array — the wire value is therefore a string
// like "[0.123,-0.456,...]", not base64.
func DecodeVector(s string) ([]float32, error) {
	var v []float32
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	if len(v) == 0 {
		return nil, errors.New("empty embedding")
	}
	return v, nil
}

// CosineSimilarity computes the cosine distance metric the upstream server
// uses for smart search (vector_cosine_ops / <=>).
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return math.Inf(-1)
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return math.Inf(-1)
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
