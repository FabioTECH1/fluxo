package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"fluxo/internal/config"
	"fluxo/internal/database"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	multipartPartSize int64 = 64 << 20
	maxMultipartParts int64 = 10000
)

var (
	bucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	regionPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	accountIDPattern  = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)
	prefixPartPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
)

type objectStore struct {
	client   *s3.Client
	bucket   string
	provider string
}

type objectReference struct {
	Key       string
	VersionID string
}

func ValidateDestination(destination database.BackupDestination) error {
	if destination.Provider != "s3" && destination.Provider != "r2" {
		return errors.New("provider must be s3 or r2")
	}
	if len(destination.Name) < 1 || len(destination.Name) > 80 || strings.TrimSpace(destination.Name) != destination.Name ||
		strings.IndexFunc(destination.Name, unicode.IsControl) >= 0 {
		return errors.New("destination name must be between 1 and 80 characters without surrounding whitespace")
	}
	if !validBucketName(destination.Bucket) {
		return errors.New("invalid bucket name")
	}
	if _, err := normalizePrefix(destination.Prefix); err != nil {
		return err
	}
	if destination.Provider == "r2" {
		if !accountIDPattern.MatchString(destination.AccountID) {
			return errors.New("R2 account ID must be 32 hexadecimal characters")
		}
		switch destination.Jurisdiction {
		case "", "default", "eu", "fedramp":
		default:
			return errors.New("R2 jurisdiction must be default, eu, or fedramp")
		}
		if destination.UseInstanceRole {
			return errors.New("the AWS default credential chain is only available for Amazon S3")
		}
	} else {
		if !regionPattern.MatchString(destination.Region) {
			return errors.New("a valid AWS region is required")
		}
		if destination.AccountID != "" {
			return errors.New("account ID is only used by Cloudflare R2")
		}
		if destination.Jurisdiction != "" && destination.Jurisdiction != "default" {
			return errors.New("jurisdiction is only used by Cloudflare R2")
		}
	}
	if !destination.UseInstanceRole && (destination.AccessKey == "" || destination.SecretKey == "") {
		return errors.New("access key and secret key are required")
	}
	if len(destination.AccessKey) > 512 || len(destination.SecretKey) > 2048 {
		return errors.New("destination credentials are too long")
	}
	return nil
}

func validBucketName(name string) bool {
	if !bucketNamePattern.MatchString(name) || strings.Contains(name, "..") || strings.Contains(name, ".-") || strings.Contains(name, "-.") {
		return false
	}
	return net.ParseIP(name) == nil
}

func normalizePrefix(prefix string) (string, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return "fluxo", nil
	}
	if len(prefix) > 512 {
		return "", errors.New("prefix is too long")
	}
	for _, part := range strings.Split(prefix, "/") {
		if part == "" || part == "." || part == ".." || !prefixPartPattern.MatchString(part) {
			return "", errors.New("prefix may contain only letters, numbers, dots, underscores, hyphens, and slashes")
		}
	}
	return prefix, nil
}

func newObjectStore(ctx context.Context, destination database.BackupDestination) (*objectStore, error) {
	destination.AccessKey = config.Decrypt(destination.AccessKey)
	destination.SecretKey = config.Decrypt(destination.SecretKey)
	if err := ValidateDestination(destination); err != nil {
		return nil, err
	}
	region := destination.Region
	if destination.Provider == "r2" {
		region = "auto"
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if !destination.UseInstanceRole {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(destination.AccessKey, destination.SecretKey, ""),
		))
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("initialize %s client: %w", destination.Provider, err)
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		if destination.Provider == "r2" {
			jurisdiction := destination.Jurisdiction
			if jurisdiction == "" || jurisdiction == "default" {
				options.BaseEndpoint = aws.String("https://" + destination.AccountID + ".r2.cloudflarestorage.com")
			} else {
				options.BaseEndpoint = aws.String("https://" + destination.AccountID + "." + jurisdiction + ".r2.cloudflarestorage.com")
			}
			options.UsePathStyle = true
		}
	})
	return &objectStore{client: client, bucket: destination.Bucket, provider: destination.Provider}, nil
}

func (store *objectStore) test(ctx context.Context, prefix string) error {
	payload := []byte("fluxo-backup-destination-test")
	key := prefix + "/.fluxo-test/" + time.Now().UTC().Format("20060102T150405.000000000Z")
	versionID, err := store.uploadBytes(ctx, key, payload, "text/plain", "")
	reference := objectReference{Key: key, VersionID: versionID}
	cleanup := true
	defer func() {
		if cleanup {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = store.deleteObjects(cleanupCtx, []objectReference{reference})
			cancel()
		}
	}()
	if err != nil {
		return fmt.Errorf("write test object: %w", err)
	}

	getInput := &s3.GetObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key)}
	if store.provider == "s3" && versionID != "" {
		getInput.VersionId = aws.String(versionID)
	}
	response, err := store.client.GetObject(ctx, getInput)
	if err != nil {
		return fmt.Errorf("read test object: %w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, int64(len(payload)+1)))
	response.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read test response: %w", readErr)
	}
	if !bytes.Equal(body, payload) {
		return errors.New("test object contents did not match")
	}
	if err := store.deleteObjects(ctx, []objectReference{reference}); err != nil {
		return fmt.Errorf("delete test object: %w", err)
	}
	cleanup = false
	return nil
}

