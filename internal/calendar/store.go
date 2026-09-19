package calendar

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/brotherlogic/busybar-bridge/pkg/pb"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrAlreadyBound indicates that a calendar binding has already been established
	// and cannot be modified due to one-time binding immutability.
	ErrAlreadyBound = errors.New("calendar binding is already established and immutable")

	// ErrNilBinding indicates that a nil calendar binding was passed to Save.
	ErrNilBinding = errors.New("cannot save nil calendar binding")

	// ErrEmptyPath indicates that an empty store path was provided.
	ErrEmptyPath = errors.New("calendar store path cannot be empty")
)

// Store provides a thread-safe persistence store for Google Calendar bindings.
// It enforces one-time binding immutability once credentials have been saved.
type Store struct {
	mu      sync.RWMutex
	path    string
	binding *pb.CalendarBinding
}

// NewStore initializes a Store at the specified file path.
// If the file does not exist, it returns an unbound store.
// If the file exists, it unmarshals the binary protobuf into memory or returns an error on corruption.
func NewStore(path string) (*Store, error) {
	if path == "" {
		return nil, ErrEmptyPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Store{path: path}, nil
		}
		return nil, fmt.Errorf("failed to read calendar store file %s: %w", path, err)
	}

	var binding pb.CalendarBinding
	if err := proto.Unmarshal(data, &binding); err != nil {
		return nil, fmt.Errorf("failed to unmarshal calendar binding from %s: %w", path, err)
	}

	return &Store{
		path:    path,
		binding: &binding,
	}, nil
}

// IsBound returns true if a calendar binding exists in the store.
func (s *Store) IsBound() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.binding != nil
}

// Get returns a deep copy of the cached CalendarBinding and true if bound,
// or nil and false if unbound.
func (s *Store) Get() (*pb.CalendarBinding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.binding == nil {
		return nil, false
	}

	return proto.Clone(s.binding).(*pb.CalendarBinding), true
}

// Save atomically writes the CalendarBinding protobuf to disk with 0600 permissions
// and caches it in memory. If a binding already exists, ErrAlreadyBound is returned
// to preserve one-time immutability.
func (s *Store) Save(binding *pb.CalendarBinding) error {
	if binding == nil {
		return ErrNilBinding
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.binding != nil {
		return ErrAlreadyBound
	}

	data, err := proto.Marshal(binding)
	if err != nil {
		return fmt.Errorf("failed to marshal calendar binding: %w", err)
	}

	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("failed to create directory for calendar store %s: %w", dir, err)
		}
	}

	tmpFile, err := os.CreateTemp(dir, "calendar-binding-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}

	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write calendar binding data: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temporary file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	if err := os.Chmod(tmpPath, 0600); err != nil {
		return fmt.Errorf("failed to chmod temporary file to 0600: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("failed to rename temporary file to %s: %w", s.path, err)
	}

	cleanup = false
	s.binding = proto.Clone(binding).(*pb.CalendarBinding)
	return nil
}
