// Package harness provides a test harness for SMTP sessions.
// It allows testing SMTP conversations without network sockets.
package harness

import (
	"bytes"
	"context"
	crypto_tls "crypto/tls"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"icesmtp"
	"icesmtp/mem"
)

// Harness provides a test environment for SMTP sessions.
// It operates over io.Reader/io.Writer pairs without network dependencies.
type Harness struct {
	// Config is the session configuration.
	Config icesmtp.SessionConfig

	// Engine is the SMTP engine under test.
	Engine *icesmtp.Engine

	// Storage provides access to stored messages.
	Storage *mem.Storage

	// Mailbox provides access to the mailbox registry.
	Mailbox *mem.Mailbox

	// Input is the client input buffer.
	Input *PipeBuffer

	// Output is the server output buffer.
	Output *PipeBuffer

	// Transcript records the full SMTP conversation.
	Transcript *Transcript

	// Errors collects any errors that occurred.
	Errors []error

	mu sync.Mutex
}

// HarnessOption configures a Harness.
type HarnessOption func(*Harness)

// WithServerHostname sets the server hostname.
func WithServerHostname(hostname icesmtp.Hostname) HarnessOption {
	return func(h *Harness) {
		h.Config.ServerHostname = hostname
	}
}

// WithMailbox sets the mailbox implementation.
func WithMailbox(mailbox icesmtp.Mailbox) HarnessOption {
	return func(h *Harness) {
		h.Config.Mailbox = mailbox
	}
}

// WithStorage sets the storage implementation.
func WithStorage(storage icesmtp.Storage) HarnessOption {
	return func(h *Harness) {
		h.Config.Storage = storage
	}
}

// WithLimits sets session limits.
func WithLimits(limits icesmtp.SessionLimits) HarnessOption {
	return func(h *Harness) {
		h.Config.Limits = limits
	}
}

// WithExtensions sets enabled extensions.
func WithExtensions(ext icesmtp.ExtensionSet) HarnessOption {
	return func(h *Harness) {
		h.Config.Extensions = ext
	}
}

// WithTLSProvider sets the TLS provider.
func WithTLSProvider(provider icesmtp.TLSProvider) HarnessOption {
	return func(h *Harness) {
		h.Config.TLSProvider = provider
	}
}

// WithTLSPolicy sets the TLS policy.
func WithTLSPolicy(policy icesmtp.TLSPolicy) HarnessOption {
	return func(h *Harness) {
		h.Config.TLSPolicy = policy
	}
}

// NewHarness creates a new test harness with default configuration.
func NewHarness(opts ...HarnessOption) *Harness {
	storage := mem.NewStorage()
	mailbox := mem.NewMailbox()

	h := &Harness{
		Config: icesmtp.SessionConfig{
			ServerHostname: "test.example.com",
			Limits:         icesmtp.DefaultSessionLimits(),
			Extensions:     icesmtp.DefaultExtensions(),
			Mailbox:        mailbox,
			Storage:        storage,
		},
		Storage:    storage,
		Mailbox:    mailbox,
		Input:      NewPipeBuffer(),
		Output:     NewPipeBuffer(),
		Transcript: NewTranscript(),
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
}

// Start starts the SMTP engine.
// Call this before sending commands.
func (h *Harness) Start(ctx context.Context) {
	// Create a PipeConn with deadline support
	conn := icesmtp.WrapPipe(h.Input, h.Output)
	h.Engine = icesmtp.NewEngineWithConn(conn, h.Config)

	go func() {
		if err := h.Engine.Run(ctx); err != nil && err != context.Canceled {
			h.mu.Lock()
			h.Errors = append(h.Errors, err)
			h.mu.Unlock()
		}
	}()
}

// StartWithTLS starts the SMTP engine with TLS upgrade support for testing.
// The tlsUpgrader function is called when STARTTLS upgrade is attempted.
func (h *Harness) StartWithTLS(ctx context.Context, tlsUpgrader func(*crypto_tls.Config) (io.Reader, io.Writer, icesmtp.TLSConnectionState, error)) {
	conn := icesmtp.WrapPipe(h.Input, h.Output)
	conn.SetTLSUpgrader(tlsUpgrader)
	h.Engine = icesmtp.NewEngineWithConn(conn, h.Config)

	go func() {
		if err := h.Engine.Run(ctx); err != nil && err != context.Canceled {
			h.mu.Lock()
			h.Errors = append(h.Errors, err)
			h.mu.Unlock()
		}
	}()
}

// Send sends a command line to the server.
// The CRLF terminator is added automatically.
func (h *Harness) Send(line string) {
	data := line + "\r\n"
	h.Input.Write([]byte(data))
	h.Transcript.RecordClient(data)
}

// SendRaw sends raw bytes to the server.
func (h *Harness) SendRaw(data []byte) {
	h.Input.Write(data)
	h.Transcript.RecordClient(string(data))
}

// Expect reads a response and checks if it starts with the expected code.
// Returns the full response line(s).
func (h *Harness) Expect(code icesmtp.ReplyCode) ([]string, error) {
	return h.ExpectWithTimeout(code, 5*time.Second)
}

// ExpectWithTimeout reads a response with a timeout.
func (h *Harness) ExpectWithTimeout(code icesmtp.ReplyCode, timeout time.Duration) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	lines, err := h.readResponse(ctx)
	if err != nil {
		return nil, err
	}

	// Check the code of the last line
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	lastLine := lines[len(lines)-1]
	if len(lastLine) < 3 {
		return nil, fmt.Errorf("response too short: %s", lastLine)
	}

	var gotCode int
	fmt.Sscanf(lastLine[:3], "%d", &gotCode)

	if icesmtp.ReplyCode(gotCode) != code {
		return lines, fmt.Errorf("expected %d, got %d: %s", code, gotCode, lastLine)
	}

	return lines, nil
}

