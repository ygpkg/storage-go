package storage

import "time"

type DriverType string

const (
	DriverMinio  DriverType = "minio"
	DriverCOS    DriverType = "cos"
	DriverWeedFS DriverType = "weedfs"
	DriverLocal  DriverType = "local"
)

type Config struct {
	Driver DriverType

	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool

	PublicDomain string

	BaseDir string

	Timeout      time.Duration
	MaxRetries   int
	ExtraOptions map[string]string
}
