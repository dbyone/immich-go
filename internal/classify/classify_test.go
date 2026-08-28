package classify

import (
	"context"
	"errors"
	"testing"

	"immich-go/internal/ml"
)

// fakeProvider embeds by lookup table so label/image similarities are
// fully deterministic in tests.
type fakeProvider struct {
	texts map[string][]float32
	calls int
}

func (f *fakeProvider) Name() string        { return "immich" }
func (f *fakeProvider) SupportsCLIP() bool  { return true }
func (f *fakeProvider) SupportsFaces() bool { return true }
func (f *fakeProvider) SupportsOCR() bool   { return true }
func (f *fakeProvider) EncodeImage(ctx context.Context, p, m string) ([]float32, error) {
	return nil, errors.New("unused")
}
func (f *fakeProvider) EncodeText(_ context.Context, text string, _ ml.TextOptions) ([]float32, error) {
	f.calls++
	if v, ok := f.texts[text]; ok {
		return v, nil
	}
	return []float32{0.001, 0.002}, nil
}
func (f *fakeProvider) DetectFaces(ctx context.Context, p string, o ml.FaceDetectionOptions) (*ml.FaceDetectionResult, error) {
	return nil, errors.New("unused")
}
func (f *fakeProvider) OCR(ctx context.Context, p string, o ml.OCROptions) (*ml.OCRResult, error) {
	return nil, errors.New("unused")
}
func (f *fakeProvider) Ping(u string) bool { return true }
func (f *fakeProvider) Teardown()          {}

func TestClassifyPicksMatchingLabels(t *testing.T) {
	// The EN texts for 海滩 and 日落 point at the image vector; everything
	// else lands near zero.
	prov := &fakeProvider{texts: map[string][]float32{
		"beach":  {1, 0},
		"sunset": {0.9, 0.1},
		"train":  {0, 0.1},
		"comic":  {-1, 0},
	}}
	c := New(prov, Options{Threshold: 0.8, TopK: 3})

	got, err := c.Classify(context.Background(), []float32{1, 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 labels, got %+v", got)
	}
	if got[0].Label.ZH != "海滩" {
		t.Fatalf("best label = %q, want 海滩", got[0].Label.ZH)
	}
	if got[1].Label.ZH != "日落" {
		t.Fatalf("second label = %q, want 日落", got[1].Label.ZH)
	}
	if got[0].Score < got[1].Score {
		t.Fatalf("scores not descending: %+v", got)
	}
}

func TestClassifyTopKAndThreshold(t *testing.T) {
	prov := &fakeProvider{texts: map[string][]float32{
		"beach":  {1, 0},
		"sunset": {1, 0},
		"forest": {1, 0},
		"train":  {1, 0},
	}}
	c := New(prov, Options{Threshold: 0.5, TopK: 2})
	got, err := c.Classify(context.Background(), []float32{1, 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("TopK=2 must cap results, got %d", len(got))
	}
}

func TestClassifyCachesLabelEmbeddings(t *testing.T) {
	prov := &fakeProvider{texts: map[string][]float32{"beach": {1, 0}}}
	c := New(prov, Options{Threshold: 0.9, TopK: 1})
	for i := 0; i < 3; i++ {
		if _, err := c.Classify(context.Background(), []float32{1, 0}); err != nil {
			t.Fatal(err)
		}
	}
	if prov.calls > len(DefaultTaxonomy) {
		t.Fatalf("label embeddings must be cached, got %d calls for %d labels", prov.calls, len(DefaultTaxonomy))
	}
}

func TestClassifyChineseLabelsForMTPhotos(t *testing.T) {
	prov := &fakeProvider{texts: map[string][]float32{"海滩": {1, 0}}}
	prov2 := &mtFake{prov}
	c := New(prov2, Options{Threshold: 0.9, TopK: 1})
	got, err := c.Classify(context.Background(), []float32{1, 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Label.ZH != "海滩" {
		t.Fatalf("mtphotos dialect must embed ZH labels, got %+v", got)
	}
}

type mtFake struct{ inner *fakeProvider }

func (m *mtFake) Name() string        { return "mtphotos" }
func (m *mtFake) SupportsCLIP() bool  { return true }
func (m *mtFake) SupportsFaces() bool { return false }
func (m *mtFake) SupportsOCR() bool   { return true }
func (m *mtFake) EncodeImage(ctx context.Context, p, mm string) ([]float32, error) {
	return m.inner.EncodeImage(ctx, p, mm)
}
func (m *mtFake) EncodeText(ctx context.Context, text string, o ml.TextOptions) ([]float32, error) {
	return m.inner.EncodeText(ctx, text, o)
}
func (m *mtFake) DetectFaces(ctx context.Context, p string, o ml.FaceDetectionOptions) (*ml.FaceDetectionResult, error) {
	return nil, errors.New("unused")
}
func (m *mtFake) OCR(ctx context.Context, p string, o ml.OCROptions) (*ml.OCRResult, error) {
	return nil, errors.New("unused")
}
func (m *mtFake) Ping(u string) bool { return true }
func (m *mtFake) Teardown()          {}

func TestTaxonomySanity(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range DefaultTaxonomy {
		if l.ZH == "" || l.EN == "" {
			t.Fatalf("incomplete label %+v", l)
		}
		if seen[l.ZH] {
			t.Fatalf("duplicate ZH label %q", l.ZH)
		}
		seen[l.ZH] = true
	}
	if len(DefaultTaxonomy) < 60 {
		t.Fatalf("taxonomy too small: %d", len(DefaultTaxonomy))
	}
}