// ExpectAny reads a response and returns it without checking the code.
func (h *Harness) ExpectAny() ([]string, error) {
	return h.ExpectAnyWithTimeout(5 * time.Second)
}

// ExpectAnyWithTimeout reads a response with a timeout.
func (h *Harness) ExpectAnyWithTimeout(timeout time.Duration) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return h.readResponse(ctx)
}

// readResponse reads a complete SMTP response (handles multi-line).
func (h *Harness) readResponse(ctx context.Context) ([]string, error) {
	var lines []string

	for {
		select {
		case <-ctx.Done():
			return lines, ctx.Err()
		default:
		}

		line, err := h.Output.ReadLine(ctx)
		if err != nil {
			return lines, err
		}

		h.Transcript.RecordServer(line)
		lines = append(lines, line)

		// Check if this is the last line (code followed by space, not hyphen)
		if len(line) >= 4 && line[3] == ' ' {
			break
		}
		// Also accept lines with just code and CRLF
		if len(line) <= 5 && !strings.Contains(line, "-") {
			break
		}
	}

	return lines, nil
}

// SendData sends message data terminated with <CRLF>.<CRLF>.
func (h *Harness) SendData(data string) {
	// Ensure proper line endings
	lines := strings.Split(data, "\n")
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		// Dot-stuff if needed
		if strings.HasPrefix(line, ".") {
			line = "." + line
		}
		if i < len(lines)-1 {
			h.Send(line)
		} else if line != "" {
			h.Send(line)
		}
	}
	// Send terminator
	h.Send(".")
}

// RunConversation runs a scripted SMTP conversation.
func (h *Harness) RunConversation(ctx context.Context, script []ConversationStep) error {
	h.Start(ctx)

	for _, step := range script {
		if step.Send != "" {
			h.Send(step.Send)
		}
		if step.SendRaw != nil {
			h.SendRaw(step.SendRaw)
		}
		if step.Expect != 0 {
			_, err := h.Expect(step.Expect)
			if err != nil {
				return fmt.Errorf("step %q: %w", step.Description, err)
			}
		}
		if step.ExpectAny {
			_, err := h.ExpectAny()
			if err != nil {
				return fmt.Errorf("step %q: %w", step.Description, err)
			}
		}
		if step.Delay > 0 {
			time.Sleep(step.Delay)
		}
	}

	return nil
}

// Close closes the harness.
func (h *Harness) Close() {
	h.Input.Close()
	h.Output.Close()
	if h.Engine != nil {
		h.Engine.Close()
	}
}

// Messages returns all stored messages.
func (h *Harness) Messages() []*mem.StoredMessage {
	return h.Storage.List()
}

// MessageCount returns the number of stored messages.
func (h *Harness) MessageCount() int {
	return h.Storage.Count()
}

