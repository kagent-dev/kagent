package skillsinit

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parseS3URI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantKey    string
		wantErr    bool
	}{
		{name: "prefix", uri: "s3://bucket/path/to/skill", wantBucket: "bucket", wantKey: "path/to/skill"},
		{name: "archive", uri: "s3://bucket/skills/ops.zip", wantBucket: "bucket", wantKey: "skills/ops.zip"},
		{name: "trailing slash", uri: "s3://bucket/path/", wantBucket: "bucket", wantKey: "path/"},
		{name: "missing scheme", uri: "bucket/key", wantErr: true},
		{name: "missing key", uri: "s3://bucket", wantErr: true},
		{name: "empty key", uri: "s3://bucket/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, key, err := parseS3URI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantBucket, bucket)
			assert.Equal(t, tt.wantKey, key)
		})
	}
}

func Test_isArchiveKey(t *testing.T) {
	assert.True(t, isArchiveKey("a/b.zip"))
	assert.True(t, isArchiveKey("a/b.TGZ"))
	assert.True(t, isArchiveKey("a/b.tar.gz"))
	assert.False(t, isArchiveKey("a/b/SKILL.md"))
	assert.False(t, isArchiveKey("a/b.tar"))
}

func Test_relKeyUnderPrefix(t *testing.T) {
	rel, ok := relKeyUnderPrefix("team/skill", "team/skill/SKILL.md")
	require.True(t, ok)
	assert.Equal(t, "SKILL.md", rel)

	rel, ok = relKeyUnderPrefix("team/skill", "team/skill/scripts/run.sh")
	require.True(t, ok)
	assert.Equal(t, filepath.Join("scripts", "run.sh"), rel)

	_, ok = relKeyUnderPrefix("team/skill", "team/other/SKILL.md")
	assert.False(t, ok)

	_, ok = relKeyUnderPrefix("team/skill", "team/skill/../escape")
	assert.False(t, ok)
}

func Test_extractZip_rejectsPathTraversal(t *testing.T) {
	dst := t.TempDir()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("../escape.txt")
	require.NoError(t, err)
	_, err = w.Write([]byte("pwned"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	err = extractZip(buf.Bytes(), dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes destination")
}

func Test_fetchS3_prefix(t *testing.T) {
	client := &fakeS3{
		objects: map[string][]byte{
			"team/kebab/SKILL.md":        []byte("# kebab"),
			"team/kebab/scripts/make.py": []byte("print(1)"),
			"team/kebab-other/SKILL.md":  []byte("nope"),
		},
	}
	dst := t.TempDir()
	err := fetchS3(context.Background(), client, S3Ref{
		URI:  "s3://skills/team/kebab",
		Dest: dst,
	})
	require.NoError(t, err)
	assert.Equal(t, "# kebab", readFile(t, filepath.Join(dst, "SKILL.md")))
	assert.Equal(t, "print(1)", readFile(t, filepath.Join(dst, "scripts", "make.py")))
	_, err = os.Stat(filepath.Join(dst, "SKILL.md"))
	require.NoError(t, err)
}

func Test_fetchS3_singleObjectRejected(t *testing.T) {
	client := &fakeS3{
		objects: map[string][]byte{
			"team/readme.txt": []byte("hi"),
		},
	}
	err := fetchS3(context.Background(), client, S3Ref{
		URI:  "s3://skills/team/readme.txt",
		Dest: t.TempDir(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "single object")
}

func Test_fetchS3_zipArchive(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("SKILL.md")
	require.NoError(t, err)
	_, err = w.Write([]byte("# from zip"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	client := &fakeS3{
		objects: map[string][]byte{
			"bundles/ops.zip": buf.Bytes(),
		},
	}
	dst := t.TempDir()
	err = fetchS3(context.Background(), client, S3Ref{
		URI:       "s3://skills/bundles/ops.zip",
		Dest:      dst,
		VersionID: "version-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "version-1", client.gotVersionID)
	assert.Equal(t, "# from zip", readFile(t, filepath.Join(dst, "SKILL.md")))
}

func Test_fetchS3_tgzArchive(t *testing.T) {
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "SKILL.md",
		Mode: 0o644,
		Size: int64(len("# from tgz")),
	}))
	_, err := tw.Write([]byte("# from tgz"))
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	var gzBuf bytes.Buffer
	gzw := gzip.NewWriter(&gzBuf)
	_, err = gzw.Write(tarBuf.Bytes())
	require.NoError(t, err)
	require.NoError(t, gzw.Close())

	client := &fakeS3{
		objects: map[string][]byte{
			"bundles/ops.tgz": gzBuf.Bytes(),
		},
	}
	dst := t.TempDir()
	err = fetchS3(context.Background(), client, S3Ref{
		URI:  "s3://skills/bundles/ops.tgz",
		Dest: dst,
	})
	require.NoError(t, err)
	assert.Equal(t, "# from tgz", readFile(t, filepath.Join(dst, "SKILL.md")))
}

type fakeS3 struct {
	objects      map[string][]byte
	gotVersionID string
}

func (f *fakeS3) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := aws.ToString(params.Prefix)
	var contents []types.Object
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			k := key
			contents = append(contents, types.Object{Key: &k})
		}
	}
	return &s3.ListObjectsV2Output{Contents: contents}, nil
}

func (f *fakeS3) GetObject(ctx context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.gotVersionID = aws.ToString(params.VersionId)
	key := aws.ToString(params.Key)
	data, ok := f.objects[key]
	if !ok {
		return nil, &types.NoSuchKey{Message: aws.String("not found")}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(data))}, nil
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}
