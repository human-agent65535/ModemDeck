package recording

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRejectsTraversalAndSymlinkedCallDirectory(t *testing.T) {
	files, err := newFileStore(filepath.Join(t.TempDir(), "recordings"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.close() })
	for _, relative := range []string{
		"../segment.ogg",
		"call/../../segment.ogg",
		"/call/segment.ogg",
		"call/not-ogg.wav",
	} {
		if _, _, err := parseRelativeRecording(relative); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("parseRelativeRecording(%q) error = %v, want ErrInvalidArgument", relative, err)
		}
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(files.root, "call-symlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := files.createSegment(
		"call-symlink",
		"segment",
	); !errors.Is(err, ErrStorage) {
		t.Fatalf("createSegment() symlink error = %v, want ErrStorage", err)
	}

	segment, err := files.createSegment("call-replaced", "segment")
	if err != nil {
		t.Fatal(err)
	}
	relative := segment.relative
	if err := segment.abort(); err != nil {
		t.Fatal(err)
	}
	callDirectory := filepath.Join(files.root, "call-replaced")
	if err := os.Remove(callDirectory); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "segment.ogg")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, callDirectory); err != nil {
		t.Fatal(err)
	}
	if _, err := files.open(relative); !errors.Is(err, ErrStorage) {
		t.Fatalf("open() replaced directory error = %v, want ErrStorage", err)
	}
}

func TestFileStoreCrashCleanupRemovesOnlyPartialRecordings(t *testing.T) {
	files, err := newFileStore(filepath.Join(t.TempDir(), "recordings"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.close() })
	callDirectory := filepath.Join(files.root, "call-cleanup")
	if err := os.Mkdir(callDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	relative := "call-cleanup/segment-cleanup.ogg"
	temporary := filepath.Join(callDirectory, ".segment-cleanup.ogg.part")
	final := filepath.Join(callDirectory, "segment-cleanup.ogg")
	if err := os.WriteFile(temporary, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := files.cleanupPartialFiles(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(temporary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(final); err != nil {
		t.Fatalf("ready recording was removed: %v", err)
	}
	if err := os.Chmod(final, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := files.open(relative); !errors.Is(err, ErrStorage) {
		t.Fatalf("open() insecure mode error = %v, want ErrStorage", err)
	}
	if err := os.Chmod(final, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := files.open(relative)
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
}
