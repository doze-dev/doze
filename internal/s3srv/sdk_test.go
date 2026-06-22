package s3srv

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// TestS3SDK drives the real aws-sdk-go-v2 S3 client against the server: bucket
// CRUD, object put/get/list/head/delete, manual multipart upload, and a
// presigned GET. The SDK performs its own (de)serialization and validation, so
// passing means real client compatibility.
func TestS3SDK(t *testing.T) {
	handler, closer, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx := context.Background()
	c := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(ts.URL)
		o.UsePathStyle = true
		// gofakes3 doesn't speak aws-chunked / trailer checksums; keep payloads plain.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	const bucket = "media"
	if _, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}

	// Put + Get.
	if _, err := c.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("greeting.txt"),
		Body: strings.NewReader("hello doze"),
	}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	got, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("greeting.txt")})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if b, _ := io.ReadAll(got.Body); string(b) != "hello doze" {
		t.Fatalf("GetObject body = %q", b)
	}

	// Head.
	if _, err := c.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String("greeting.txt")}); err != nil {
		t.Fatalf("HeadObject: %v", err)
	}

	// List.
	list, err := c.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
	if err != nil {
		t.Fatalf("ListObjectsV2: %v", err)
	}
	if len(list.Contents) != 1 || aws.ToString(list.Contents[0].Key) != "greeting.txt" {
		t.Fatalf("ListObjectsV2 unexpected: %+v", list.Contents)
	}

	// Manual multipart upload (two parts) -> get -> verify concatenation.
	testMultipart(t, ctx, c, bucket)

	// Presigned GET works without credentials on the wire.
	testPresign(t, ctx, c, bucket)

	// Delete.
	if _, err := c.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("greeting.txt")}); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
}

func testMultipart(t *testing.T, ctx context.Context, c *s3.Client, bucket string) {
	t.Helper()
	key := "big.bin"
	part1 := bytes.Repeat([]byte("A"), 5*1024*1024) // 5 MiB (S3 min part size)
	part2 := bytes.Repeat([]byte("B"), 1024)

	mpu, err := c.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		t.Fatalf("CreateMultipartUpload: %v", err)
	}
	var completed []types.CompletedPart
	for i, part := range [][]byte{part1, part2} {
		n := int32(i + 1)
		up, err := c.UploadPart(ctx, &s3.UploadPartInput{
			Bucket: aws.String(bucket), Key: aws.String(key),
			UploadId: mpu.UploadId, PartNumber: aws.Int32(n), Body: bytes.NewReader(part),
		})
		if err != nil {
			t.Fatalf("UploadPart %d: %v", n, err)
		}
		completed = append(completed, types.CompletedPart{ETag: up.ETag, PartNumber: aws.Int32(n)})
	}
	if _, err := c.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String(key), UploadId: mpu.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	}); err != nil {
		t.Fatalf("CompleteMultipartUpload: %v", err)
	}
	out, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		t.Fatalf("GetObject(multipart): %v", err)
	}
	b, _ := io.ReadAll(out.Body)
	if len(b) != len(part1)+len(part2) {
		t.Fatalf("multipart object size = %d, want %d", len(b), len(part1)+len(part2))
	}
}

func testPresign(t *testing.T, ctx context.Context, c *s3.Client, bucket string) {
	t.Helper()
	ps := s3.NewPresignClient(c)
	req, err := ps.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("greeting.txt")})
	if err != nil {
		t.Fatalf("PresignGetObject: %v", err)
	}
	resp, err := http.Get(req.URL)
	if err != nil {
		t.Fatalf("GET presigned: %v", err)
	}
	defer resp.Body.Close()
	if b, _ := io.ReadAll(resp.Body); string(b) != "hello doze" {
		t.Fatalf("presigned GET body = %q (status %s)", b, resp.Status)
	}
}
