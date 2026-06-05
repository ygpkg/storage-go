package registry

import (
	"errors"
	"testing"
)

type stubStorage struct{}

func (s *stubStorage) Close() error { return nil }

func TestRegisterAndGet(t *testing.T) {
	defer Reset()
	Register("stub", func(cfg any) (any, error) { return &stubStorage{}, nil })

	f, ok := Get("stub")
	if !ok {
		t.Fatal("Get(stub) should return true")
	}
	fn, ok := f.(func(any) (any, error))
	if !ok {
		t.Fatalf("factory has wrong type: %T", f)
	}
	s, err := fn(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*stubStorage); !ok {
		t.Errorf("got %T, want *stubStorage", s)
	}
}

func TestGetUnknown(t *testing.T) {
	_, ok := Get("not-exists")
	if ok {
		t.Error("Get(not-exists) should return false")
	}
}

func TestRegisterOverwrite(t *testing.T) {
	defer Reset()
	Register("dup", func(cfg any) (any, error) { return &stubStorage{}, nil })
	Register("dup", func(cfg any) (any, error) { return nil, errors.New("second") })

	f, _ := Get("dup")
	fn := f.(func(any) (any, error))
	_, err := fn(nil)
	if err == nil || err.Error() != "second" {
		t.Errorf("err = %v, want 'second'", err)
	}
}
