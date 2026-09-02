package store

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/hostpack/hostpack/internal/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3 struct {
	client         *minio.Client
	bucket, prefix string
}

func minioMakeBucketOptions(region string) minio.MakeBucketOptions {
	return minio.MakeBucketOptions{Region: region}
}

func NewS3(c config.S3Config) (*S3, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return nil, err
	}
	secure := u.Scheme != "http"
	if c.Secure != nil {
		secure = *c.Secure
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("AWS_SESSION_TOKEN")), Secure: secure, Region: c.Region, Transport: transport})
	if err != nil {
		return nil, err
	}
	return &S3{client: client, bucket: c.Bucket, prefix: strings.Trim(c.Prefix, "/")}, nil
}
func (s *S3) key(k string) string {
	if s.prefix == "" {
		return k
	}
	return path.Join(s.prefix, k)
}
func (s *S3) List(ctx context.Context, prefix string) ([]string, error) {
	var out []string
	full := s.key(prefix)
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: full, Recursive: true}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		k := strings.TrimPrefix(obj.Key, s.prefix)
		k = strings.TrimPrefix(k, "/")
		out = append(out, k)
	}
	return out, nil
}
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	o, err := s.client.GetObject(ctx, s.bucket, s.key(key), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err = o.Stat(); err != nil {
		o.Close()
		return nil, err
	}
	return o, nil
}
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, s.bucket, s.key(key), r, size, minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}
