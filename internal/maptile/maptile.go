// Package maptile abstracts the basemap provider behind two seams:
// which style documents the web map loads, and whether photo
// coordinates need shifting onto the provider's datum.
//
// Two dialects:
//
//   - "osm": the upstream default — Immich-hosted vector styles
//     (tiles.immich.cloud), WGS-84 everywhere, identity transform.
//
//   - "amap": AMap (Gaode) raster tiles for mainland-China deployments.
//     AMap tiles use the GCJ-02 datum, so markers shot in WGS-84 must
//     be converted — but only inside China; outside the country the
//     two datums coincide and converting would *introduce* error
//     ("switch to AMap domestically, keep the coordinates as-is
//     abroad").
package maptile

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
)

// Providers recognized by IMMICH_MAP_PROVIDER.
const (
	ProviderOSM  = "osm"
	ProviderAMap = "amap"
)

// Normalize validates the configured provider name.
func Normalize(p string) string {
	switch strings.ToLower(p) {
	case ProviderAMap, "gaode", "高德":
		return ProviderAMap
	default:
		return ProviderOSM
	}
}

// StyleURL returns the maplibre style document URL for a theme.
// baseURL is this server's own origin (served styles live under /api).
func StyleURL(provider, theme, baseURL string) string {
	switch Normalize(provider) {
	case ProviderAMap:
		// Served by this binary: raster tiles need no style proxying
		// upstream, and AMap has no dark variant (satellite fills in).
		return fmt.Sprintf("%s/api/server/map-style/%s", baseURL, theme)
	default:
		if theme == "dark" {
			return "https://tiles.immich.cloud/v1/style/dark.json"
		}
		return "https://tiles.immich.cloud/v1/style/light.json"
	}
}

// WriteStyle serves a maplibre style document for the provider. The
// OSM dialect emits a plain OpenStreetMap raster style so the endpoint
// is a working fallback even though config URLs normally point at the
// Immich-hosted styles directly.
func WriteStyle(w http.ResponseWriter, provider, theme string) {
	doc := amapStyle(theme)
	if Normalize(provider) == ProviderOSM {
		doc = osmStyle(theme)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(doc))
}

func styleDocument(tiles []string) string {
	tileJSON, _ := json.Marshal(tiles)
	return `{
  "version": 8,
  "glyphs": "https://tiles.immich.cloud/v1/glyphs/{fontstack}/{range}.pbf",
  "sources": {"basemap": {"type": "raster", "tiles": ` + string(tileJSON) + `, "tileSize": 256, "maxzoom": 18}},
  "layers": [{"id": "basemap", "type": "raster", "source": "basemap"}]
}`
}

func amapStyle(theme string) string {
	// AMap raster tiles: style 7 is the street map, style 6 the
	// satellite layer (the closest thing to a dark theme). The
	// webst0-4 hosts rotate the same tiles and need no API key.
	tpl := "https://webst0%d.is.autonavi.com/appmaptile?style=%s&x={x}&y={y}&z={z}"
	styleID := "7"
	if theme == "dark" {
		styleID = "6"
	}
	var tiles []string
	for i := 0; i <= 3; i++ {
		tiles = append(tiles, fmt.Sprintf(tpl, i, styleID))
	}
	return styleDocument(tiles)
}

func osmStyle(theme string) string {
	return styleDocument([]string{"https://tile.openstreetmap.org/{z}/{x}/{y}.png"})
}

// FixCoord shifts a photo coordinate onto the provider's datum when
// needed: WGS-84 → GCJ-02 for AMap, and only within mainland China
// (the datums agree elsewhere, so a conversion would add error).
func FixCoord(provider string, lat, lng float64) (float64, float64) {
	if Normalize(provider) != ProviderAMap || !inChina(lat, lng) {
		return lat, lng
	}
	gLat, gLng := wgs84ToGCJ02(lat, lng)
	return gLat, gLng
}

// inChina is the coarse bounding box used by the common WGS↔GCJ
// implementations; false positives on the edge just yield the ~0.6"
// datum offset, which is negligible there.
func inChina(lat, lng float64) bool {
	return lng >= 72.004 && lng <= 137.8347 && lat >= 0.8293 && lat <= 55.8271
}

const (
	a        = 6378245.0              // semi-major axis
	ee       = 0.00669342162296594323 // first eccentricity squared
	xPI      = math.Pi * 3000.0 / 180.0
	degToRad = math.Pi / 180.0
)

func transformLat(x, y float64) float64 {
	ret := -100.0 + 2.0*x + 3.0*y + 0.2*y*y + 0.1*x*y + 0.2*math.Sqrt(math.Abs(x))
	ret += (20.0*math.Sin(6.0*x*math.Pi) + 20.0*math.Sin(2.0*x*math.Pi)) * 2.0 / 3.0
	ret += (20.0*math.Sin(y*math.Pi) + 40.0*math.Sin(y/3.0*math.Pi)) * 2.0 / 3.0
	ret += (160.0*math.Sin(y/12.0*math.Pi) + 320*math.Sin(y*math.Pi/30.0)) * 2.0 / 3.0
	return ret
}

func transformLng(x, y float64) float64 {
	ret := 300.0 + x + 2.0*y + 0.1*x*x + 0.1*x*y + 0.1*math.Sqrt(math.Abs(x))
	ret += (20.0*math.Sin(6.0*x*math.Pi) + 20.0*math.Sin(2.0*x*math.Pi)) * 2.0 / 3.0
	ret += (20.0*math.Sin(x*math.Pi) + 40.0*math.Sin(x/3.0*math.Pi)) * 2.0 / 3.0
	ret += (150.0*math.Sin(x/12.0*math.Pi) + 300.0*math.Sin(x/30.0*math.Pi)) * 2.0 / 3.0
	return ret
}

// wgs84ToGCJ02 is the standard public-domain transform (the same one
// MT Photos and every domestic map app uses): offsets of a few hundred
// meters that vary smoothly across the country.
func wgs84ToGCJ02(lat, lng float64) (float64, float64) {
	if lat <= -90 || lat >= 90 || lng <= -180 || lng >= 180 {
		return lat, lng
	}
	dLat := transformLat(lng-105.0, lat-35.0)
	dLng := transformLng(lng-105.0, lat-35.0)
	radLat := lat / 180.0 * math.Pi
	magic := math.Sin(radLat)
	magic = 1 - ee*magic*magic
	sqrtMagic := math.Sqrt(magic)
	dLat = (dLat * 180.0) / ((a * (1 - ee)) / (magic * sqrtMagic) * math.Pi)
	dLng = (dLng * 180.0) / (a / sqrtMagic * math.Cos(radLat) * math.Pi)
	return lat + dLat, lng + dLng
}
