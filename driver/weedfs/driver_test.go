package weedfs

import (
	"os"
	"testing"

	"github.com/insmtx/storage-go"
	"github.com/insmtx/storage-go/driver/storagetest"
)

func TestWeedfsIntegration(t *testing.T) {
	endpoint := os.Getenv("TEST_WEEDFS_ENDPOINT")
	if endpoint == "" {
		t.Skip("set TEST_WEEDFS_ENDPOINT to enable integration test")
	}
	d, err := New(storage.Config{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("TEST_WEEDFS_ACCESS_KEY"),
		SecretKey: os.Getenv("TEST_WEEDFS_SECRET_KEY"),
		UseSSL:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	storagetest.RunSuite(t, d, "test-bucket")
}
