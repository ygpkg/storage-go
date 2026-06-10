package seaweedfs

import (
	"os"
	"testing"

	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/storage-go/testkit"
)

func TestSeaweedFSIntegration(t *testing.T) {
	endpoint := os.Getenv("TEST_WEEDFS_ENDPOINT")
	if endpoint == "" {
		t.Skip("set TEST_WEEDFS_ENDPOINT to enable integration test")
	}
	d, err := New(storage.Config{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("TEST_WEEDFS_ACCESS_KEY"),
		SecretKey: os.Getenv("TEST_WEEDFS_SECRET_KEY"),
		UseSSL:    false,
	}, &storage.S3PathBuilder{
		Endpoint: endpoint,
		Format:   storage.URLFormatS3,
	})
	if err != nil {
		t.Fatal(err)
	}
	testkit.RunSuite(t, d, "test-bucket")
}
