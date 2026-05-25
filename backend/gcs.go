package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

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

	g := &GCSClient{client: client, bucket: gc.GCS.Bucket, projectID: gc.ProjectID}

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
	objectName := fmt.Sprintf("%s/%s%s", prefix, fileID, ext)
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
