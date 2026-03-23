package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreUpdatePersistsChanges(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))

	updated, err := store.Update(func(st *FileState) error {
		st.MarkCapture("2026-03-09", time.Date(2026, 3, 9, 15, 0, 0, 0, time.UTC))
		return nil
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.LastCaptureTime("2026-03-09").IsZero() {
		t.Fatalf("expected capture timestamp to be set")
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.LastCaptureTime("2026-03-09").IsZero() {
		t.Fatalf("expected persisted capture timestamp")
	}
}
