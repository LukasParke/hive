package backup

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"go.uber.org/zap"
)

type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type S3Uploader struct {
	client *minio.Client
	log    *zap.SugaredLogger
}

func NewS3Uploader(cfg S3Config, log *zap.SugaredLogger) (*S3Uploader, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 client: %w", err)
	}
	return &S3Uploader{client: client, log: log}, nil
}

func (u *S3Uploader) Upload(ctx context.Context, bucket, objectName string, reader io.Reader, size int64) error {
	_, err := u.client.PutObject(ctx, bucket, objectName, reader, size, minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("s3 upload %s/%s: %w", bucket, objectName, err)
	}
	u.log.Infof("uploaded backup to s3://%s/%s", bucket, objectName)
	return nil
}

type S3Downloader struct {
	client *minio.Client
	log    *zap.SugaredLogger
}

func NewS3Downloader(cfg S3Config, log *zap.SugaredLogger) (*S3Downloader, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 client: %w", err)
	}
	return &S3Downloader{client: client, log: log}, nil
}

func (d *S3Downloader) Download(ctx context.Context, bucket, objectName, destPath string) error {
	obj, err := d.client.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("s3 get %s/%s: %w", bucket, objectName, err)
	}
	defer func() { _ = obj.Close() }()

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create dest file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, obj); err != nil {
		return fmt.Errorf("s3 download %s/%s: %w", bucket, objectName, err)
	}

	d.log.Infof("downloaded s3://%s/%s to %s", bucket, objectName, destPath)
	return nil
}
