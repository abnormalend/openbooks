package irc

import (
	"io"
	"net"
	"testing"
	"time"
)

// fakeNetConn satisfies net.Conn with in-memory read input and a buffer
// capturing every Write. Read returns io.EOF when input is exhausted.
type fakeNetConn struct {
	in     []byte
	out    []byte
	closed bool
}

func (f *fakeNetConn) Read(p []byte) (int, error) {
	if len(f.in) == 0 {
		return 0, io.EOF
	}
	n := copy(p, f.in)
	f.in = f.in[n:]
	return n, nil
}

func (f *fakeNetConn) Write(p []byte) (int, error) {
	if f.closed {
		return 0, io.ErrClosedPipe
	}
	f.out = append(f.out, p...)
	return len(p), nil
}

func (f *fakeNetConn) Close() error                       { f.closed = true; return nil }
func (f *fakeNetConn) LocalAddr() net.Addr                { return nil }
func (f *fakeNetConn) RemoteAddr() net.Addr               { return nil }
func (f *fakeNetConn) SetDeadline(time.Time) error        { return nil }
func (f *fakeNetConn) SetReadDeadline(time.Time) error    { return nil }
func (f *fakeNetConn) SetWriteDeadline(time.Time) error   { return nil }

func newTestConn() (*Conn, *fakeNetConn) {
	f := &fakeNetConn{}
	return &Conn{Conn: f, Username: "tester"}, f
}

func TestIsConnectedFalseOnFreshConn(t *testing.T) {
	c := New("user", "OpenBooks 4.3.0")
	if c.IsConnected() {
		t.Error("expected IsConnected() to be false before Connect()")
	}
}

func TestIsConnectedTrueWithEmbeddedConn(t *testing.T) {
	c, _ := newTestConn()
	if !c.IsConnected() {
		t.Error("expected IsConnected() to be true when Conn is set")
	}
}

func TestJoinChannelWritesAndUpdatesState(t *testing.T) {
	c, f := newTestConn()
	c.JoinChannel("ebooks")
	if got := string(f.out); got != "JOIN #ebooks\r\n" {
		t.Errorf("got %q, want %q", got, "JOIN #ebooks\r\n")
	}
	if c.channel != "ebooks" {
		t.Errorf("internal channel = %q, want %q", c.channel, "ebooks")
	}
}

func TestSendMessageTargetsCurrentChannel(t *testing.T) {
	c, f := newTestConn()
	c.JoinChannel("ebooks")
	c.SendMessage("@search the great gatsby")
	want := "JOIN #ebooks\r\nPRIVMSG #ebooks :@search the great gatsby\r\n"
	if got := string(f.out); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSendNoticeWritesToUser(t *testing.T) {
	c, f := newTestConn()
	c.SendNotice("evanbot", "\x01VERSION OpenBooks 4.3.0\x01")
	want := "NOTICE evanbot :\x01VERSION OpenBooks 4.3.0\x01\r\n"
	if got := string(f.out); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPongWritesPong(t *testing.T) {
	c, f := newTestConn()
	c.Pong("server.example")
	want := "PONG server.example\r\n"
	if got := string(f.out); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGetUsersWritesNames(t *testing.T) {
	c, f := newTestConn()
	c.GetUsers("ebooks")
	want := "NAMES #ebooks\r\n"
	if got := string(f.out); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDisconnectClosesAndQuits(t *testing.T) {
	c, f := newTestConn()
	c.Disconnect()
	want := "QUIT :Goodbye\r\n"
	if got := string(f.out); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if !f.closed {
		t.Error("expected underlying conn to be Closed")
	}
}

func TestWritesAreNoopWhenDisconnected(t *testing.T) {
	c := New("user", "OpenBooks 4.3.0")
	// IsConnected() is false; methods should early-return without panic.
	c.SendMessage("hello")
	c.SendNotice("foo", "bar")
	c.JoinChannel("ebooks")
	c.GetUsers("ebooks")
	c.Pong("server")
	c.Disconnect()
}
