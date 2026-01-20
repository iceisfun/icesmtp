package icesmtp

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// ErrDeadlineExceeded is returned when a read/write deadline is exceeded.
var ErrDeadlineExceeded = errors.New("deadline exceeded")

// Conn wraps a connection with deadline support and TLS upgrade capability.
// This abstraction allows the engine to work with both net.Conn and
// io.Reader/io.Writer pairs while supporting timeouts.
type Conn interface {
	io.Reader
	io.Writer
	io.Closer

	// SetReadDeadline sets the deadline for future Read calls.
	SetReadDeadline(t time.Time) error

	// SetWriteDeadline sets the deadline for future Write calls.
	SetWriteDeadline(t time.Time) error

	// UpgradeTLS upgrades the connection to TLS using the provided config.
	// Returns the negotiated TLS connection state.
	UpgradeTLS(config *tls.Config) (TLSConnectionState, error)

	// TLSConnectionState returns the TLS state if TLS is active, nil otherwise.
	TLSConnectionState() *TLSConnectionState
}

// NetConn wraps a net.Conn to implement the Conn interface.
type NetConn struct {
	conn     net.Conn
	tlsState *TLSConnectionState
}

// WrapNetConn wraps a net.Conn.
func WrapNetConn(conn net.Conn) *NetConn {
	return &NetConn{conn: conn}
}

func (c *NetConn) Read(p []byte) (n int, err error) {
	return c.conn.Read(p)
}

func (c *NetConn) Write(p []byte) (n int, err error) {
	return c.conn.Write(p)
}

func (c *NetConn) Close() error {
	return c.conn.Close()
}

func (c *NetConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *NetConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

func (c *NetConn) UpgradeTLS(config *tls.Config) (TLSConnectionState, error) {
	tlsConn := tls.Server(c.conn, config)
	if err := tlsConn.Handshake(); err != nil {
		return TLSConnectionState{}, &TLSError{
			Phase:   TLSErrorPhaseHandshake,
			Cause:   err,
			Message: "TLS handshake failed",
		}
	}

	cs := tlsConn.ConnectionState()
	state := TLSConnectionState{
		Version:          cs.Version,
		CipherSuite:      cs.CipherSuite,
		ServerName:       cs.ServerName,
		PeerCertificates: len(cs.PeerCertificates) > 0,
		VerifiedChains:   len(cs.VerifiedChains) > 0,
	}

	c.conn = tlsConn
	c.tlsState = &state
	return state, nil
}

func (c *NetConn) TLSConnectionState() *TLSConnectionState {
	return c.tlsState
}

// PipeConn wraps io.Reader/io.Writer pairs for testing.
// It supports deadline simulation via context cancellation.
type PipeConn struct {
	reader       io.Reader
	writer       io.Writer
	readDeadline time.Time
	mu           sync.Mutex
	closed       bool

	// For TLS testing, allow injecting a TLS upgrader
	tlsUpgrader func(*tls.Config) (io.Reader, io.Writer, TLSConnectionState, error)
	tlsState    *TLSConnectionState
}

// WrapPipe wraps an io.Reader and io.Writer as a Conn.
// This is useful for testing without actual network connections.
func WrapPipe(r io.Reader, w io.Writer) *PipeConn {
	return &PipeConn{
		reader: r,
		writer: w,
	}
}

// SetTLSUpgrader sets a custom TLS upgrade function for testing.
func (c *PipeConn) SetTLSUpgrader(fn func(*tls.Config) (io.Reader, io.Writer, TLSConnectionState, error)) {
	c.tlsUpgrader = fn
}

func (c *PipeConn) Read(p []byte) (n int, err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	deadline := c.readDeadline
	c.mu.Unlock()

	// If deadline is set and in the past, return immediately
	if !deadline.IsZero() && time.Now().After(deadline) {
		return 0, ErrDeadlineExceeded
	}

	// For pipe connections, we can't actually enforce deadlines on blocking reads
	// unless the underlying reader supports it (like our harness PipeBuffer).
	// In production, use NetConn which has proper deadline support.
	return c.reader.Read(p)
}

func (c *PipeConn) Write(p []byte) (n int, err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	c.mu.Unlock()
	return c.writer.Write(p)
}

func (c *PipeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true

	// Try to close underlying reader/writer if they implement io.Closer
	if closer, ok := c.reader.(io.Closer); ok {
		closer.Close()
	}
	if closer, ok := c.writer.(io.Closer); ok {
		closer.Close()
	}
	return nil
}

func (c *PipeConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t

	// If the reader supports SetReadDeadline (e.g., harness.PipeBuffer), use it
	if dl, ok := c.reader.(interface{ SetReadDeadline(time.Time) error }); ok {
		return dl.SetReadDeadline(t)
	}
	return nil
}

func (c *PipeConn) SetWriteDeadline(t time.Time) error {
	// If the writer supports SetWriteDeadline, use it
	if dl, ok := c.writer.(interface{ SetWriteDeadline(time.Time) error }); ok {
		return dl.SetWriteDeadline(t)
	}
	return nil
}

func (c *PipeConn) UpgradeTLS(config *tls.Config) (TLSConnectionState, error) {
	if c.tlsUpgrader != nil {
		r, w, state, err := c.tlsUpgrader(config)
		if err != nil {
			return TLSConnectionState{}, err
		}
		c.reader = r
		c.writer = w
		c.tlsState = &state
		return state, nil
	}
	return TLSConnectionState{}, &TLSError{
		Phase:   TLSErrorPhaseHandshake,
		Message: "TLS upgrade not supported on pipe connection",
	}
}

func (c *PipeConn) TLSConnectionState() *TLSConnectionState {
	return c.tlsState
}

// TimeoutReader wraps a reader with context-based timeout support.
// This is used when the underlying connection doesn't support deadlines.
type TimeoutReader struct {
	r       io.Reader
	timeout time.Duration
}

// NewTimeoutReader creates a reader that times out reads.
func NewTimeoutReader(r io.Reader, timeout time.Duration) *TimeoutReader {
	return &TimeoutReader{r: r, timeout: timeout}
}

// ReadWithContext reads with context cancellation support.
// This uses a goroutine, so it's less efficient than deadline-based reads.
func (r *TimeoutReader) ReadWithContext(ctx context.Context, p []byte) (int, error) {
	type result struct {
		n   int
		err error
	}

	ch := make(chan result, 1)
	go func() {
		n, err := r.r.Read(p)
		ch <- result{n, err}
	}()

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case res := <-ch:
		return res.n, res.err
	}
}

// BufferedConn wraps a Conn with buffered reading.
type BufferedConn struct {
	Conn
	reader *bufio.Reader
}

// NewBufferedConn creates a buffered connection.
func NewBufferedConn(conn Conn) *BufferedConn {
	return &BufferedConn{
		Conn:   conn,
		reader: bufio.NewReader(conn),
	}
}

// ReadLine reads a line with deadline support.
func (c *BufferedConn) ReadLine(timeout time.Duration) ([]byte, error) {
	if timeout > 0 {
		c.SetReadDeadline(time.Now().Add(timeout))
		defer c.SetReadDeadline(time.Time{}) // Clear deadline
	}
	return c.reader.ReadBytes('\n')
}

// Reader returns the buffered reader.
func (c *BufferedConn) Reader() *bufio.Reader {
	return c.reader
}

// ResetReader resets the buffered reader with a new underlying reader.
// This is needed after TLS upgrade.
func (c *BufferedConn) ResetReader() {
	c.reader = bufio.NewReader(c.Conn)
}
