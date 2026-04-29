package core

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// startMockDcc spins up a local TCP listener that, on Accept, writes
// `payload` and closes. Returns the bound port and a stop function.
func startMockDcc(t *testing.T, payload []byte) (port string, stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write(payload)
	}()

	return fmt.Sprintf("%d", addr.Port), func() {
		l.Close()
		wg.Wait()
	}
}

// dccSendString builds a "DCC SEND" PRIVMSG pointing at 127.0.0.1:port.
// 2130706433 is the integer form of 127.0.0.1.
func dccSendString(filename, port string, size int) string {
	return fmt.Sprintf(":bot PRIVMSG user :DCC SEND %s 2130706433 %s %d", filename, port, size)
}

func makeSingleFileZip(t *testing.T, name, contents string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write([]byte(contents)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestDownloadExtractDCCStringPlainFile(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("epub bytes here")
	port, stop := startMockDcc(t, payload)
	defer stop()

	dcc := dccSendString("book.epub", port, len(payload))
	out, err := DownloadExtractDCCString(dir, dcc, nil)
	if err != nil {
		t.Fatalf("DownloadExtractDCCString: %v", err)
	}

	if filepath.Base(out) != "book.epub" {
		t.Errorf("output filename = %q, want book.epub", filepath.Base(out))
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", out, err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("file content = %q, want %q", got, payload)
	}
}

func TestDownloadExtractDCCStringExtractsSingleFileArchive(t *testing.T) {
	dir := t.TempDir()
	payload := makeSingleFileZip(t, "gatsby.epub", "epub bytes here")
	port, stop := startMockDcc(t, payload)
	defer stop()

	dcc := dccSendString("results.zip", port, len(payload))
	out, err := DownloadExtractDCCString(dir, dcc, nil)
	if err != nil {
		t.Fatalf("DownloadExtractDCCString: %v", err)
	}

	if filepath.Base(out) != "gatsby.epub" {
		t.Errorf("expected extracted 'gatsby.epub', got %q", filepath.Base(out))
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "epub bytes here" {
		t.Errorf("extracted content = %q, want %q", got, "epub bytes here")
	}
}

func TestDownloadExtractDCCStringInvalidDccString(t *testing.T) {
	dir := t.TempDir()
	_, err := DownloadExtractDCCString(dir, "this is not a dcc string", nil)
	if err == nil {
		t.Error("expected error on malformed DCC SEND, got nil")
	}
}
