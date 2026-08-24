package skillsinit

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3API is the subset of the S3 client used by FetchS3 (for tests).
type s3API interface {
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// FetchS3 downloads a skill bundle from S3 into ref.Dest.
//
// Bundle shapes:
//   - Archive: URI key ending in .zip / .tgz / .tar.gz → download + extract
//   - Prefix: otherwise treat URI as a prefix and sync objects recursively
//
// Auth uses the AWS SDK default credential chain (env static keys, etc.).
func FetchS3(ctx context.Context, ref S3Ref) error {
	client, err := newS3Client(ctx, ref.Region, ref.Endpoint)
	if err != nil {
		return err
	}
	return fetchS3(ctx, client, ref)
}

// newS3Client creates a new S3 client with the given region.
func newS3Client(ctx context.Context, region, endpoint string) (*s3.Client, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
			options.UsePathStyle = true
		}
	}), nil
}

// fetchS3 downloads a skill bundle from S3 into ref.Dest.
func fetchS3(ctx context.Context, client s3API, ref S3Ref) error {
	bucket, key, err := parseS3URI(ref.URI)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(ref.Dest, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", ref.Dest, err)
	}
	if isArchiveKey(key) {
		return fetchS3Archive(ctx, client, bucket, key, ref.Dest, ref.VersionID)
	}
	if ref.VersionID != "" {
		return fmt.Errorf("versionId requires an S3 archive object")
	}
	return fetchS3Prefix(ctx, client, bucket, key, ref.Dest)
}

// parseS3URI parses a S3 URI into bucket and key.
func parseS3URI(uri string) (bucket, key string, err error) {
	const prefix = "s3://"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", fmt.Errorf("s3 uri must start with s3://, got %q", uri)
	}
	rest := strings.TrimPrefix(uri, prefix)
	if rest == "" {
		return "", "", fmt.Errorf("s3 uri missing bucket: %q", uri)
	}
	bucket, key, ok := strings.Cut(rest, "/")
	if !ok || bucket == "" {
		return "", "", fmt.Errorf("s3 uri missing bucket or key: %q", uri)
	}
	key = strings.TrimPrefix(key, "/")
	if key == "" {
		return "", "", fmt.Errorf("s3 uri missing key/prefix: %q", uri)
	}
	return bucket, key, nil
}

// isArchiveKey returns true if key is a S3 archive (zip, tgz, tar.gz).
func isArchiveKey(key string) bool {
	lower := strings.ToLower(key)
	return strings.HasSuffix(lower, ".zip") ||
		strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tar.gz")
}

// fetchS3Archive downloads a single S3 archive into dest.
func fetchS3Archive(ctx context.Context, client s3API, bucket, key, dest, versionID string) error {
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	out, err := client.GetObject(ctx, input)
	if err != nil {
		return fmt.Errorf("get s3://%s/%s: %w", bucket, key, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return fmt.Errorf("read s3://%s/%s: %w", bucket, key, err)
	}

	lower := strings.ToLower(key)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(data, dest)
	case strings.HasSuffix(lower, ".tgz"), strings.HasSuffix(lower, ".tar.gz"):
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("gzip s3://%s/%s: %w", bucket, key, err)
		}
		defer gz.Close()
		return extractTar(gz, dest)
	default:
		return fmt.Errorf("unsupported archive %q", key)
	}
}

// fetchS3Prefix downloads all objects under s3://bucket/prefix into dest.
func fetchS3Prefix(ctx context.Context, client s3API, bucket, prefix, dest string) error {
	prefix = strings.TrimSuffix(prefix, "/")
	var keys []string
	var token *string
	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return fmt.Errorf("list s3://%s/%s: %w", bucket, prefix, err)
		}
		for _, obj := range out.Contents {
			// Skip empty folder markers
			if obj.Key == nil || strings.HasSuffix(*obj.Key, "/") {
				continue
			}
			keys = append(keys, *obj.Key)
		}
		if !aws.ToBool(out.IsTruncated) {
			break
		}
		token = out.NextContinuationToken
	}

	if len(keys) == 0 {
		return fmt.Errorf("no objects under s3://%s/%s", bucket, prefix)
	}
	// Exact single non-archive object at the prefix is not a skill bundle.
	if len(keys) == 1 && keys[0] == prefix {
		return fmt.Errorf("s3://%s/%s is a single object; use a prefix with SKILL.md or a .zip/.tgz archive", bucket, prefix)
	}

	for _, key := range keys {
		rel, ok := relKeyUnderPrefix(prefix, key)
		if !ok {
			continue
		}
		if err := downloadS3Object(ctx, client, bucket, key, filepath.Join(dest, rel)); err != nil {
			return err
		}
	}
	return nil
}

// relKeyUnderPrefix returns the relative path of key under prefix, or false if key is not under prefix.
// e.g. relKeyUnderPrefix("s3://bucket/prefix", "s3://bucket/prefix/scripts/test.py") == "scripts/test.py", true
func relKeyUnderPrefix(prefix, key string) (string, bool) {
	prefix = strings.TrimSuffix(prefix, "/")
	if key == prefix {
		return path.Base(key), true
	}
	cut := prefix + "/"
	if !strings.HasPrefix(key, cut) {
		return "", false
	}
	rel := key[len(cut):]
	if rel == "" || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", false
	}
	return filepath.FromSlash(rel), true
}

// downloadS3Object downloads a single S3 object into destPath.
func downloadS3Object(ctx context.Context, client s3API, bucket, key, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("get s3://%s/%s: %w", bucket, key, err)
	}
	defer out.Body.Close()

	f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, out.Body); err != nil {
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	return nil
}

// extractZip writes zip bytes into dst using os.Root (zip-slip safe).
func extractZip(data []byte, dst string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return fmt.Errorf("open root %s: %w", dst, err)
	}
	defer root.Close()

	for _, f := range zr.File {
		rel, err := tarEntryToLocal(f.Name)
		if err != nil {
			return fmt.Errorf("zip entry %q: %w", f.Name, err)
		}
		if rel == "" {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := root.MkdirAll(rel, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode()&0o777)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
