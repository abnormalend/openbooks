package dcc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/evan-buss/openbooks/mock"
)

// TestStringParsing makes sure that data is properly extracted from the DCC
// response string. (filename, IP conversion, port, and size)
func TestStringParsing(t *testing.T) {
	tables := []struct {
		search   string
		download *Download
	}{
		{
			":SearchOok!ook@only.ook PRIVMSG evan_28 :DCC SEND SearchOok_results_for__hp_lovecraft.txt.zip 1543751478 2043 784",
			&Download{Filename: "SearchOok_results_for__hp_lovecraft.txt.zip", IP: "92.3.199.54", Port: "2043", Size: 784},
		},
		{
			":Search!Search@ihw-4q5hcb.dyn.suddenlink.net PRIVMSG evan_bot :DCC SEND SearchBot_results_for__stephen_king_the_stand.txt.zip 2907707975 4342 1116",
			&Download{Filename: "SearchBot_results_for__stephen_king_the_stand.txt.zip", IP: "173.80.26.71", Port: "4342", Size: 1116},
		},
		{
			`:DV8!HandyAndy@ihw-39fkft.ip-164-132-173.eu PRIVMSG negative-bishop-1 :DCC SEND "Douglas Adams - [HITCHHIKER'S GUIDE TO THE GALAXY & THE 01] - Hitchhiker's Guide to the Galaxy & The (v5.0) (EPUB).rar" 2760158537 2050 2321788`,
			&Download{Filename: "Douglas Adams - [HITCHHIKER'S GUIDE TO THE GALAXY & THE 01] - Hitchhiker's Guide to the Galaxy & The (v5.0) (EPUB).rar", IP: "164.132.173.73", Port: "2050", Size: 2321788},
		},
	}

	for _, table := range tables {
		download, err := ParseString(table.search)
		require.NoError(t, err)
		assert.Equal(t, table.download, download)
	}
}

func TestDownload(t *testing.T) {
	text := "Test dcc download content."

	textDownload := Download{
		Filename: "test.txt",
		IP:       "localhost",
		Port:     "6969",
		Size:     int64(len(text)),
	}

	reader := bytes.NewReader([]byte(text))
	server := mock.DccServer{
		Port:   ":" + textDownload.Port,
		Reader: reader,
	}

	ready := make(chan struct{}, 1)
	go server.Start(ready)
	<-ready

	t.Log("After server start")

	received := new(mock.WriteCloser)
	err := textDownload.Download(received)
	require.NoError(t, err)
	assert.Equal(t, text, string(received.Data))
}

func TestDownloadContextAbortsOnCancel(t *testing.T) {
	// A DCC server that announces a big size but dribbles bytes forever.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64)
		for {
			if _, err := conn.Write(buf); err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	ip, portStr, _ := net.SplitHostPort(l.Addr().String())
	var ipInt uint32
	for _, b := range net.ParseIP(ip).To4() {
		ipInt = ipInt<<8 | uint32(b)
	}
	port, _ := strconv.Atoi(portStr)
	d := Download{Filename: "x", IP: ip, Port: fmt.Sprintf("%d", port), Size: 1 << 30}
	_ = ipInt

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(80 * time.Millisecond); cancel() }()
	done := make(chan error, 1)
	go func() { done <- d.DownloadContext(ctx, io.Discard) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from a cancelled download")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DownloadContext did not return promptly after cancel")
	}
}
