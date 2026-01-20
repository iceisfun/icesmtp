package icesmtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"icesmtp/testdata"
)

// TestEngineTimeoutHandling tests that command timeouts work correctly.
func TestEngineTimeoutHandling(t *testing.T) {
	// Create a pipe connection with deadline support
	input := newTestPipeBuffer()
	output := &bytes.Buffer{}

	config := SessionConfig{
		ServerHostname: "test.example.com",
		Limits: SessionLimits{
			CommandTimeout: 100 * time.Millisecond, // Very short timeout for testing
			MaxErrors:      10,
		},
		Extensions: DefaultExtensions(),
		Mailbox:    &acceptAllMailbox{},
	}

	conn := WrapPipe(input, output)
	engine := NewEngineWithConn(conn, config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run the engine in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Run(ctx)
	}()

	// Wait for greeting
	time.Sleep(50 * time.Millisecond)

	// Don't send any commands - let it timeout
	// The engine should disconnect due to timeout

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected timeout error, got nil")
		}
		// Engine should have terminated due to timeout
		if !errors.Is(err, ErrDeadlineExceeded) && !errors.Is(err, context.DeadlineExceeded) && !isTimeoutError(err) {
			// Check output for timeout-related disconnect
			outputStr := output.String()
			if !strings.Contains(outputStr, "220") {
				t.Errorf("expected greeting, got: %s", outputStr)
			}
		}
	case <-time.After(2 * time.Second):
		t.Error("engine did not timeout as expected")
		engine.Close()
	}
}

// TestEngineSTARTTLS tests the full STARTTLS upgrade flow.
func TestEngineSTARTTLS(t *testing.T) {
	// Create pipe buffers for pre-TLS and post-TLS communication
	preInput := newTestPipeBuffer()
	preOutput := newTestPipeBuffer()

	// Create post-TLS buffers (simulated TLS connection)
	postInput := newTestPipeBuffer()
	postOutput := newTestPipeBuffer()

	// Load test TLS config
	tlsConfig, err := testdata.TestTLSConfig()
	if err != nil {
		t.Fatalf("failed to load test TLS config: %v", err)
	}

	// Create TLS provider
	provider := NewStaticTLSProvider(tlsConfig, TLSOptional)

	config := SessionConfig{
		ServerHostname: "test.example.com",
		Limits:         DefaultSessionLimits(),
		Extensions: ExtensionSet{
			STARTTLS:         true,
			ENHANCEDSTATUSCODES: true,
			HELP:             true,
		},
		TLSPolicy:   TLSOptional,
		TLSProvider: provider,
		Mailbox:     &acceptAllMailbox{},
	}

	// Create a mock TLS upgrader
	var upgradeCount int
	var upgradeMu sync.Mutex

	conn := WrapPipe(preInput, preOutput)
	conn.SetTLSUpgrader(func(cfg *tls.Config) (io.Reader, io.Writer, TLSConnectionState, error) {
		upgradeMu.Lock()
		upgradeCount++
		upgradeMu.Unlock()

		state := TLSConnectionState{
			Version:     tls.VersionTLS13,
			CipherSuite: tls.TLS_AES_128_GCM_SHA256,
			ServerName:  "test.example.com",
		}
		return postInput, postOutput, state, nil
	})

	engine := NewEngineWithConn(conn, config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run engine in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Run(ctx)
	}()

	// Helper to read response
	readResponse := func(buf *testPipeBuffer) string {
		var result bytes.Buffer
		for {
			line, err := buf.ReadLineWithTimeout(500 * time.Millisecond)
			if err != nil {
				break
			}
			result.WriteString(line)
			if len(line) >= 4 && line[3] == ' ' {
				break
			}
		}
		return result.String()
	}

	// Read greeting
	greeting := readResponse(preOutput)
	if !strings.HasPrefix(greeting, "220") {
		t.Fatalf("expected 220 greeting, got: %s", greeting)
	}

	// Send EHLO
	preInput.WriteString("EHLO client.example.com\r\n")
	ehloResp := readResponse(preOutput)
	if !strings.HasPrefix(ehloResp, "250") {
		t.Fatalf("expected 250 response to EHLO, got: %s", ehloResp)
	}
	if !strings.Contains(ehloResp, "STARTTLS") {
		t.Errorf("expected STARTTLS in EHLO response, got: %s", ehloResp)
	}

	// Send STARTTLS
	preInput.WriteString("STARTTLS\r\n")
	startTLSResp := readResponse(preOutput)
	if !strings.HasPrefix(startTLSResp, "220") {
		t.Fatalf("expected 220 response to STARTTLS, got: %s", startTLSResp)
	}

	// Wait for TLS upgrade to happen
	time.Sleep(100 * time.Millisecond)

	upgradeMu.Lock()
	if upgradeCount != 1 {
		t.Errorf("expected 1 TLS upgrade, got %d", upgradeCount)
	}
	upgradeMu.Unlock()

	// Now communicate over the "TLS" connection (post buffers)
	// Send new EHLO as required after STARTTLS
	postInput.WriteString("EHLO client.example.com\r\n")
	postEhloResp := readResponse(postOutput)
	if !strings.HasPrefix(postEhloResp, "250") {
		t.Fatalf("expected 250 response to post-TLS EHLO, got: %s", postEhloResp)
	}
	// STARTTLS should NOT be advertised after TLS is active
	if strings.Contains(postEhloResp, "STARTTLS") {
		t.Errorf("STARTTLS should not be advertised after TLS upgrade, got: %s", postEhloResp)
	}

	// Send QUIT
	postInput.WriteString("QUIT\r\n")
	quitResp := readResponse(postOutput)
	if !strings.HasPrefix(quitResp, "221") {
		t.Errorf("expected 221 response to QUIT, got: %s", quitResp)
	}

	// Wait for engine to finish
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("engine did not finish")
		engine.Close()
	}
}

