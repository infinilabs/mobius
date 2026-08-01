package tools

import (
	"context"
	"log/slog"
	"mobius/internal/config"
	"mobius/internal/domain"
	"mobius/internal/gcs"
	"mobius/internal/search"
	"os"
	"path/filepath"
	"time"
)

func UploadAssetToGCS(cfg *config.Config, gcs *gcs.Client, es *search.Client, project *domain.Project, assetID, localPath, relativePath string) {
	ctx := context.Background()
	pc := cfg.Projects
	gcsKey := filepath.Join("projects", project.Name, relativePath)

	f, err := os.Open(localPath)
	if err != nil {
		es.UpdateProjectAssetGCS(ctx, assetID, "", "failed")
		return
	}
	defer f.Close()

	mimeType := "application/octet-stream"

	for attempt := 0; attempt <= pc.GCSMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(pc.GCSBaseBackoff) * time.Millisecond * (1 << uint(attempt-1))
			time.Sleep(backoff)
			f.Seek(0, 0)
		}
		gcsURI, uerr := gcs.Upload(ctx, filepath.Dir(gcsKey), filepath.Base(gcsKey), "", f, mimeType)
		if uerr == nil {
			es.UpdateProjectAssetGCS(ctx, assetID, gcsURI, "synced")
			return
		}
		slog.Warn("gcs asset upload retry", "asset_id", assetID, "attempt", attempt, "error", uerr)
	}
	es.UpdateProjectAssetGCS(ctx, assetID, "", "failed")
	slog.Error("gcs asset upload failed permanently", "asset_id", assetID)
}
