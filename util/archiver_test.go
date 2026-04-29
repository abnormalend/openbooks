package util

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create entry %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func TestIsArchiveRecognizesZip(t *testing.T) {
	if !IsArchive("foo.zip") {
		t.Error("expected foo.zip to be recognized as archive")
	}
	if !IsArchive("foo.zip.temp") {
		t.Error("expected foo.zip.temp (with .temp suffix) to be recognized")
	}
}

func TestIsArchiveRejectsNonArchive(t *testing.T) {
	if IsArchive("foo.epub") {
		t.Error("foo.epub is not an archive")
	}
	if IsArchive("foo.epub.temp") {
		t.Error("foo.epub.temp is not an archive")
	}
}

func TestExtractArchiveSingleFile(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "book.zip.temp")
	writeZip(t, zipPath, map[string]string{"gatsby.epub": "epub bytes"})

	out, err := ExtractArchive(zipPath)
	if err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}

	wantSuffix := "gatsby.epub.temp"
	if filepath.Base(out) != wantSuffix {
		t.Errorf("extracted path = %q, want suffix %q", out, wantSuffix)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("extracted file does not exist: %v", err)
	}
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Error("expected original archive to be removed after single-file extract")
	}
}

// Regression test for upstream PR #187 / commit ad12382:
// archives with multiple files are NOT extracted; the archive itself
// is delivered to the user.
func TestExtractArchiveMultiFileReturnsArchive(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "books.zip.temp")
	writeZip(t, zipPath, map[string]string{
		"book-1.epub": "first",
		"book-2.epub": "second",
	})

	out, err := ExtractArchive(zipPath)
	if err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	if out != zipPath {
		t.Errorf("multi-file archive should return original path; got %q want %q", out, zipPath)
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Errorf("original archive should remain on disk: %v", err)
	}
}