// TestEngineSTARTTLSWhenAlreadyActive tests STARTTLS when TLS is already active.
func TestEngineSTARTTLSWhenAlreadyActive(t *testing.T) {
	input := newTestPipeBuffer()
	output := newTestPipeBuffer()

	provider := NewStaticTLSProvider(&tls.Config{}, TLSOptional)

	config := SessionConfig{
		ServerHostname: "test.example.com",
		Limits:         DefaultSessionLimits(),
		Extensions: ExtensionSet{
			STARTTLS: true,
		},
		TLSPolicy:   TLSOptional,
		TLSProvider: provider,
		Mailbox:     &acceptAllMailbox{},
	}

	// Manually set TLS as already active
	conn := WrapPipe(input, output)
	conn.SetTLSUpgrader(func(cfg *tls.Config) (io.Reader, io.Writer, TLSConnectionState, error) {
		return input, output, TLSConnectionState{Version: tls.VersionTLS13}, nil
	})

	engine := NewEngineWithConn(conn, config)

	// Manually set TLS active state (simulating implicit TLS)
	engine.state.TLSActive = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		engine.Run(ctx)
	}()

	// Read greeting
	readLine(output)

	// Send EHLO
	input.WriteString("EHLO client.example.com\r\n")
	readMultiLine(output)

	// Try STARTTLS when TLS is already active
	input.WriteString("STARTTLS\r\n")
	resp := readLine(output)
	if !strings.HasPrefix(resp, "503") {
		t.Errorf("expected 503 response when TLS already active, got: %s", resp)
	}

	input.WriteString("QUIT\r\n")
	engine.Close()
}

// TestEngineDATAErrorHandling tests that DATA errors are properly handled.
func TestEngineDATAErrorHandling(t *testing.T) {
	t.Run("storage error", func(t *testing.T) {
		input := newTestPipeBuffer()
		output := newTestPipeBuffer()

		config := SessionConfig{
			ServerHostname: "test.example.com",
			Limits:         DefaultSessionLimits(),
			Extensions:     DefaultExtensions(),
			Mailbox:        &acceptAllMailbox{},
			Storage:        &failingStorage{},
		}

		conn := WrapPipe(input, output)
		engine := NewEngineWithConn(conn, config)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		go func() {
			engine.Run(ctx)
		}()

		// Read greeting
		readLine(output)

		// Complete mail transaction
		input.WriteString("EHLO client.example.com\r\n")
		readMultiLine(output)

		input.WriteString("MAIL FROM:<sender@example.com>\r\n")
		readLine(output)

		input.WriteString("RCPT TO:<recipient@example.com>\r\n")
		readLine(output)

		input.WriteString("DATA\r\n")
		resp := readLine(output)
		if !strings.HasPrefix(resp, "354") {
			t.Fatalf("expected 354 response to DATA, got: %s", resp)
		}

		// Send message data
		input.WriteString("Subject: Test\r\n")
		input.WriteString("\r\n")
		input.WriteString("Test message.\r\n")
		input.WriteString(".\r\n")

		// Should get 451 error due to storage failure
		finalResp := readLine(output)
		if !strings.HasPrefix(finalResp, "451") {
			t.Errorf("expected 451 response due to storage error, got: %s", finalResp)
		}

		input.WriteString("QUIT\r\n")
		engine.Close()
	})
}

