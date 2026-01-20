package icesmtp

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"
)

// Envelope builder errors.
var (
	// ErrEnvelopeFinalized indicates the envelope has already been finalized.
	ErrEnvelopeFinalized = errors.New("envelope already finalized")

	// ErrNoMailFrom indicates MAIL FROM has not been set.
	ErrNoMailFrom = errors.New("no MAIL FROM set")

	// ErrNoRecipients indicates no recipients have been added.
	ErrNoRecipients = errors.New("no recipients")

	// ErrNoData indicates no message data has been written.
	ErrNoData = errors.New("no message data")

	// ErrDataWriterOpen indicates the data writer is still open.
	ErrDataWriterOpen = errors.New("data writer still open")
)

// StandardEnvelopeBuilder is the default EnvelopeBuilder implementation.
type StandardEnvelopeBuilder struct {
	mu sync.Mutex

	id          EnvelopeID
	mailFrom    *MailPath
	recipients  []MailPath
	esmtpParams ESMTPParams
	receivedAt  time.Time
	data        bytes.Buffer
	dataWriter  *envelopeDataWriter
	finalized   bool
	metadata    EnvelopeMetadata
}

// NewStandardEnvelopeBuilder creates a new envelope builder.
func NewStandardEnvelopeBuilder(metadata EnvelopeMetadata) *StandardEnvelopeBuilder {
	return &StandardEnvelopeBuilder{
		id:         generateEnvelopeID(),
		receivedAt: time.Now(),
		metadata:   metadata,
		recipients: make([]MailPath, 0),
	}
}

// generateEnvelopeID creates a unique envelope identifier.
func generateEnvelopeID() EnvelopeID {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// SetMailFrom sets the reverse-path and ESMTP parameters.
func (b *StandardEnvelopeBuilder) SetMailFrom(path MailPath, params ESMTPParams) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finalized {
		return ErrEnvelopeFinalized
	}

	b.mailFrom = &path
	b.esmtpParams = params
	b.receivedAt = time.Now()

	return nil
}

// AddRecipient adds a forward-path to the envelope.
func (b *StandardEnvelopeBuilder) AddRecipient(path MailPath) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finalized {
		return ErrEnvelopeFinalized
	}

	b.recipients = append(b.recipients, path)
	return nil
}

// DataWriter returns an io.WriteCloser for writing message data.
func (b *StandardEnvelopeBuilder) DataWriter() (io.WriteCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finalized {
		return nil, ErrEnvelopeFinalized
	}

	if b.dataWriter != nil {
		return nil, errors.New("data writer already open")
	}

	b.dataWriter = &envelopeDataWriter{
		builder: b,
		buf:     &b.data,
	}

	return b.dataWriter, nil
}

// Finalize marks the envelope as complete and returns it.
func (b *StandardEnvelopeBuilder) Finalize() (Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.finalized {
		return nil, ErrEnvelopeFinalized
	}

	if b.mailFrom == nil {
		return nil, ErrNoMailFrom
	}

	if len(b.recipients) == 0 {
		return nil, ErrNoRecipients
	}

	if b.dataWriter != nil && !b.dataWriter.closed {
		return nil, ErrDataWriterOpen
	}

	b.finalized = true

	return &StandardEnvelope{
		id:          b.id,
		mailFrom:    *b.mailFrom,
		recipients:  b.recipients,
		esmtpParams: b.esmtpParams,
		receivedAt:  b.receivedAt,
		data:        b.data.Bytes(),
		finalized:   true,
		metadata:    b.metadata,
	}, nil
}

// Reset clears the envelope builder for reuse.
func (b *StandardEnvelopeBuilder) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.id = generateEnvelopeID()
	b.mailFrom = nil
	b.recipients = make([]MailPath, 0)
	b.esmtpParams = nil
	b.receivedAt = time.Time{}
	b.data.Reset()
	b.dataWriter = nil
	b.finalized = false
}

// Build returns the current envelope state without finalizing.
func (b *StandardEnvelopeBuilder) Build() Envelope {
	b.mu.Lock()
	defer b.mu.Unlock()

	mailFrom := MailPath{}
	if b.mailFrom != nil {
		mailFrom = *b.mailFrom
	}

	recipients := make([]MailPath, len(b.recipients))
	copy(recipients, b.recipients)

	return &StandardEnvelope{
		id:          b.id,
		mailFrom:    mailFrom,
		recipients:  recipients,
		esmtpParams: b.esmtpParams,
		receivedAt:  b.receivedAt,
		data:        b.data.Bytes(),
		finalized:   b.finalized,
		metadata:    b.metadata,
	}
}

// envelopeDataWriter wraps the data buffer with Close semantics.
type envelopeDataWriter struct {
	builder *StandardEnvelopeBuilder
	buf     *bytes.Buffer
	closed  bool
}

func (w *envelopeDataWriter) Write(p []byte) (n int, err error) {
	if w.closed {
		return 0, errors.New("writer closed")
	}
	return w.buf.Write(p)
}

func (w *envelopeDataWriter) Close() error {
	w.closed = true
	return nil
}

// StandardEnvelopeFactory is the default EnvelopeFactory implementation.
type StandardEnvelopeFactory struct{}

// NewBuilder creates a new envelope builder.
func (f StandardEnvelopeFactory) NewBuilder(metadata EnvelopeMetadata) EnvelopeBuilder {
	return NewStandardEnvelopeBuilder(metadata)
}
