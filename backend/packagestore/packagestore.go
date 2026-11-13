// Package packagestore keeps uploaded bot packages in S3. A package never
// passes through the backend: the backend hands out a presigned POST, the
// browser or the CLI uploads the zip straight to the bucket, and the backend
// then reads what it needs of the zip with range requests.
package packagestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// ErrNotFound is returned for an object that isn't in the bucket, such as a
// package whose zip hasn't been uploaded yet.
var ErrNotFound = errors.New("the object is not in the bucket")

// Upload is a presigned POST: a multipart/form-data POST to URL with every
// field, and the file last in a field named "file", stores the object.
type Upload struct {
	URL    string
	Fields map[string]string
}

// Store keeps bot packages by key.
type Store interface {
	// PresignUpload lets whoever holds the result upload one object of
	// 1 to maxBytes bytes to key, for expires.
	PresignUpload(ctx context.Context, key string, maxBytes int64, expires time.Duration) (Upload, error)
	// Size returns the size of the object at key, or ErrNotFound.
	Size(ctx context.Context, key string) (int64, error)
	// ReadRange returns length bytes of the object at key from offset.
	ReadRange(ctx context.Context, key string, offset, length int64) ([]byte, error)
	// Delete removes the object at key; a missing object is no error.
	Delete(ctx context.Context, key string) error
}

// S3 is a Store in one bucket.
type S3 struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

// NewS3 keeps packages in bucket with client.
func NewS3(client *s3.Client, bucket string) *S3 {
	return &S3{client: client, presign: s3.NewPresignClient(client), bucket: bucket}
}

// FromEnvironment keeps packages in bucket with the credentials and region
// of the environment, such as the Lambda function's role and AWS_REGION.
func FromEnvironment(ctx context.Context, bucket string) (*S3, error) {
	config, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load the AWS configuration for the bot package bucket: %w", err)
	}
	if config.Region == "" {
		return nil, errors.New("the bot package bucket needs a region: set AWS_REGION")
	}
	return NewS3(s3.NewFromConfig(config), bucket), nil
}

// Endpoint keeps packages in bucket on an S3-compatible server at endpoint,
// such as the local server host's fake S3, with path-style URLs and static
// credentials.
func Endpoint(endpoint, bucket, accessKeyID, secretAccessKey string) *S3 {
	client := s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
	})
	return NewS3(client, bucket)
}

func (store *S3) PresignUpload(ctx context.Context, key string, maxBytes int64, expires time.Duration) (Upload, error) {
	presigned, err := store.presign.PresignPostObject(ctx, &s3.PutObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key)},
		func(options *s3.PresignPostOptions) {
			options.Expires = expires
			options.Conditions = []any{[]any{"content-length-range", 1, maxBytes}}
		})
	if err != nil {
		return Upload{}, fmt.Errorf("presign the upload: %w", err)
	}
	return Upload{URL: presigned.URL, Fields: presigned.Values}, nil
}

func (store *S3) Size(ctx context.Context, key string) (int64, error) {
	head, err := store.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key)})
	if err != nil {
		// The backend's role may not list the bucket, and S3 then answers
		// 403 rather than 404 for a key that isn't there: a zip that hasn't
		// been uploaded yet.
		if notFound(err) || httpStatus(err) == http.StatusForbidden {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("read the package's size: %w", err)
	}
	return aws.ToInt64(head.ContentLength), nil
}

func (store *S3) ReadRange(ctx context.Context, key string, offset, length int64) ([]byte, error) {
	if length <= 0 {
		return nil, nil
	}
	object, err := store.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(key),
		Range: aws.String("bytes=" + strconv.FormatInt(offset, 10) + "-" + strconv.FormatInt(offset+length-1, 10)),
	})
	if err != nil {
		if notFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read the package: %w", err)
	}
	defer func() { _ = object.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(object.Body, length))
	if err != nil {
		return nil, fmt.Errorf("read the package: %w", err)
	}
	return data, nil
}

func (store *S3) Delete(ctx context.Context, key string) error {
	_, err := store.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key)})
	if err != nil && !notFound(err) {
		return fmt.Errorf("delete the package: %w", err)
	}
	return nil
}

func notFound(err error) bool {
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "NotFound", "NoSuchKey":
			return true
		}
	}
	return httpStatus(err) == http.StatusNotFound
}

// httpStatus is the status of S3's answer to a failed request, or 0.
func httpStatus(err error) int {
	var response interface{ HTTPStatusCode() int }
	if errors.As(err, &response) {
		return response.HTTPStatusCode()
	}
	return 0
}
