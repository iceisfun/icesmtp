package icesmtp

import (
	"io"
	"time"
)

// Envelope represents a single SMTP mail transaction.
// An envelope is created when MAIL FROM is accepted and finalized
// when DATA completes successfully.
type Envelope interface {
	// ID returns a unique identifier for this envelope.
	ID() EnvelopeID

	// MailFrom returns the reverse-path (sender) of this envelope.
	MailFrom() MailPath

	// Recipients returns all accepted forward-paths (recipients).
	Recipients() []MailPath

	// RecipientCount returns the number of accepted recipients.
	RecipientCount() RecipientCount

	// ESMTPParams returns the ESMTP parameters from the MAIL command.
	ESMTPParams() ESMTPParams

	// DeclaredSize returns the SIZE parameter value if provided, or 0.
	DeclaredSize() MessageSize

	// ReceivedAt returns the time the envelope was created (MAIL FROM accepted).
	ReceivedAt() time.Time

	// Data returns the message data if the envelope is finalized.
	// Returns nil if the envelope has not yet received DATA.
	Data() MessageData

	// DataSize returns the actual size of the message data in bytes.
	// Returns 0 if no data has been received.
	DataSize() MessageSize

	// IsFinalized returns true if the envelope has been finalized
	// (DATA completed successfully).
	IsFinalized() bool

	// Metadata returns session metadata associated with this envelope.
	Metadata() EnvelopeMetadata
}

// EnvelopeID is a unique identifier for an envelope.
type EnvelopeID = string

// RecipientCount is the number of recipients in an envelope.
type RecipientCount = int

// MessageSize is the size of a message in bytes.
type MessageSize = int64

// MessageData is the raw message content (headers + body).
type MessageData = []byte

// EnvelopeMetadata contains session information associated with an envelope.
type EnvelopeMetadata struct {
	// SessionID is the identifier of the session that created this envelope.
	SessionID SessionID

	// ClientHostname is the hostname provided in HELO/EHLO.
	ClientHostname Hostname

	// ClientIP is the IP address of the client.
	ClientIP IPAddress

	// ServerHostname is this server's hostname.
	ServerHostname Hostname

	// TLSActive indicates whether TLS was active during receipt.
	TLSActive bool

	// TLSVersion is the negotiated TLS version, if TLS is active.
	TLSVersion TLSVersion

	// TLSCipherSuite is the negotiated cipher suite, if TLS is active.
	TLSCipherSuite TLSCipherSuite

	// AuthenticatedUser is the username if authentication succeeded.
	AuthenticatedUser Username
}

// SessionID is a unique identifier for an SMTP session.
type SessionID = string

// IPAddress represents an IP address as a string.
type IPAddress = string

// TLSVersion represents a TLS version string.
type TLSVersion = string

// TLSCipherSuite represents a TLS cipher suite name.
type TLSCipherSuite = string

// Username represents an authenticated username.
type Username = string

// EnvelopeBuilder provides methods for constructing an envelope during a transaction.
type EnvelopeBuilder interface {
	// SetMailFrom sets the reverse-path and ESMTP parameters.
	SetMailFrom(path MailPath, params ESMTPParams) error

	// AddRecipient adds a forward-path to the envelope.
	AddRecipient(path MailPath) error

	// DataWriter returns an io.WriteCloser for writing message data.
	// The caller must call Close() when data transfer is complete.
	// This allows streaming large messages without buffering.
	DataWriter() (io.WriteCloser, error)

	// Finalize marks the envelope as complete and ready for storage.
	// After finalization, no further modifications are allowed.
	Finalize() (Envelope, error)

	// Reset clears the envelope builder for reuse.
	Reset()

	// Build returns the current envelope state without finalizing.
	// Useful for inspection during the transaction.
	Build() Envelope
}

// EnvelopeFactory creates new envelope builders.
type EnvelopeFactory interface {
	// NewBuilder creates a new envelope builder with the given metadata.
	NewBuilder(metadata EnvelopeMetadata) EnvelopeBuilder
}

// StandardEnvelope is the default implementation of Envelope.
type StandardEnvelope struct {
	id         EnvelopeID
	mailFrom   MailPath
	recipients []MailPath
	esmtpParams ESMTPParams
	receivedAt time.Time
	data       MessageData
	finalized  bool
	metadata   EnvelopeMetadata
}

// ID returns the envelope identifier.
func (e *StandardEnvelope) ID() EnvelopeID {
	return e.id
}

// MailFrom returns the reverse-path.
func (e *StandardEnvelope) MailFrom() MailPath {
	return e.mailFrom
}

// Recipients returns all forward-paths.
func (e *StandardEnvelope) Recipients() []MailPath {
	result := make([]MailPath, len(e.recipients))
	copy(result, e.recipients)
	return result
}

// RecipientCount returns the number of recipients.
func (e *StandardEnvelope) RecipientCount() RecipientCount {
	return len(e.recipients)
}

// ESMTPParams returns the ESMTP parameters.
func (e *StandardEnvelope) ESMTPParams() ESMTPParams {
	return e.esmtpParams
}

// DeclaredSize returns the SIZE parameter value.
func (e *StandardEnvelope) DeclaredSize() MessageSize {
	if e.esmtpParams == nil {
		return 0
	}
	// SIZE parameter handling would parse the value here
	return 0
}

// ReceivedAt returns the creation time.
func (e *StandardEnvelope) ReceivedAt() time.Time {
	return e.receivedAt
}

// Data returns the message data.
func (e *StandardEnvelope) Data() MessageData {
	return e.data
}

// DataSize returns the size of the message data.
func (e *StandardEnvelope) DataSize() MessageSize {
	return MessageSize(len(e.data))
}

// IsFinalized returns whether the envelope is finalized.
func (e *StandardEnvelope) IsFinalized() bool {
	return e.finalized
}

// Metadata returns the envelope metadata.
func (e *StandardEnvelope) Metadata() EnvelopeMetadata {
	return e.metadata
}

// RecipientStatus represents the acceptance status of a recipient.
type RecipientStatus int

const (
	// RecipientPending indicates the recipient has not been validated.
	RecipientPending RecipientStatus = iota

	// RecipientAccepted indicates the recipient was accepted.
	RecipientAccepted

	// RecipientRejected indicates the recipient was rejected.
	RecipientRejected

	// RecipientDeferred indicates the recipient check was deferred.
	RecipientDeferred
)

// RecipientResult contains the result of validating a recipient.
type RecipientResult struct {
	// Path is the recipient address.
	Path MailPath

	// Status is the validation status.
	Status RecipientStatus

	// Response is the SMTP response to send.
	Response Response
}
