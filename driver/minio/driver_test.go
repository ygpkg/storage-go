package minio

import (
	"os"
	"testing"

	"github.com/insmtx/storage-go"
	"github.com/insmtx/storage-go/driver/storagetest"
)

func TestMinioIntegration(t *testing.T) {
	endpoint := os.Getenv("TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("set TEST_MINIO_ENDPOINT to enable integration test")
	}
	d, err := New(storage.Config{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("TEST_MINIO_ACCESS_KEY"),
		SecretKey: os.Getenv("TEST_MINIO_SECRET_KEY"),
		UseSSL:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	storagetest.RunSuite(t, d, "test-bucket")
}
