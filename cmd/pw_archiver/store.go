package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// ObjectStore writes sealed partitions. Put creates key only if it does not exist and
// reports whether it did; an existing key is not an error.
type ObjectStore interface {
	Put(ctx context.Context, key string, body []byte, contentType string) (created bool, err error)
}

// S3Store is an ObjectStore on any S3 API, Cloudflare R2 included.
type S3Store struct {
	client *s3.Client
	bucket string
}

// S3Config names the bucket and how to reach it.
type S3Config struct {
	Endpoint  string // e.g. https://<account>.r2.cloudflarestorage.com
	Region    string // "auto" for R2
	Bucket    string
	AccessKey string
	SecretKey string
	PathStyle bool // MinIO and most self-hosted stores; R2 accepts either
}

// NewS3Store connects to the bucket. R2 does not implement the SDK's default
// request checksums, so they are sent only where the API requires them.
func NewS3Store(ctx context.Context, c S3Config) (*S3Store, error) {
	if c.Endpoint == "" || c.Bucket == "" {
		return nil, errors.New("s3: endpoint and bucket are required")
	}
	if c.AccessKey == "" || c.SecretKey == "" {
		return nil, errors.New("s3: access key and secret are required")
	}
	if c.Region == "" {
		c.Region = "auto"
	}
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(c.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, "")),
		config.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		config.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		return nil, fmt.Errorf("s3: %w", err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(c.Endpoint)
		o.UsePathStyle = c.PathStyle
	})
	return &S3Store{client: client, bucket: c.Bucket}, nil
}

// Put writes body under key with If-None-Match: *. A 412 (the key exists) returns
// created false and no error.
func (s *S3Store) Put(ctx context.Context, key string, body []byte, contentType string) (bool, error) {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   aws.String(contentType),
		IfNoneMatch:   aws.String("*"),
	})
	if err == nil {
		return true, nil
	}
	if isPreconditionFailed(err) {
		return false, nil
	}
	return false, fmt.Errorf("s3: put %s: %w", key, err)
}

func isPreconditionFailed(err error) bool {
	var re *awshttp.ResponseError
	if errors.As(err, &re) && re.HTTPStatusCode() == http.StatusPreconditionFailed {
		return true
	}
	var ae smithy.APIError
	if errors.As(err, &ae) && ae.ErrorCode() == "PreconditionFailed" {
		return true
	}
	return false
}
