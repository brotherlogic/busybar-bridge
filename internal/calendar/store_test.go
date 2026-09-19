package calendar_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/brotherlogic/busybar-bridge/internal/calendar"
	"github.com/brotherlogic/busybar-bridge/pkg/pb"
	"google.golang.org/protobuf/proto"
)

func TestNewStore_EmptyPath(t *testing.T) {
	_, err := calendar.NewStore("")
	if !errors.Is(err, calendar.ErrEmptyPath) {
		t.Errorf("expected ErrEmptyPath, got %v", err)
	}
}

func TestNewStore_NonExistentFile(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "calendar.pb")

	store, err := calendar.NewStore(storePath)
	if err != nil {
		t.Fatalf("unexpected error creating store with non-existent file: %v", err)
	}

	if store.IsBound() {
		t.Errorf("expected store to be unbound, got bound")
	}

	binding, ok := store.Get()
	if ok {
		t.Errorf("expected Get() ok to be false, got true")
	}
	if binding != nil {
		t.Errorf("expected Get() binding to be nil, got %v", binding)
	}
}

func TestStore_SaveAndReload(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "calendar.pb")

	store, err := calendar.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	sampleBinding := &pb.CalendarBinding{
		Email:           "testuser@example.com",
		CalendarId:      "primary",
		AccessToken:     "ya29.sample-token",
		RefreshToken:    "1//refresh-sample",
		TokenExpiryUnix: 1710003600,
		LinkedAtUnix:    1710000000,
	}

	if err := store.Save(sampleBinding); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file mode 0600
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("os.Stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected file mode 0600, got %04o", perm)
	}

	// Verify in-memory state
	if !store.IsBound() {
		t.Errorf("expected store to be bound after Save")
	}
	cached, ok := store.Get()
	if !ok || cached == nil {
		t.Fatalf("expected Get() to return binding")
	}
	if !proto.Equal(cached, sampleBinding) {
		t.Errorf("cached binding mismatch: got %v, want %v", cached, sampleBinding)
	}

	// Reload from disk with a fresh store instance
	reloadedStore, err := calendar.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore reload failed: %v", err)
	}
	if !reloadedStore.IsBound() {
		t.Errorf("expected reloaded store to be bound")
	}
	reloadedBinding, ok := reloadedStore.Get()
	if !ok || reloadedBinding == nil {
		t.Fatalf("reloaded store Get() returned nil")
	}
	if !proto.Equal(reloadedBinding, sampleBinding) {
		t.Errorf("reloaded binding mismatch: got %v, want %v", reloadedBinding, sampleBinding)
	}
}

func TestStore_SaveCreatesDirectory(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "nested", "sub", "dir", "calendar.pb")

	store, err := calendar.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	binding := &pb.CalendarBinding{
		Email:      "nested@example.com",
		CalendarId: "primary",
	}

	if err := store.Save(binding); err != nil {
		t.Fatalf("Save failed to create directory structure: %v", err)
	}

	if !store.IsBound() {
		t.Errorf("expected store to be bound")
	}
}

func TestStore_Immutability_ErrAlreadyBound(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "calendar.pb")

	store, err := calendar.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	binding1 := &pb.CalendarBinding{
		Email:      "first@example.com",
		CalendarId: "primary",
	}
	if err := store.Save(binding1); err != nil {
		t.Fatalf("first Save failed: %v", err)
	}

	binding2 := &pb.CalendarBinding{
		Email:      "second@example.com",
		CalendarId: "secondary",
	}
	err = store.Save(binding2)
	if err == nil {
		t.Fatalf("expected Save to fail when already bound, got nil")
	}
	if !errors.Is(err, calendar.ErrAlreadyBound) {
		t.Errorf("expected ErrAlreadyBound, got %v", err)
	}

	// Verify original binding is unchanged
	got, ok := store.Get()
	if !ok || got.GetEmail() != "first@example.com" {
		t.Errorf("expected email to remain first@example.com, got %v", got)
	}
}

func TestStore_CloningPreventsCallerMutation(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "calendar.pb")

	store, err := calendar.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	original := &pb.CalendarBinding{
		Email:      "original@example.com",
		CalendarId: "primary",
	}
	if err := store.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Mutating the original struct passed to Save should not affect store
	original.Email = "mutated@example.com"
	got, _ := store.Get()
	if got.GetEmail() != "original@example.com" {
		t.Errorf("store internal state was mutated via original pointer: got %s, want original@example.com", got.GetEmail())
	}

	// Mutating the struct returned by Get() should not affect store
	got.Email = "mutated-again@example.com"
	gotAgain, _ := store.Get()
	if gotAgain.GetEmail() != "original@example.com" {
		t.Errorf("store internal state was mutated via Get() return pointer: got %s, want original@example.com", gotAgain.GetEmail())
	}
}

func TestStore_CorruptedFile(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "corrupted.pb")

	// Write invalid non-protobuf binary data
	if err := os.WriteFile(storePath, []byte("not a valid protobuf message"), 0600); err != nil {
		t.Fatalf("failed to write corrupted file: %v", err)
	}

	store, err := calendar.NewStore(storePath)
	if err == nil {
		t.Fatalf("expected NewStore to fail on corrupted file, got store: %v", store)
	}
}

func TestStore_SaveNilBinding(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "calendar.pb")

	store, err := calendar.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	if err := store.Save(nil); err == nil {
		t.Errorf("expected Save(nil) to return error, got nil")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	storePath := filepath.Join(tempDir, "calendar.pb")

	store, err := calendar.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})

	// 10 goroutines racing to Save
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			_ = store.Save(&pb.CalendarBinding{
				Email:      "concurrent@example.com",
				CalendarId: "primary",
			})
		}(i)
	}

	// 20 goroutines reading Get and IsBound concurrently
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = store.IsBound()
			_, _ = store.Get()
		}()
	}

	close(start)
	wg.Wait()

	if !store.IsBound() {
		t.Errorf("expected store to be bound after concurrent saves")
	}
	binding, ok := store.Get()
	if !ok || binding.GetEmail() != "concurrent@example.com" {
		t.Errorf("unexpected binding after concurrent saves: %v", binding)
	}
}
