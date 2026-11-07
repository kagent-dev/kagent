package checkpoint

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Fault policies and injected objects belong to a bucket created exclusively
// for this scenario; the installation's shared snapshot bucket is untouched.
func checkpointObjectStore(t *testing.T, endpoint, accessKey, secretKey string) (*s3.Client, string) {
	t.Helper()
	require.NotEmpty(t, endpoint, "real object-store endpoint is required")
	require.NotEmpty(t, accessKey)
	require.NotEmpty(t, secretKey)
	client := s3.NewFromConfig(aws.Config{
		Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""), RetryMaxAttempts: 1,
	}, func(o *s3.Options) { o.BaseEndpoint = aws.String(endpoint); o.UsePathStyle = true })
	bucket := "checkpoint-recovery-" + uuid.NewString()
	_, err := client.CreateBucket(t.Context(), &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, err := client.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucket)})
		require.NoError(t, err)
		for _, key := range checkpointObjectKeys(t, ctx, client, bucket, "") {
			_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
			require.NoError(t, err)
		}
		_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
		require.NoError(t, err)
	})
	return client, bucket
}

func checkpointObjectKeys(t *testing.T, ctx context.Context, client *s3.Client, bucket, prefix string) []string {
	t.Helper()
	pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	var keys []string
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		require.NoError(t, err)
		for _, object := range page.Contents {
			keys = append(keys, aws.ToString(object.Key))
		}
	}
	return keys
}