// TestEngineTLSRequired tests that TLS is enforced when required.
func TestEngineTLSRequired(t *testing.T) {
	input := newTestPipeBuffer()
	output := newTestPipeBuffer()

	config := SessionConfig{
		ServerHostname: "test.example.com",
		Limits:         DefaultSessionLimits(),
		Extensions: ExtensionSet{
			STARTTLS: true,
		},
		TLSPolicy:   TLSRequired,
		TLSProvider: NewStaticTLSProvider(&tls.Config{}, TLSRequired),
		Mailbox:     &acceptAllMailbox{},
	}

	conn := WrapPipe(input, output)
	engine := NewEngineWithConn(conn, config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		engine.Run(ctx)
	}()

	// Read greeting
	readLine(output)

	// Send EHLO
	input.WriteString("EHLO client.example.com\r\n")
	readMultiLine(output)

	// Try MAIL FROM without TLS - should fail
	input.WriteString("MAIL FROM:<sender@example.com>\r\n")
	resp := readLine(output)
	if !strings.HasPrefix(resp, "530") {
		t.Errorf("expected 530 response requiring STARTTLS, got: %s", resp)
	}

	input.WriteString("QUIT\r\n")
	engine.Close()
}

// Helper types and functions

// testPipeBuffer is a test buffer with deadline support.
type testPipeBuffer struct {
	mu           sync.Mutex
	cond         *sync.Cond
	buf          bytes.Buffer
	closed       bool
	readDeadline time.Time
}

func newTestPipeBuffer() *testPipeBuffer {
	p := &testPipeBuffer{}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *testPipeBuffer) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := p.buf.Write(data)
	p.cond.Broadcast()
	return n, err
}

func (p *testPipeBuffer) WriteString(s string) (int, error) {
	return p.Write([]byte(s))
}

func (p *testPipeBuffer) Read(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	deadline := p.readDeadline

	for p.buf.Len() == 0 && !p.closed {
		if !deadline.IsZero() && time.Now().After(deadline) {
			return 0, ErrDeadlineExceeded
		}

		if !deadline.IsZero() {
			timeout := time.Until(deadline)
			if timeout <= 0 {
				return 0, ErrDeadlineExceeded
			}
			go func() {
				time.Sleep(timeout)
				p.cond.Broadcast()
			}()
		}
		p.cond.Wait()

		if !deadline.IsZero() && time.Now().After(deadline) {
			return 0, ErrDeadlineExceeded
		}
	}

	if p.buf.Len() == 0 && p.closed {
		return 0, io.EOF
	}

	return p.buf.Read(data)
}

func (p *testPipeBuffer) SetReadDeadline(t time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.readDeadline = t
	p.cond.Broadcast()
	return nil
}

func (p *testPipeBuffer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.cond.Broadcast()
	return nil
}

func (p *testPipeBuffer) ReadLineWithTimeout(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var line bytes.Buffer

	for {
		if time.Now().After(deadline) {
			return line.String(), ErrDeadlineExceeded
		}

		p.mu.Lock()
		for p.buf.Len() == 0 && !p.closed {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				p.mu.Unlock()
				return line.String(), ErrDeadlineExceeded
			}
			go func() {
				time.Sleep(remaining)
				p.cond.Broadcast()
			}()
			p.cond.Wait()
		}

		if p.buf.Len() == 0 && p.closed {
			p.mu.Unlock()
			return line.String(), io.EOF
		}

		b, err := p.buf.ReadByte()
		p.mu.Unlock()

		if err != nil {
			return line.String(), err
		}

		line.WriteByte(b)

		if b == '\n' {
			return line.String(), nil
		}
	}
}

// acceptAllMailbox accepts all recipients.
type acceptAllMailbox struct{}

func (m *acceptAllMailbox) ValidateRecipient(ctx context.Context, recipient MailPath, session SessionInfo) RecipientResult {
	return RecipientResult{
		Status:   RecipientAccepted,
		Response: ResponseOK,
	}
}

// failingStorage always fails to store messages.
type failingStorage struct{}

func (s *failingStorage) Store(ctx context.Context, envelope Envelope) (StorageReceipt, error) {
	return StorageReceipt{}, errors.New("storage failure")
}

func (s *failingStorage) StoreStream(ctx context.Context, envelope Envelope, data io.Reader) (StorageReceipt, error) {
	return StorageReceipt{}, errors.New("storage failure")
}

// Helper functions

func readLine(buf *testPipeBuffer) string {
	line, _ := buf.ReadLineWithTimeout(500 * time.Millisecond)
	return line
}