// ConversationStep represents a step in a scripted conversation.
type ConversationStep struct {
	// Description describes this step (for error messages).
	Description string

	// Send is the command to send (CRLF added automatically).
	Send string

	// SendRaw is raw bytes to send (no CRLF added).
	SendRaw []byte

	// Expect is the expected reply code.
	Expect icesmtp.ReplyCode

	// ExpectAny expects any response without checking the code.
	ExpectAny bool

	// Delay pauses before the next step.
	Delay time.Duration
}

// PipeBuffer is a thread-safe buffer for simulating I/O.
// It supports deadline-based reads for timeout testing.
type PipeBuffer struct {
	mu           sync.Mutex
	cond         *sync.Cond
	buf          bytes.Buffer
	closed       bool
	readDeadline time.Time
}

// NewPipeBuffer creates a new pipe buffer.
func NewPipeBuffer() *PipeBuffer {
	p := &PipeBuffer{}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// Write writes data to the buffer.
func (p *PipeBuffer) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return 0, io.ErrClosedPipe
	}

	n, err := p.buf.Write(data)
	p.cond.Broadcast()
	return n, err
}

// Read reads data from the buffer with deadline support.
func (p *PipeBuffer) Read(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check deadline
	deadline := p.readDeadline

	for p.buf.Len() == 0 && !p.closed {
		// Check if deadline has passed
		if !deadline.IsZero() && time.Now().After(deadline) {
			return 0, icesmtp.ErrDeadlineExceeded
		}

		// Wait with timeout if deadline is set
		if !deadline.IsZero() {
			timeout := time.Until(deadline)
			if timeout <= 0 {
				return 0, icesmtp.ErrDeadlineExceeded
			}
			// Use a timed wait
			go func() {
				time.Sleep(timeout)
				p.cond.Broadcast()
			}()
		}
		p.cond.Wait()

		// Re-check deadline after wake
		if !deadline.IsZero() && time.Now().After(deadline) {
			return 0, icesmtp.ErrDeadlineExceeded
		}
	}

	if p.buf.Len() == 0 && p.closed {
		return 0, io.EOF
	}

	return p.buf.Read(data)
}

// SetReadDeadline sets the deadline for future Read calls.
func (p *PipeBuffer) SetReadDeadline(t time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.readDeadline = t
	p.cond.Broadcast() // Wake any waiters to re-check deadline
	return nil
}

// ReadLine reads a line from the buffer.
func (p *PipeBuffer) ReadLine(ctx context.Context) (string, error) {
	var line bytes.Buffer

	for {
		select {
		case <-ctx.Done():
			return line.String(), ctx.Err()
		default:
		}

		p.mu.Lock()
		for p.buf.Len() == 0 && !p.closed {
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

// Close closes the buffer.
func (p *PipeBuffer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	p.cond.Broadcast()
	return nil
}

// Transcript records an SMTP conversation.
type Transcript struct {
	mu      sync.Mutex
	entries []TranscriptEntry
}

// TranscriptEntry is a single entry in the transcript.
type TranscriptEntry struct {
	Time      time.Time
	Direction TranscriptDirection
	Data      string
}

// TranscriptDirection indicates client or server.
type TranscriptDirection int

const (
	DirectionClient TranscriptDirection = iota
	DirectionServer
)

// NewTranscript creates a new transcript.
func NewTranscript() *Transcript {
	return &Transcript{}
}

// RecordClient records data from the client.
func (t *Transcript) RecordClient(data string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.entries = append(t.entries, TranscriptEntry{
		Time:      time.Now(),
		Direction: DirectionClient,
		Data:      data,
	})
}

// RecordServer records data from the server.
func (t *Transcript) RecordServer(data string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.entries = append(t.entries, TranscriptEntry{
		Time:      time.Now(),
		Direction: DirectionServer,
		Data:      data,
	})
}

// String returns the transcript as a string.
func (t *Transcript) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var b strings.Builder
	for _, e := range t.entries {
		if e.Direction == DirectionClient {
			b.WriteString("C: ")
		} else {
			b.WriteString("S: ")
		}
		b.WriteString(strings.TrimSuffix(e.Data, "\r\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// Entries returns all transcript entries.
func (t *Transcript) Entries() []TranscriptEntry {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := make([]TranscriptEntry, len(t.entries))
	copy(result, t.entries)
	return result
}
