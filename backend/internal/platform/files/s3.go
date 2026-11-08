package files

import (
	"bytes"
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Store uses any S3-compatible service. Locally and on the testing server, that is SeaweedFS.
type S3Store struct {
	client *s3.Client
	bucket string
}

// NewS3Store connects to an S3-compatible endpoint.
func NewS3Store(endpoint, accessKey, secretKey, bucket string) *S3Store {
	client := s3.New(s3.Options{
		BaseEndpoint: aws.String(endpoint),
		Region:       "us-east-1", // Required by the client. S3-compatible services ignore it.
		Credentials:  credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		UsePathStyle: true,
	})
	return &S3Store{client: client, bucket: bucket}
}

// EnsureBucket creates the bucket if needed. Buckets are private unless made otherwise.
func (s *S3Store) EnsureBucket(ctx context.Context) error {
	if _, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.bucket}); err == nil {
		return nil
	}
	_, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &s.bucket})
	return err
}

// Put stores bytes.
func (s *S3Store) Put(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: &s.bucket, Key: &key, Body: bytes.NewReader(body), ContentType: &contentType})
	return err
}

// Get returns bytes.
func (s *S3Store) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return nil, err
	}
	defer func() { _ = out.Body.Close() }()
	return io.ReadAll(io.LimitReader(out.Body, MaxBytes+1))
}