func readMultiLine(buf *testPipeBuffer) string {
	var result bytes.Buffer
	for {
		line, err := buf.ReadLineWithTimeout(500 * time.Millisecond)
		if err != nil {
			break
		}
		result.WriteString(line)
		if len(line) >= 4 && line[3] == ' ' {
			break
		}
		// Also accept lines with just code (3 digits + CRLF)
		if len(line) == 5 && line[3] == '\r' && line[4] == '\n' {
			break
		}
	}
	return result.String()
}

// TestNewEngineWithNetConnSTARTTLS tests that NewEngine properly detects net.Conn
// and supports STARTTLS when used with real network connections.
func TestNewEngineWithNetConnSTARTTLS(t *testing.T) {
	// Create a real TCP listener for testing
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	tlsConfig, err := testdata.TestTLSConfig()
	if err != nil {
		t.Fatalf("failed to load test TLS config: %v", err)
	}

	provider := NewStaticTLSProvider(tlsConfig, TLSOptional)

	config := SessionConfig{
		ServerHostname: "test.example.com",
		Limits:         DefaultSessionLimits(),
		Extensions: ExtensionSet{
			STARTTLS: true,
		},
		TLSPolicy:   TLSOptional,
		TLSProvider: provider,
		Mailbox:     &acceptAllMailbox{},
	}

	// Server goroutine
	serverErrCh := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErrCh <- err
			return
		}
		defer conn.Close()

		// Use NewEngine with net.Conn (the documented pattern)
		// This should work for STARTTLS
		engine := NewEngine(conn, conn, config)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		serverErrCh <- engine.Run(ctx)
	}()

	// Client
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer clientConn.Close()

	clientConn.SetDeadline(time.Now().Add(5 * time.Second))

	// Read greeting
	buf := make([]byte, 1024)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}
	greeting := string(buf[:n])
	if !strings.HasPrefix(greeting, "220") {
		t.Fatalf("expected 220 greeting, got: %s", greeting)
	}

	// Send EHLO
	clientConn.Write([]byte("EHLO client.example.com\r\n"))
	n, _ = clientConn.Read(buf)
	ehloResp := string(buf[:n])
	if !strings.Contains(ehloResp, "STARTTLS") {
		t.Fatalf("expected STARTTLS in EHLO response, got: %s", ehloResp)
	}

	// Send STARTTLS
	clientConn.Write([]byte("STARTTLS\r\n"))
	n, _ = clientConn.Read(buf)
	startTLSResp := string(buf[:n])
	if !strings.HasPrefix(startTLSResp, "220") {
		t.Fatalf("expected 220 response to STARTTLS, got: %s", startTLSResp)
	}

	// Perform TLS handshake on client side
	tlsClientConn := tls.Client(clientConn, &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         "localhost",
	})
	if err := tlsClientConn.Handshake(); err != nil {
		t.Fatalf("TLS handshake failed: %v (this indicates NewEngine doesn't properly support STARTTLS with net.Conn)", err)
	}

	// Send EHLO over TLS
	tlsClientConn.Write([]byte("EHLO client.example.com\r\n"))
	n, _ = tlsClientConn.Read(buf)
	postTLSResp := string(buf[:n])
	if !strings.HasPrefix(postTLSResp, "250") {
		t.Errorf("expected 250 response to post-TLS EHLO, got: %s", postTLSResp)
	}

	// STARTTLS should not be advertised after upgrade
	if strings.Contains(postTLSResp, "STARTTLS") {
		t.Errorf("STARTTLS should not be advertised after TLS upgrade")
	}

	// Clean shutdown
	tlsClientConn.Write([]byte("QUIT\r\n"))
	tlsClientConn.Read(buf)
	tlsClientConn.Close()

	// Wait for server
	select {
	case err := <-serverErrCh:
		if err != nil && !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "use of closed") {
			t.Logf("server error (may be expected): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("server did not finish")
	}
}

