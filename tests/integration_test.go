//go:build integration

// Package tests contains build-tagged integration tests that drive the
// real Go pipeline (irc.Conn -> core.StartReader -> core.DownloadExtractDCCString
// -> core.ParseSearchFile) against in-process fakes. They only run when the
// `integration` build tag is set:
//
//	go test -tags=integration ./...
//
// Unit tests under -race must stay fast; this suite is allowed to do TCP
// listens and short sleeps.
package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evan-buss/openbooks/core"
	"github.com/evan-buss/openbooks/irc"
)

const sampleSearchResults = `Search results from SearchBot v3
!DV8 F. Scott Fitzgerald - The Great Gatsby (Epub).rar  ::INFO:: 394.7KB
!Horla F Scott Fitzgerald - The Great Gatsby (retail) (epub).epub  ::INFO:: 1.2MB
!Bot J. R. R. Tolkien - The Hobbit.epub  ::INFO:: 800KB
`

func makeZip(t *testing.T, name, contents string) []byte {
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

// startDccServer accepts one connection, writes payload, and closes.
func startDccServer(t *testing.T, payload []byte) (port string, stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen dcc: %v", err)
	}
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
	return fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port), func() {
		l.Close()
		wg.Wait()
	}
}

// startIrcServer accepts one IRC client, swallows USER/NICK/JOIN/PRIVMSG
// chatter until it sees an "@search" request, then replies with a
// DCC SEND pointing at dccPort. 2130706433 is the int form of 127.0.0.1.
func startIrcServer(t *testing.T, dccPort string, payloadSize int) (addr string, stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen irc: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			if strings.Contains(string(buf[:n]), "@search") {
				fmt.Fprintf(conn,
					":SearchBot!u@h PRIVMSG user :DCC SEND SearchBot_results_for__gatsby.txt.zip 2130706433 %s %d\r\n",
					dccPort, payloadSize)
				return
			}
		}
	}()
	return fmt.Sprintf("127.0.0.1:%d", l.Addr().(*net.TCPAddr).Port), func() {
		l.Close()
		wg.Wait()
	}
}

// TestSearchToBookDetails exercises the full search pipeline:
// irc.Conn.Connect -> core.SearchBook (writes IRC PRIVMSG) ->
// fake IRC server replies with DCC SEND -> StartReader classifies it as
// SearchResult -> core.DownloadExtractDCCString downloads + extracts ->
// core.ParseSearchFile yields BookDetail records.
func TestSearchToBookDetails(t *testing.T) {
	payload := makeZip(t, "results.txt", sampleSearchResults)
	dccPort, stopDcc := startDccServer(t, payload)
	defer stopDcc()

	ircAddr, stopIrc := startIrcServer(t, dccPort, len(payload))
	defer stopIrc()

	conn := irc.New("evan_28", "OpenBooks integration")
	if err := conn.Connect(ircAddr, false); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Disconnect()

	received := make(chan string, 1)
	handler := core.EventHandler{
		core.SearchResult: func(text string) { received <- text },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go core.StartReader(ctx, conn, handler)

	core.SearchBook(conn, "search", "the great gatsby")

	var dccString string
	select {
	case dccString = <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("never received SearchResult event from fake IRC server")
	}

	tmp := t.TempDir()
	extractedPath, err := core.DownloadExtractDCCString(tmp, dccString, nil)
	if err != nil {
		t.Fatalf("DownloadExtractDCCString: %v", err)
	}

	books, parseErrors, err := core.ParseSearchFile(extractedPath)
	if err != nil {
		t.Fatalf("ParseSearchFile: %v", err)
	}
	if len(parseErrors) > 0 {
		// Sample data is hand-crafted to be clean; any error is unexpected.
		for _, e := range parseErrors {
			t.Logf("parse error: %v", e)
		}
		t.Errorf("expected 0 parse errors, got %d", len(parseErrors))
	}
	if len(books) != 3 {
		t.Fatalf("expected 3 books, got %d: %+v", len(books), books)
	}

	// Spot-check that author/title/format made it through end-to-end.
	gatsbys := 0
	hobbits := 0
	for _, b := range books {
		if strings.Contains(b.Title, "Great Gatsby") {
			gatsbys++
			if b.Format != "epub" {
				t.Errorf("Great Gatsby format = %q, want epub", b.Format)
			}
		}
		if strings.Contains(b.Title, "Hobbit") {
			hobbits++
			if !strings.Contains(b.Author, "Tolkien") {
				t.Errorf("Hobbit author = %q, want Tolkien", b.Author)
			}
		}
	}
	if gatsbys != 2 {
		t.Errorf("expected 2 Gatsby results, got %d", gatsbys)
	}
	if hobbits != 1 {
		t.Errorf("expected 1 Hobbit result, got %d", hobbits)
	}
}
