package cos

import (
	"os"
	"testing"

	"github.com/yangguang/storage-go/driver/storagetest"
)

func TestCosIntegration(t *testing.T) {
	endpoint := os.Getenv("TEST_COS_ENDPOINT")
	if endpoint == "" {
		t.Skip("set TEST_COS_ENDPOINT to enable integration test")
	}
	d, err := New(Config{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("TEST_COS_ACCESS_KEY"),
		SecretKey: os.Getenv("TEST_COS_SECRET_KEY"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	storagetest.RunSuite(t, d, "test-bucket")
}
