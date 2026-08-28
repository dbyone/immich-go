// Package classify implements the MT-Photos-inspired zero-shot scene
// tagger. It reuses the CLIP embedding already stored for smart search:
// the image vector is scored against a fixed bilingual taxonomy of scene
// labels (embedded once through the configured AI provider), and the
// top-K labels above a threshold become hierarchical "场景/<label>"
// tags. No extra inference runs per asset beyond the one-time label
// embedding pass.
package classify

import (
	"context"
	"math"
	"sort"
	"sync"

	"immich-go/internal/ml"
)

// Label is one taxonomy entry. ZH is the tag text stored on the asset;
// EN is the text fed to English-centric CLIP models (the immich
// dialect). The mtphotos dialect embeds ZH directly — Chinese-CLIP's
// native strength.
type Label struct {
	ZH string
	EN string
}

// DefaultTaxonomy is the built-in scene vocabulary.
var DefaultTaxonomy = []Label{
	// landscapes & nature
	{"海滩", "beach"}, {"山脉", "mountain"}, {"雪景", "snow scene"}, {"森林", "forest"},
	{"湖泊", "lake"}, {"河流", "river"}, {"瀑布", "waterfall"}, {"沙漠", "desert"},
	{"草原", "grassland"}, {"田园", "countryside fields"}, {"海景", "sea view"},
	{"岛屿", "island"}, {"洞穴", "cave"}, {"峡谷", "canyon"}, {"火山", "volcano"},
	{"星空", "starry sky"}, {"极光", "aurora"}, {"彩虹", "rainbow"}, {"蓝天白云", "blue sky with clouds"},
	{"雨天", "rainy day"}, {"雾景", "foggy scene"}, {"闪电", "lightning"}, {"日落", "sunset"},
	{"日出", "sunrise"}, {"夜景", "night scene"}, {"月亮", "moon"}, {"云海", "sea of clouds"},
	// plants & animals
	{"花朵", "flowers"}, {"樱花", "cherry blossoms"}, {"向日葵", "sunflowers"},
	{"红叶", "autumn leaves"}, {"绿植", "green plants"}, {"宠物", "pets"},
	{"猫", "cat"}, {"狗", "dog"}, {"鸟类", "birds"}, {"昆虫", "insects"},
	{"海洋生物", "marine life"}, {"野生动物", "wild animals"},
	// urban & architecture
	{"城市街景", "city street"}, {"城市天际线", "city skyline"}, {"古镇", "ancient town"},
	{"寺庙", "temple"}, {"教堂", "church"}, {"城堡", "castle"}, {"桥梁", "bridge"},
	{"现代建筑", "modern architecture"}, {"园林", "classical garden"}, {"涂鸦", "graffiti"},
	{"霓虹灯", "neon lights"},
	// activities
	{"婚礼", "wedding"}, {"生日", "birthday"}, {"毕业典礼", "graduation ceremony"},
	{"演唱会", "concert"}, {"演出", "stage performance"}, {"展览", "exhibition"},
	{"运动比赛", "sports match"}, {"健身", "workout"}, {"跑步", "running"},
	{"骑行", "cycling"}, {"游泳", "swimming"}, {"滑雪", "skiing"}, {"爬山", "hiking"},
	{"露营", "camping"}, {"钓鱼", "fishing"}, {"划船", "boating"}, {"冲浪", "surfing"},
	{"自驾", "road trip"}, {"旅行", "travel"}, {"野餐", "picnic"}, {"聚会", "party"},
	{"游乐园", "amusement park"}, {"动物园", "zoo"}, {"水族馆", "aquarium"},
	{"博物馆", "museum"}, {"美术馆", "art gallery"}, {"购物", "shopping"},
	// food & drink
	{"美食", "food"}, {"中餐", "chinese food"}, {"西餐", "western food"},
	{"日料", "japanese food"}, {"火锅", "hotpot"}, {"烧烤", "barbecue"},
	{"烘焙", "baking"}, {"甜品", "dessert"}, {"咖啡", "coffee"}, {"饮品", "drinks"},
	{"水果", "fruit"},
	// people
	{"人像", "portrait"}, {"合影", "group photo"}, {"儿童", "children"},
	{"家庭", "family"}, {"情侣", "couple"}, {"街拍人像", "street portrait"},
	// interiors & objects
	{"室内", "interior"}, {"客厅", "living room"}, {"卧室", "bedroom"},
	{"厨房", "kitchen"}, {"办公室", "office"}, {"教室", "classroom"},
	{"酒店", "hotel room"}, {"咖啡馆内景", "cafe interior"}, {"书店", "bookstore"},
	{"书架", "bookshelf"},
	// transport
	{"汽车", "car"}, {"摩托车", "motorcycle"}, {"自行车", "bicycle"},
	{"火车", "train"}, {"飞机", "airplane"}, {"机场", "airport"}, {"轮船", "ship"},
	{"车站", "station"},
	// documents & screenshots
	{"文档", "document"}, {"屏幕截图", "screenshot"}, {"手写笔记", "handwritten notes"},
	{"合同票据", "receipt or invoice"}, {"证件", "id document"}, {"漫画", "comic"},
	{"表情包", "meme"}, {"二维码", "qr code"}, {"海报", "poster"},
	// special formats
	{"全景照片", "panorama"}, {"微距", "macro photography"}, {"黑白照片", "black and white photo"},
	{"逆光", "backlit photo"}, {"烟花", "fireworks"}, {"倒影", "reflection"},
}

