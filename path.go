package storage

type AccessScheme string

const (
	SchemeS3   AccessScheme = "s3"
	SchemeFile AccessScheme = "file"
)

type StoragePath interface {
	Path() string
	URL() string
}
