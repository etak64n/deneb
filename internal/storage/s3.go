package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/etak64n/deneb/internal/config"
)

// redirectTTL is how long the signed URLs of the redirects to the bucket stay valid.
// Players fetch a segment within seconds of the redirect.
const redirectTTL = 10 * time.Minute

// S3 keeps the files in a bucket of an S3-compatible service.
type S3 struct {
	bucket     string
	redirects  bool
	client     *s3.Client
	uploader   *manager.Uploader
	downloader *manager.Downloader
	presigner  *s3.PresignClient
}

// NewS3 connects to the bucket.
func NewS3(ctx context.Context, cfg config.S3Config) (*S3, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
		// Several S3-compatible services refuse the checksums that the SDK adds by default.
		awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		awsconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.PathStyle
	})
	return &S3{
		bucket:    cfg.Bucket,
		redirects: cfg.Redirects,
		client:    client,
		uploader: manager.NewUploader(client, func(u *manager.Uploader) {
			u.PartSize = 16 << 20
			// The uploader has a setting of its own, which the client's does not change.
			u.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		}),
		downloader: manager.NewDownloader(client, func(d *manager.Downloader) { d.PartSize = 16 << 20 }),
		presigner:  s3.NewPresignClient(client),
	}, nil
}

// Check makes sure that deneb can reach the bucket, so that a wrong setting shows at start.
func (s *S3) Check(ctx context.Context) error {
	_, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), MaxKeys: aws.Int32(1)})
	if err != nil {
		return fmt.Errorf("list the bucket %s: %w", s.bucket, err)
	}
	return nil
}

func (s *S3) MoveFile(ctx context.Context, key, localPath string) error {
	if err := s.PutFile(ctx, key, localPath); err != nil {
		return err
	}
	return os.Remove(localPath)
}

func (s *S3) PutFile(ctx context.Context, key, localPath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = s.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String(ContentType(key)),
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

func (s *S3) LocalFile(ctx context.Context, key, dir string) (string, func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	target := filepath.Join(dir, "download"+path.Ext(key))
	file, err := os.Create(target)
	if err != nil {
		return "", nil, err
	}
	_, err = s.downloader.Download(ctx, file, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(target)
		return "", nil, fmt.Errorf("download %s: %w", key, err)
	}
	return target, func() { os.Remove(target) }, nil
}

// Serve sends a file of the bucket through deneb, with ranges. With Redirects, it
// sends players to signed URLs of the bucket for the files other than playlists and
// subtitles, which deneb sends itself because their URLs point back to deneb.
func (s *S3) Serve(w http.ResponseWriter, r *http.Request, key string) {
	if s.redirects && !isText(key) {
		signed, err := s.presigner.PresignGetObject(r.Context(), &s3.GetObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(key),
		}, s3.WithPresignExpires(redirectTTL))
		if err != nil {
			slog.Error("sign a URL of the bucket", "key", key, "error", err)
			http.Error(w, "could not reach the storage", http.StatusBadGateway)
			return
		}
		http.Redirect(w, r, signed.URL, http.StatusFound)
		return
	}
	input := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}
	if ranges := r.Header.Get("Range"); ranges != "" {
		input.Range = aws.String(ranges)
	}
	object, err := s.client.GetObject(r.Context(), input)
	if err != nil {
		var missing *types.NoSuchKey
		if errors.As(err, &missing) {
			http.NotFound(w, r)
			return
		}
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidRange" {
			http.Error(w, "the range is outside the file", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		slog.Error("read from the bucket", "key", key, "error", err)
		http.Error(w, "could not reach the storage", http.StatusBadGateway)
		return
	}
	defer object.Body.Close()
	w.Header().Set("Content-Type", ContentType(key))
	w.Header().Set("Accept-Ranges", "bytes")
	if object.ContentLength != nil {
		w.Header().Set("Content-Length", fmt.Sprint(*object.ContentLength))
	}
	status := http.StatusOK
	if object.ContentRange != nil {
		w.Header().Set("Content-Range", *object.ContentRange)
		status = http.StatusPartialContent
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	io.Copy(w, object.Body)
}

func (s *S3) DeletePrefix(ctx context.Context, prefix string) error {
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return err
		}
		if len(page.Contents) == 0 {
			continue
		}
		objects := make([]types.ObjectIdentifier, 0, len(page.Contents))
		for _, object := range page.Contents {
			objects = append(objects, types.ObjectIdentifier{Key: object.Key})
		}
		result, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		})
		if err != nil {
			// Some services refuse the checksum that the SDK sends with DeleteObjects,
			// or do not have it. Removing the files one by one works everywhere.
			if ctx.Err() != nil {
				return err
			}
			if err := s.deleteEach(ctx, objects); err != nil {
				return err
			}
			continue
		}
		if len(result.Errors) > 0 {
			return fmt.Errorf("delete %s: %s", aws.ToString(result.Errors[0].Key), aws.ToString(result.Errors[0].Message))
		}
	}
	return nil
}

// deleteEach removes files one request at a time, a few at once.
func (s *S3) deleteEach(ctx context.Context, objects []types.ObjectIdentifier) error {
	keys := make(chan *string)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range keys {
				if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: key}); err != nil {
					select {
					case errs <- fmt.Errorf("delete %s: %w", aws.ToString(key), err):
					default:
					}
				}
			}
		}()
	}
	for _, object := range objects {
		keys <- object.Key
	}
	close(keys)
	wg.Wait()
	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
}