func (store *objectStore) uploadBytes(ctx context.Context, key string, body []byte, contentType, checksum string) (string, error) {
	metadata := map[string]string{}
	if checksum != "" {
		metadata["sha256"] = checksum
	}
	response, err := store.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(store.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   aws.String(contentType),
		Metadata:      metadata,
	})
	if err != nil {
		return "", err
	}
	versionID := aws.ToString(response.VersionId)
	if err := store.verifyObject(ctx, key, versionID, int64(len(body)), checksum); err != nil {
		return versionID, err
	}
	return versionID, nil
}

func (store *objectStore) uploadFile(ctx context.Context, key, path, contentType, checksum string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	var versionID string
	if info.Size() < multipartPartSize {
		var response *s3.PutObjectOutput
		response, err = store.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:        aws.String(store.bucket),
			Key:           aws.String(key),
			Body:          file,
			ContentLength: aws.Int64(info.Size()),
			ContentType:   aws.String(contentType),
			Metadata:      map[string]string{"sha256": checksum},
		})
		if response != nil {
			versionID = aws.ToString(response.VersionId)
		}
	} else {
		versionID, err = store.uploadMultipart(ctx, key, file, info.Size(), contentType, checksum)
	}
	if err != nil {
		return versionID, err
	}
	if err := store.verifyObject(ctx, key, versionID, info.Size(), checksum); err != nil {
		return versionID, err
	}
	return versionID, nil
}

func (store *objectStore) uploadMultipart(ctx context.Context, key string, file *os.File, size int64, contentType, checksum string) (versionID string, resultErr error) {
	partSize := multipartPartSize
	if required := (size + maxMultipartParts - 1) / maxMultipartParts; required > partSize {
		const alignment int64 = 1 << 20
		partSize = ((required + alignment - 1) / alignment) * alignment
	}
	created, err := store.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(store.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
		Metadata:    map[string]string{"sha256": checksum},
	})
	if err != nil {
		return "", err
	}
	completed := false
	defer func() {
		if !completed {
			abortCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = store.client.AbortMultipartUpload(abortCtx, &s3.AbortMultipartUploadInput{
				Bucket: aws.String(store.bucket), Key: aws.String(key), UploadId: created.UploadId,
			})
		}
	}()

	parts := make([]types.CompletedPart, 0, (size+partSize-1)/partSize)
	var partNumber int32 = 1
	for offset := int64(0); offset < size; offset += partSize {
		partLength := partSize
		if remaining := size - offset; remaining < partLength {
			partLength = remaining
		}
		response, err := store.client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket: aws.String(store.bucket), Key: aws.String(key), UploadId: created.UploadId,
			PartNumber: aws.Int32(partNumber), Body: io.NewSectionReader(file, offset, partLength),
			ContentLength: aws.Int64(partLength),
		})
		if err != nil {
			return "", fmt.Errorf("upload part %d: %w", partNumber, err)
		}
		parts = append(parts, types.CompletedPart{ETag: response.ETag, PartNumber: aws.Int32(partNumber)})
		partNumber++
	}
	response, err := store.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(store.bucket), Key: aws.String(key), UploadId: created.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	if err != nil {
		return "", err
	}
	completed = true
	return aws.ToString(response.VersionId), nil
}

func (store *objectStore) verifyObject(ctx context.Context, key, versionID string, expectedSize int64, checksum string) error {
	input := &s3.HeadObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key)}
	if store.provider == "s3" && versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	response, err := store.client.HeadObject(ctx, input)
	if err != nil {
		return fmt.Errorf("verify uploaded object: %w", err)
	}
	if response.ContentLength == nil || *response.ContentLength != expectedSize {
		return fmt.Errorf("uploaded object size mismatch: expected %d bytes", expectedSize)
	}
	if checksum != "" && !strings.EqualFold(response.Metadata["sha256"], checksum) {
		return errors.New("uploaded object checksum metadata mismatch")
	}
	return nil
}

func (store *objectStore) deleteObjects(ctx context.Context, objects []objectReference) error {
	for _, object := range objects {
		if object.Key == "" {
			continue
		}
		input := &s3.DeleteObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(object.Key)}
		versionID := object.VersionID
		if store.provider == "s3" && versionID == "" {
			response, err := store.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(object.Key)})
			if err == nil {
				versionID = aws.ToString(response.VersionId)
			} else if isNotFoundResponse(err) {
				continue
			} else {
				return fmt.Errorf("resolve object version for %s: %w", object.Key, err)
			}
		}
		if store.provider == "s3" && versionID != "" {
			input.VersionId = aws.String(versionID)
		}
		if _, err := store.client.DeleteObject(ctx, input); err != nil {
			return err
		}
	}
	return nil
}

func isNotFoundResponse(err error) bool {
	var responseError *smithyhttp.ResponseError
	return errors.As(err, &responseError) && responseError.HTTPStatusCode() == 404
}

func (store *objectStore) presignDownload(ctx context.Context, key, versionID, filename string, expiry time.Duration) (string, error) {
	filename = strings.ReplaceAll(strings.ReplaceAll(filename, "\r", ""), "\n", "")
	input := &s3.GetObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(key),
		ResponseContentDisposition: aws.String(`attachment; filename="` + url.PathEscape(filename) + `"`),
	}
	if store.provider == "s3" && versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	response, err := s3.NewPresignClient(store.client).PresignGetObject(ctx, input, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", err
	}
	return response.URL, nil
}

func contentTypeForArtifact(filename string) string {
	if strings.HasSuffix(filename, ".json") {
		return "application/json"
	}
	if strings.HasSuffix(filename, ".tar.gz") || strings.HasSuffix(filename, ".sql.gz") {
		return "application/gzip"
	}
	return "application/octet-stream"
}
