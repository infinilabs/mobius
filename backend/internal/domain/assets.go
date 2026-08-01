package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// ClassifyAspect maps width/height to one of the supported aspect-ratio buckets
// (9:16, 1:1, 4:5, 16:9) within a tolerance, else "other".
func ClassifyAspect(w, h int) string {
	if w <= 0 || h <= 0 {
		return "other"
	}
	r := float64(w) / float64(h)
	cands := []struct {
		label string
		val   float64
	}{
		{"1:1", 1.0}, {"9:16", 9.0 / 16.0}, {"16:9", 16.0 / 9.0}, {"4:5", 4.0 / 5.0},
	}
	best, bestDiff := "other", 0.06 // ~6% relative tolerance
	for _, c := range cands {
		if d := math.Abs(r-c.val) / c.val; d < bestDiff {
			best, bestDiff = c.label, d
		}
	}
	return best
}

// ComputeAspectRatio decodes an image's dimensions to derive its aspect ratio.
// Non-image content (and undecodable images) return "other".
func ComputeAspectRatio(localPath, contentType string) string {
	if contentType != "image" {
		return "other"
	}
	f, err := os.Open(localPath)
	if err != nil {
		return "other"
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return "other"
	}
	return ClassifyAspect(cfg.Width, cfg.Height)
}

func CalculateSHA256(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func ResolveMimeType(filename, headerMime string) string {
	headerMime = strings.TrimSpace(strings.ToLower(headerMime))
	if headerMime != "" && headerMime != "application/octet-stream" {
		return headerMime
	}
	ext := filepath.Ext(filename)
	switch ext {
	case ".go", ".py", ".js", ".ts", ".rs":
		return "text/x-" + ext[1:]
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".md":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".html":
		return "text/html"
	case ".csv":
		return "text/csv"
	case ".sql":
		return "text/x-sql"
	case ".pdf":
		return "application/pdf"
	}
	return "application/octet-stream"
}

func ClassifyContentType(mimeType string) string {
	switch {
	case mimeType == "text/plain" || mimeType == "text/csv":
		return "text"
	case strings.HasPrefix(mimeType, "text/x-") ||
		mimeType == "application/json" || mimeType == "application/xml" ||
		mimeType == "application/javascript" || mimeType == "application/yaml" ||
		mimeType == "application/x-yaml":
		return "code"
	case mimeType == "text/markdown" || mimeType == "text/html":
		return "document"
	case mimeType == "application/pdf":
		return "pdf"
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	default:
		return "binary"
	}
}

func IsTextIndexable(contentType string) bool {
	return contentType == "text" || contentType == "code" || contentType == "document"
}
