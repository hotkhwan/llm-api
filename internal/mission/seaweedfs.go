package mission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const defaultMaxSeaweedObjectBytes int64 = 512 << 20

// SeaweedFSStore uses only the standard S3 protocol exposed by SeaweedFS.
// Credentials are constructor inputs from the runtime secret provider and are
// never retained in domain records or logs.
type SeaweedFSStore struct {
	bucket         string
	prefix         string
	client         *s3.Client
	presign        *s3.PresignClient
	maxObjectBytes int64
}

func NewSeaweedFSStore(ctx context.Context, endpoint, publicEndpoint, region, bucket, prefix, accessKey, secretKey string) (*SeaweedFSStore, error) {
	if strings.TrimSpace(endpoint) == "" || strings.TrimSpace(bucket) == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("SeaweedFS S3 endpoint, bucket and credentials are required")
	}
	if region == "" {
		region = "us-east-1"
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")))
	if err != nil {
		return nil, fmt.Errorf("load S3 config: %w", err)
	}
	clientFor := func(base string) *s3.Client {
		return s3.NewFromConfig(cfg, func(options *s3.Options) {
			options.BaseEndpoint = aws.String(strings.TrimRight(base, "/"))
			options.UsePathStyle = true
		})
	}
	client := clientFor(endpoint)
	if strings.TrimSpace(publicEndpoint) == "" {
		publicEndpoint = endpoint
	}
	return &SeaweedFSStore{bucket: bucket, prefix: strings.Trim(strings.TrimSpace(prefix), "/"), client: client, presign: s3.NewPresignClient(clientFor(publicEndpoint)), maxObjectBytes: defaultMaxSeaweedObjectBytes}, nil
}

func (s *SeaweedFSStore) objectKey(key string) string {
	key = strings.TrimLeft(key, "/")
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

func (s *SeaweedFSStore) Put(ctx context.Context, key, contentType string, reader io.Reader) (ObjectMetadata, error) {
	// The AWS SDK cannot sign a trailing checksum for an unseekable stream over
	// the cluster-internal HTTP endpoint. Spool into a bounded, private file so
	// the SDK can calculate the checksum before upload without buffering derived
	// video exports in memory. Inbound request bodies remain independently
	// constrained by HTTP_BODY_LIMIT_BYTES.
	spool, err := os.CreateTemp("", "kwanni-seaweed-put-*")
	if err != nil {
		return ObjectMetadata{}, fmt.Errorf("create SeaweedFS upload spool: %w", err)
	}
	spoolName := spool.Name()
	defer os.Remove(spoolName)
	defer spool.Close()

	limit := s.maxObjectBytes
	if limit <= 0 {
		limit = defaultMaxSeaweedObjectBytes
	}
	hash := sha256.New()
	counter := &countWriter{}
	written, err := io.Copy(io.MultiWriter(spool, hash, counter), io.LimitReader(reader, limit+1))
	if err != nil {
		return ObjectMetadata{}, fmt.Errorf("spool SeaweedFS object: %w", err)
	}
	if written == 0 || written > limit {
		return ObjectMetadata{}, fmt.Errorf("SeaweedFS object must be between 1 and %d bytes", limit)
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return ObjectMetadata{}, fmt.Errorf("rewind SeaweedFS upload spool: %w", err)
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.objectKey(key)), ContentType: aws.String(contentType), ContentLength: aws.Int64(written), Body: spool})
	if err != nil {
		return ObjectMetadata{}, fmt.Errorf("put SeaweedFS object: %w", err)
	}
	return ObjectMetadata{Key: key, ContentType: contentType, Bytes: counter.n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (s *SeaweedFSStore) DownloadURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 || ttl > time.Hour {
		return "", fmt.Errorf("download URL ttl must be within one hour")
	}
	result, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.objectKey(key))}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("presign SeaweedFS object: %w", err)
	}
	return result.URL, nil
}

func (s *SeaweedFSStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.objectKey(key))})
	if err != nil {
		return nil, fmt.Errorf("get SeaweedFS object: %w", err)
	}
	return result.Body, nil
}

type countWriter struct{ n int64 }

func (w *countWriter) Write(value []byte) (int, error) {
	w.n += int64(len(value))
	return len(value), nil
}
