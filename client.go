package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"
)

// MultipartThreshold 对象大小低于该值时 Client.UploadObject 自动降级为 PutObject。
const MultipartThreshold = 128 << 20 // 128MB

const (
	defaultChunkSize   int64         = 32 << 20 // 32MB
	defaultConcurrency int           = 5
	defaultPartSize    int64         = 5 << 20 // 5MB, S3 最小分片大小
	uploadMaxRetries                = 3
	uploadBackoffBase time.Duration = 200 * time.Millisecond
)

// Client 注入 Bucket 后的高层封装，提供 UploadObject 等便利方法。
type Client struct {
	Storage
	bucket string
}

// NewClient 等价 New(cfg) + 注入 cfg.Bucket 字段。
func NewClient(cfg Config) (*Client, error) {
	s, err := New(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Bucket == "" {
		return nil, wrapInvalidConfig("Bucket is required for NewClient")
	}
	return &Client{Storage: s, bucket: cfg.Bucket}, nil
}

// Bucket 返回注入的 bucket 名称。
func (c *Client) Bucket() string { return c.bucket }

// UploadObject 高层分片上传封装。
//   - size < MultipartThreshold：自动降级为 PutObject
//   - size >= MultipartThreshold：分片 + errgroup 并发上传，任一失败触发 Abort
//   - 单一分片上传失败时按 200ms→400ms→800ms 退避重试最多 cfg.MaxRetries 次
func (c *Client) UploadObject(ctx context.Context, key string, body io.Reader, size int64, opts ...UploadOption) (*PutObjectResult, error) {
	uOpts := &UploadOptions{
		ObjectSize:  size,
		ChunkSize:   defaultChunkSize,
		Concurrency: defaultConcurrency,
	}
	for _, opt := range opts {
		opt(uOpts)
	}

	if uOpts.ChunkSize <= 0 {
		uOpts.ChunkSize = defaultChunkSize
	}
	if uOpts.Concurrency <= 0 {
		uOpts.Concurrency = defaultConcurrency
	}

	putOpts := uOpts.PutOptions
	if size > 0 && size < MultipartThreshold {
		return c.PutObject(ctx, c.bucket, key, body, putOpts...)
	}

	uploadID, err := c.CreateMultipartUpload(ctx, c.bucket, key, putOpts...)
	if err != nil {
		return nil, err
	}

	abortOnErr := func(retErr error) error {
		if abortErr := c.AbortMultipartUpload(ctx, c.bucket, key, uploadID); abortErr != nil && retErr != nil {
			return fmt.Errorf("%w; abort failed: %v", retErr, abortErr)
		}
		return retErr
	}

	parts, err := c.uploadParts(ctx, key, uploadID, body, size, uOpts.ChunkSize, uOpts.Concurrency)
	if err != nil {
		return nil, abortOnErr(err)
	}

	if err := c.CompleteMultipartUpload(ctx, c.bucket, key, uploadID, parts); err != nil {
		return nil, abortOnErr(err)
	}

	info, err := c.HeadObject(ctx, c.bucket, key)
	if err != nil {
		return nil, abortOnErr(err)
	}
	return &PutObjectResult{Path: info.Path, ETag: info.ETag}, nil
}

// uploadParts 把 body 切成 chunks，errgroup 并发上传，parts 按 PartNumber 升序返回。
func (c *Client) uploadParts(
	ctx context.Context, key, uploadID string, body io.Reader, size, chunkSize int64, concurrency int,
) ([]CompletedPart, error) {
	if size <= 0 {
		return nil, fmt.Errorf("%w: size must be > 0 for multipart upload", ErrInvalidConfig)
	}

	if chunkSize < defaultPartSize {
		chunkSize = defaultPartSize
	}

	parts, err := readAllParts(body, size, chunkSize)
	if err != nil {
		return nil, err
	}

	results := make([]CompletedPart, len(parts))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for i, p := range parts {
		i, p := i, p
		g.Go(func() error {
			var lastErr error
			for attempt := 0; attempt <= uploadMaxRetries; attempt++ {
				if attempt > 0 {
					delay := uploadBackoffBase * (1 << (attempt - 1))
					select {
					case <-gCtx.Done():
						return gCtx.Err()
					case <-time.After(delay):
					}
				}
				part, err := c.UploadPart(gCtx, c.bucket, key, uploadID, p.number, p.reader)
				if err == nil {
					results[i] = *part
					return nil
				}
				lastErr = err
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
			}
			return fmt.Errorf("upload part %d: %w", p.number, lastErr)
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool { return results[i].PartNumber < results[j].PartNumber })
	return results, nil
}

type partChunk struct {
	number int
	reader io.Reader
}

func readAllParts(body io.Reader, size, chunkSize int64) ([]partChunk, error) {
	if size <= 0 {
		return nil, nil
	}
	numParts := int((size + chunkSize - 1) / chunkSize)
	parts := make([]partChunk, 0, numParts)
	offset := int64(0)
	for i := 0; i < numParts; i++ {
		end := offset + chunkSize
		if end > size {
			end = size
		}
		partSize := end - offset
		partBuf := make([]byte, partSize)
		if _, err := io.ReadFull(body, partBuf); err != nil {
			return nil, fmt.Errorf("read part %d: %w", i+1, err)
		}
		parts = append(parts, partChunk{number: i + 1, reader: bytes.NewReader(partBuf)})
		offset = end
	}
	return parts, nil
}

// ListPager 翻页器，对齐 AWS SDK ListObjectsV2Paginator 形态。
type ListPager interface {
	HasMore() bool
	NextPage(ctx context.Context) (*ListObjectsOutput, error)
}

// NewListObjectsPaginator 构造 ListPager。
func NewListObjectsPaginator(s Core, ctx context.Context, bucket, prefix string, opts ...ListOption) (ListPager, error) {
	o := &ListOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return &listPager{
		core:    s,
		ctx:     ctx,
		bucket:  bucket,
		prefix:  prefix,
		options: o,
	}, nil
}

type listPager struct {
	core      Core
	ctx       context.Context
	bucket    string
	prefix    string
	options   *ListOptions
	token     string
	firstPage bool
	finished  bool
	lastOut   *ListObjectsOutput
}

func (p *listPager) HasMore() bool {
	if p.firstPage {
		return true
	}
	return p.lastOut != nil && p.lastOut.IsTruncated
}

func (p *listPager) NextPage(ctx context.Context) (*ListObjectsOutput, error) {
	if p.finished {
		return nil, io.EOF
	}
	opts := []ListOption{}
	if p.token != "" {
		opts = append(opts, WithStartAfter(p.token))
	}
	if p.options != nil {
		if p.options.MaxKeys > 0 {
			opts = append(opts, WithMaxKeys(p.options.MaxKeys))
		}
		if p.options.StartAfter != "" && p.token == "" {
			opts = append(opts, WithStartAfter(p.options.StartAfter))
		}
		if p.options.Recursive {
			opts = append(opts, WithRecursive(true))
		}
	}
	out, err := p.core.ListObjects(ctx, p.bucket, p.prefix, opts...)
	if err != nil {
		return nil, err
	}
	p.firstPage = false
	p.lastOut = out
	if !out.IsTruncated {
		p.finished = true
	} else {
		p.token = out.NextContinuationToken
	}
	return out, nil
}
