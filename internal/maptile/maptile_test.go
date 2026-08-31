package maptile

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestWGS84ToGCJ02KnownPoints(t *testing.T) {
	// Tiananmen: WGS-84 39.9073, 116.3912 -> GCJ-02 ≈ 39.9087, 116.3975
	// (the canonical public test vector, ~500m offset).
	lat, lng := wgs84ToGCJ02(39.9073, 116.3912)
	if math.Abs(lat-39.9087) > 0.0005 || math.Abs(lng-116.3975) > 0.0005 {
		t.Fatalf("tiananmen: got %.6f,%.6f", lat, lng)
	}
}

func TestFixCoordOnlyInsideChina(t *testing.T) {
	// Domestic point shifts onto the AMap datum.
	lat, lng := FixCoord(ProviderAMap, 39.9073, 116.3912)
	if lat == 39.9073 || lng == 116.3912 {
		t.Fatal("domestic coordinate must shift in amap mode")
	}
	// Foreign point (London) stays untouched — the datums agree there.
	if lat, lng := FixCoord(ProviderAMap, 51.5007, -0.1246); lat != 51.5007 || lng != -0.1246 {
		t.Fatalf("foreign coordinate must not shift: %.6f,%.6f", lat, lng)
	}
	// OSM provider is always identity.
	if lat, lng := FixCoord(ProviderOSM, 39.9073, 116.3912); lat != 39.9073 || lng != 116.3912 {
		t.Fatal("osm must be identity")
	}
}

func TestStyleDocuments(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		doc := amapStyle(theme)
		var style map[string]any
		if err := json.Unmarshal([]byte(doc), &style); err != nil {
			t.Fatalf("amap %s style invalid json: %v", theme, err)
		}
		src := style["sources"].(map[string]any)["basemap"].(map[string]any)
		tiles := src["tiles"].([]any)
		if len(tiles) != 4 {
			t.Fatalf("amap style should rotate 4 tile hosts, got %d", len(tiles))
		}
		if !strings.Contains(tiles[0].(string), "autonavi.com") {
			t.Fatalf("amap tiles host = %v", tiles[0])
		}
		wantStyle := "7"
		if theme == "dark" {
			wantStyle = "6"
		}
		if !strings.Contains(tiles[0].(string), "style="+wantStyle) {
			t.Fatalf("amap %s style id mismatch: %v", theme, tiles[0])
		}
	}
	if doc := osmStyle("light"); !strings.Contains(doc, "openstreetmap.org") {
		t.Fatal("osm style should point at OSM tiles")
	}
}

func TestStyleURLRouting(t *testing.T) {
	if u := StyleURL("osm", "light", "http://x:1"); !strings.Contains(u, "tiles.immich.cloud") {
		t.Fatalf("osm url = %v", u)
	}
	if u := StyleURL("amap", "dark", "http://x:1"); !strings.Contains(u, "/api/server/map-style/dark") {
		t.Fatalf("amap url = %v", u)
	}
	if got := Normalize("gaode"); got != ProviderAMap {
		t.Fatalf("gaode alias = %v", got)
	}
	if got := Normalize("whatever"); got != ProviderOSM {
		t.Fatalf("unknown falls back to osm: %v", got)
	}
}
