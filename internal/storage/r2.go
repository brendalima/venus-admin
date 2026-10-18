package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var _ ImageStore = (*R2)(nil)

// R2 keeps the photo files in a Cloudflare R2 bucket, through R2's S3
// compatible API.
type R2 struct {
	client *s3.Client
	bucket string
}

type R2Input struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string

	// Endpoint and HTTPClient exist for tests; empty and nil mean the
	// account's R2 endpoint and a client with a timeout.
	Endpoint   string
	HTTPClient *http.Client
}

func NewR2(in R2Input) (*R2, error) {
	switch {
	case in.AccountID == "" && in.Endpoint == "":
		return nil, errors.New("missing r2 account id")
	case in.AccessKeyID == "" || in.SecretAccessKey == "":
		return nil, errors.New("missing r2 credentials")
	case in.Bucket == "":
		return nil, errors.New("missing r2 bucket")
	}

	endpoint := in.Endpoint
	if endpoint == "" {
		endpoint = "https://" + in.AccountID + ".r2.cloudflarestorage.com"
	}
	httpClient := in.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	client := s3.New(s3.Options{
		Region:       "auto", // what R2 expects
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(in.AccessKeyID, in.SecretAccessKey, ""),
		UsePathStyle: true,
		HTTPClient:   httpClient,
		// The SDK's newer default adds CRC checksums to every upload;
		// asking only when an operation requires them keeps to what every
		// S3-compatible service understands.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &R2{client: client, bucket: in.Bucket}, nil
}

func MustNewR2(in R2Input) *R2 {
	r, err := NewR2(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create r2 store: %w", err))
	}
	return r
}

func (r *R2) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(r.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String(contentType),
		// A key is never reused for different content, so browsers and
		// CDNs may keep the file for good.
		CacheControl: aws.String(immutableCacheControl),
	})
	if err != nil {
		return fmt.Errorf("couldn't put %s in r2: %w", key, err)
	}
	return nil
}

// Delete succeeds for a key that is already gone, as S3 does.
func (r *R2) Delete(ctx context.Context, key string) error {
	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("couldn't delete %s from r2: %w", key, err)
	}
	return nil
}
