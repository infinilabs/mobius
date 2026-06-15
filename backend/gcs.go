package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

type GCSClient struct {
	client    *storage.Client
	bucket    string
	projectID string
}

func NewGCSClient(ctx context.Context, cfg *Config) (*GCSClient, error) {
	gc := cfg.GoogleCloud
	if gc.GCS.Bucket == "" {
		return nil, fmt.Errorf("GCS bucket not configured")
	}

	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	projectID := gc.GCS.ProjectID
	if projectID == "" {
		projectID = gc.ProjectID
	}
	g := &GCSClient{client: client, bucket: gc.GCS.Bucket, projectID: projectID}

	if err := g.EnsureBucket(ctx, gc.GCS.Location, gc.GCS.PublicAccessPrevention); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ensure GCS bucket: %w", err)
	}

	slog.Info("GCS client connected", "bucket", gc.GCS.Bucket)
	return g, nil
}

func (g *GCSClient) EnsureBucket(ctx context.Context, location string, publicAccessPrevention bool) error {
	_, err := g.client.Bucket(g.bucket).Attrs(ctx)
	if err == nil {
		return nil
	}
	if !errors.Is(err, storage.ErrBucketNotExist) {
		return fmt.Errorf("failed to check bucket: %w", err)
	}

	if location == "" {
		location = "us-central1"
	}

	attrs := &storage.BucketAttrs{
		Location: location,
	}
	if publicAccessPrevention {
		attrs.PublicAccessPrevention = storage.PublicAccessPreventionEnforced
	}

	if err := g.client.Bucket(g.bucket).Create(ctx, g.projectID, attrs); err != nil {
		return fmt.Errorf("failed to create bucket %q: %w", g.bucket, err)
	}
	slog.Info("GCS bucket created", "bucket", g.bucket, "location", location, "public_access_prevention", publicAccessPrevention)
	return nil
}

func (g *GCSClient) Ping(ctx context.Context) error {
	_, err := g.client.Bucket(g.bucket).Attrs(ctx)
	return err
}

func (g *GCSClient) Upload(ctx context.Context, prefix, fileID, ext string, data io.Reader, contentType string) (string, error) {
	var objectName string
	if prefix == "" || prefix == "." {
		objectName = fmt.Sprintf("%s%s", fileID, ext)
	} else {
		objectName = fmt.Sprintf("%s/%s%s", prefix, fileID, ext)
	}
	obj := g.client.Bucket(g.bucket).Object(objectName)

	w := obj.NewWriter(ctx)
	w.ContentType = contentType

	if _, err := io.Copy(w, data); err != nil {
		w.Close()
		return "", fmt.Errorf("GCS write failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("GCS close failed: %w", err)
	}

	gcsURI := fmt.Sprintf("gs://%s/%s", g.bucket, objectName)
	slog.Info("GCS upload complete", "uri", gcsURI)
	return gcsURI, nil
}

func (g *GCSClient) Delete(ctx context.Context, gcsURI string) error {
	prefix := fmt.Sprintf("gs://%s/", g.bucket)
	if len(gcsURI) <= len(prefix) {
		return fmt.Errorf("invalid GCS URI: %s", gcsURI)
	}
	objectName := gcsURI[len(prefix):]

	if err := g.client.Bucket(g.bucket).Object(objectName).Delete(ctx); err != nil {
		return fmt.Errorf("GCS delete failed for %s: %w", objectName, err)
	}
	slog.Debug("GCS object deleted", "uri", gcsURI)
	return nil
}

func (g *GCSClient) DeletePrefix(ctx context.Context, prefix string) error {
	it := g.client.Bucket(g.bucket).Objects(ctx, &storage.Query{Prefix: prefix})
	var lastErr error
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("GCS list objects during prefix delete: %w", err)
		}
		if delErr := g.client.Bucket(g.bucket).Object(attrs.Name).Delete(ctx); delErr != nil {
			slog.Warn("GCS delete prefix item failed", "object", attrs.Name, "error", delErr)
			lastErr = delErr
		}
	}
	return lastErr
}

func (g *GCSClient) Close() error {
	return g.client.Close()
}

// DownloadURI downloads a gs:// object identified by its full URI to a local path.
func (g *GCSClient) DownloadURI(ctx context.Context, gcsURI, localPath string) error {
	prefix := fmt.Sprintf("gs://%s/", g.bucket)
	if len(gcsURI) <= len(prefix) {
		return fmt.Errorf("invalid GCS URI: %s", gcsURI)
	}
	return g.Download(ctx, gcsURI[len(prefix):], localPath)
}

func (g *GCSClient) Download(ctx context.Context, relativePath, localPath string) error {
	obj := g.client.Bucket(g.bucket).Object(relativePath)
	r, err := obj.NewReader(ctx)
	if err != nil {
		return fmt.Errorf("failed to open GCS reader for %s: %w", relativePath, err)
	}
	defer r.Close()

	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file %s: %w", localPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("failed to copy GCS object to local file: %w", err)
	}

	slog.Debug("GCS download complete", "object", relativePath, "local", localPath)
	return nil
}

func (g *GCSClient) ListPrefix(ctx context.Context, prefix string) ([]string, error) {
	it := g.client.Bucket(g.bucket).Objects(ctx, &storage.Query{Prefix: prefix})
	var objects []string
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("GCS list objects prefix %q: %w", prefix, err)
		}
		if strings.HasSuffix(attrs.Name, "/") {
			continue
		}
		objects = append(objects, attrs.Name)
	}
	return objects, nil
}

func (g *GCSClient) PublishPlayable(ctx context.Context, pipelineID, outputDir string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("GCS client not initialized")
	}

	absDir, err := filepath.Abs(outputDir)
	if err != nil {
		return "", err
	}

	uploadedCount := 0

	err = filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(absDir, path)
		if err != nil {
			return err
		}

		targetName := relPath
		if relPath == "preview_inline.html" {
			targetName = "index.html"
		} else if relPath == "index.html" {
			targetName = "source.html"
		}

		objectName := fmt.Sprintf("playable-ads/%s/%s", pipelineID, filepath.ToSlash(targetName))

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		contentType := mime.TypeByExtension(filepath.Ext(path))
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		obj := g.client.Bucket(g.bucket).Object(objectName)
		w := obj.NewWriter(ctx)
		w.ContentType = contentType
		w.CacheControl = "public, max-age=3600"

		if _, err := io.Copy(w, file); err != nil {
			w.Close()
			return fmt.Errorf("failed to upload %s: %w", relPath, err)
		}
		if err := w.Close(); err != nil {
			return fmt.Errorf("failed to close GCS writer for %s: %w", relPath, err)
		}

		uploadedCount++
		return nil
	})

	if err != nil {
		return "", err
	}

	slog.Info("Playable folder uploaded to GCS", "pipeline_id", pipelineID, "files_uploaded", uploadedCount)

	indexObjectName := fmt.Sprintf("playable-ads/%s/index.html", pipelineID)
	
	opts := &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  "GET",
		Expires: time.Now().Add(7 * 24 * time.Hour),
	}

	signedURL, err := g.client.Bucket(g.bucket).SignedURL(indexObjectName, opts)
	if err != nil {
		slog.Warn("GCS signing failed, returning public URL", "error", err)
		publicURL := fmt.Sprintf("https://storage.googleapis.com/%s/%s", g.bucket, indexObjectName)
		return publicURL, nil
	}

	return signedURL, nil
}

