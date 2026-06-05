package storage

import (
	"bytes"
	"context"
	"io"
	"sort"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/yangguang/storage-go/types"
)

type Client struct {
	s      types.Storage
	bucket string
}

func NewClient(s types.Storage, bucket string) *Client {
	return &Client{s: s, bucket: bucket}
}

func (c *Client) SetDefaultBucket(bucket string) { c.bucket = bucket }

func (c *Client) PutObject(ctx context.Context, key string, r io.Reader, size int64, opts ...PutOption) (*types.ObjectMeta, error) {
	return c.s.PutObject(ctx, c.bucket, key, r, size, opts...)
}

func (c *Client) Close() error { return c.s.Close() }

func WithObjectSize(n int64) UploadOption  { return types.WithObjectSize(n) }
func WithChunkSize(n int64) UploadOption   { return types.WithChunkSize(n) }
func WithConcurrency(n int) UploadOption   { return types.WithConcurrency(n) }
func WithMultipartThreshold(n int64) UploadOption {
	return func(o *UploadOptions) { o.MultipartThreshold = n }
}

func (c *Client) UploadObject(ctx context.Context, key string, r io.Reader, size int64, opts ...UploadOption) (*types.ObjectMeta, error) {
	o := types.DefaultUploadOptions()
	for _, opt := range opts {
		opt(o)
	}

	if size > 0 && size < o.MultipartThreshold {
		return c.s.PutObject(ctx, c.bucket, key, r, size)
	}

	uploadID, err := c.s.CreateMultipartUpload(ctx, c.bucket, key)
	if err != nil {
		return nil, err
	}

	var (
		parts     []types.PartInfo
		partsMu   sync.Mutex
		eg, egCtx = errgroup.WithContext(ctx)
		sem       = make(chan struct{}, o.Concurrency)
		partNum   int32
	)

	buf := make([]byte, o.ChunkSize)
	for {
		n, readErr := io.ReadFull(r, buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			pn := int(atomic.AddInt32(&partNum, 1))
			sem <- struct{}{}
			eg.Go(func() error {
				defer func() { <-sem }()
				part, err := c.s.UploadPart(egCtx, c.bucket, key, uploadID, pn, bytes.NewReader(chunk), int64(n))
				if err != nil {
					return err
				}
				partsMu.Lock()
				parts = append(parts, *part)
				partsMu.Unlock()
				return nil
			})
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			_ = eg.Wait()
			_ = c.s.AbortMultipartUpload(ctx, c.bucket, key, uploadID)
			return nil, readErr
		}
	}

	if err := eg.Wait(); err != nil {
		_ = c.s.AbortMultipartUpload(ctx, c.bucket, key, uploadID)
		return nil, err
	}

	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	return c.s.CompleteMultipartUpload(ctx, c.bucket, key, uploadID, parts)
}
