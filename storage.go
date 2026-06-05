package storage

import (
	"fmt"
	"time"

	"github.com/yangguang/storage-go/driver/registry"
	"github.com/yangguang/storage-go/types"
)

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

func (c *Config) validate() error {
	if c.Driver == "" {
		return fmt.Errorf("%w: Driver is required", types.ErrInvalidConfig)
	}
	return nil
}

func New(cfg Config) (types.Storage, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	raw, ok := registry.Get(string(cfg.Driver))
	if !ok {
		return nil, fmt.Errorf("%w: driver %q not registered (did you forget blank import?)",
			types.ErrInvalidConfig, cfg.Driver)
	}
	f, ok := raw.(func(Config) (types.Storage, error))
	if !ok {
		return nil, fmt.Errorf("%w: driver %q factory signature mismatch",
			types.ErrInvalidConfig, cfg.Driver)
	}
	return f(cfg)
}