// TestHELPDoesNotAdvertiseSTARTTLSWhenDisabled tests that HELP output
// does not include STARTTLS when TLS is disabled.
func TestHELPDoesNotAdvertiseSTARTTLSWhenDisabled(t *testing.T) {
	tests := []struct {
		name        string
		config      SessionConfig
		expectTLS   bool
	}{
		{
			name: "TLS disabled via policy",
			config: SessionConfig{
				ServerHostname: "test.example.com",
				Limits:         DefaultSessionLimits(),
				Extensions: ExtensionSet{
					STARTTLS: true, // Extension enabled but...
					HELP:     true,
				},
				TLSPolicy:   TLSDisabled, // ...policy is disabled
				TLSProvider: NewStaticTLSProvider(&tls.Config{}, TLSDisabled),
				Mailbox:     &acceptAllMailbox{},
			},
			expectTLS: false,
		},
		{
			name: "TLS disabled via extension",
			config: SessionConfig{
				ServerHostname: "test.example.com",
				Limits:         DefaultSessionLimits(),
				Extensions: ExtensionSet{
					STARTTLS: false, // Extension disabled
					HELP:     true,
				},
				TLSPolicy:   TLSOptional,
				TLSProvider: NewStaticTLSProvider(&tls.Config{}, TLSOptional),
				Mailbox:     &acceptAllMailbox{},
			},
			expectTLS: false,
		},
		{
			name: "TLS disabled via nil provider",
			config: SessionConfig{
				ServerHostname: "test.example.com",
				Limits:         DefaultSessionLimits(),
				Extensions: ExtensionSet{
					STARTTLS: true,
					HELP:     true,
				},
				TLSPolicy:   TLSOptional,
				TLSProvider: nil, // No provider
				Mailbox:     &acceptAllMailbox{},
			},
			expectTLS: false,
		},
		{
			name: "TLS enabled",
			config: SessionConfig{
				ServerHostname: "test.example.com",
				Limits:         DefaultSessionLimits(),
				Extensions: ExtensionSet{
					STARTTLS: true,
					HELP:     true,
				},
				TLSPolicy:   TLSOptional,
				TLSProvider: NewStaticTLSProvider(&tls.Config{}, TLSOptional),
				Mailbox:     &acceptAllMailbox{},
			},
			expectTLS: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := newTestPipeBuffer()
			output := newTestPipeBuffer()

			conn := WrapPipe(input, output)
			engine := NewEngineWithConn(conn, tt.config)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			go func() {
				engine.Run(ctx)
			}()

			// Read greeting
			readLine(output)

			// Send EHLO
			input.WriteString("EHLO client.example.com\r\n")
			readMultiLine(output)

			// Send HELP
			input.WriteString("HELP\r\n")
			helpResp := readMultiLine(output)

			hasSTARTTLS := strings.Contains(helpResp, "STARTTLS")

			if tt.expectTLS && !hasSTARTTLS {
				t.Errorf("expected STARTTLS in HELP output, got: %s", helpResp)
			}
			if !tt.expectTLS && hasSTARTTLS {
				t.Errorf("STARTTLS should not be in HELP output when TLS is disabled, got: %s", helpResp)
			}

			input.WriteString("QUIT\r\n")
			engine.Close()
		})
	}
}

// TestTLSHandshakeTimeout tests that TLS handshake has a timeout.
func TestTLSHandshakeTimeout(t *testing.T) {
	// Create a real TCP listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	tlsConfig, err := testdata.TestTLSConfig()
	if err != nil {
		t.Fatalf("failed to load test TLS config: %v", err)
	}

	provider := NewStaticTLSProvider(tlsConfig, TLSOptional)

	config := SessionConfig{
		ServerHostname: "test.example.com",
		Limits: SessionLimits{
			CommandTimeout: 500 * time.Millisecond, // Short timeout
			IdleTimeout:    500 * time.Millisecond,
		},
		Extensions: ExtensionSet{
			STARTTLS: true,
		},
		TLSPolicy:   TLSOptional,
		TLSProvider: provider,
		Mailbox:     &acceptAllMailbox{},
	}

	// Server goroutine
	serverDone := make(chan struct{})
	serverStarted := time.Now()
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		engine := NewEngineFromNetConn(conn, config)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		engine.Run(ctx)
	}()

	// Client - connects and sends STARTTLS but never completes handshake
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer clientConn.Close()

	clientConn.SetDeadline(time.Now().Add(10 * time.Second))

	// Read greeting
	buf := make([]byte, 1024)
	clientConn.Read(buf)

	// Send EHLO
	clientConn.Write([]byte("EHLO client.example.com\r\n"))
	clientConn.Read(buf)

	// Send STARTTLS
	clientConn.Write([]byte("STARTTLS\r\n"))
	n, _ := clientConn.Read(buf)
	if !strings.HasPrefix(string(buf[:n]), "220") {
		t.Fatalf("expected 220 response to STARTTLS")
	}

	// Now we DON'T do the TLS handshake - we just wait
	// The server should timeout and close the connection

	select {
	case <-serverDone:
		elapsed := time.Since(serverStarted)
		// Should timeout within ~1 second (500ms timeout + some margin)
		if elapsed > 3*time.Second {
			t.Errorf("TLS handshake took too long to timeout: %v (expected ~500ms)", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not timeout on stalled TLS handshake - DoS vulnerability!")
	}
}
