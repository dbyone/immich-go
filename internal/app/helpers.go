package app

import (
	"os"

	"immich-go/internal/media"
	"immich-go/internal/storage"
)

func fileSizeOf(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func probeImage(path string) (width, height int, format string, err error) {
	return media.Probe(path)
}

func generateThumbnail(originalPath string, stg *storage.Storage, ownerID, assetID, variant string) (string, error) {
	maxEdge := media.PreviewMax
	if variant == "thumbnail" {
		maxEdge = media.ThumbnailMax
	}
	data, err := media.GenerateThumb(originalPath, maxEdge)
	if err != nil {
		return "", err
	}
	return stg.WriteThumb(ownerID, assetID, variant, data)
}