// LabelScore is one scored taxonomy hit.
type LabelScore struct {
	Label Label
	Score float64 // cosine similarity in [-1, 1]
}

// Options tune classification.
type Options struct {
	Threshold float64
	TopK      int
}

// Classifier scores image embeddings against the taxonomy. Label
// embeddings are computed lazily once per process through the provider.
type Classifier struct {
	prov ml.Provider
	opts Options
	tax  []Label

	mu       sync.Mutex
	embedded bool
	matrix   [][]float32 // normalized label vectors, aligned with tax
	norms    []float32
}

// New builds a classifier over the taxonomy for the given provider.
func New(prov ml.Provider, opts Options) *Classifier {
	if opts.TopK <= 0 {
		opts.TopK = 3
	}
	return &Classifier{prov: prov, opts: opts, tax: DefaultTaxonomy}
}

// Texter abstracts which language string is embedded per label.
func labelText(prov ml.Provider, l Label) string {
	if prov != nil && prov.Name() == "mtphotos" {
		return l.ZH
	}
	return l.EN
}

// ensureLabels embeds the taxonomy once (concurrent callers wait).
func (c *Classifier) ensureLabels(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.embedded {
		return nil
	}
	matrix := make([][]float32, len(c.tax))
	norms := make([]float32, len(c.tax))
	for i, l := range c.tax {
		vec, err := c.prov.EncodeText(ctx, labelText(c.prov, l), ml.TextOptions{})
		if err != nil {
			return err
		}
		matrix[i] = vec
		norms[i] = l2norm(vec)
	}
	c.matrix = matrix
	c.norms = norms
	c.embedded = true
	return nil
}

func l2norm(v []float32) float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return float32(math.Sqrt(sum))
}

// Classify scores one image embedding and returns the labels at or above
// the threshold, best first, capped at TopK.
func (c *Classifier) Classify(ctx context.Context, image []float32) ([]LabelScore, error) {
	if c.prov == nil || !c.prov.SupportsCLIP() {
		return nil, ml.ErrUnsupported
	}
	if err := c.ensureLabels(ctx); err != nil {
		return nil, err
	}
	imgNorm := l2norm(image)
	if imgNorm == 0 {
		return nil, nil
	}
	scores := make([]LabelScore, 0, len(c.tax))
	c.mu.Lock()
	for i := range c.tax {
		if c.norms[i] == 0 {
			continue
		}
		var dot float64
		for j, x := range image {
			if j < len(c.matrix[i]) {
				dot += float64(x) * float64(c.matrix[i][j])
			}
		}
		sim := dot / (float64(imgNorm) * float64(c.norms[i]))
		if sim >= c.opts.Threshold {
			scores = append(scores, LabelScore{Label: c.tax[i], Score: sim})
		}
	}
	c.mu.Unlock()
	sort.SliceStable(scores, func(i, j int) bool { return scores[i].Score > scores[j].Score })
	if len(scores) > c.opts.TopK {
		scores = scores[:c.opts.TopK]
	}
	return scores, nil
}
