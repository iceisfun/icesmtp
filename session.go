package icesmtp

import (
	"context"
	"io"
	"time"
)

// Session represents an active SMTP session.
// A session is created for each client connection and handles the
// complete SMTP conversation lifecycle.
type Session interface {
	SessionInfo

	// Run executes the session until completion.
	// The context controls timeouts and cancellation.
	// Returns when the session terminates normally, errors, or is cancelled.
	Run(ctx context.Context) error

	// Close terminates the session immediately.
	// This may be called from another goroutine.
	Close() error
}

// SessionHandler processes SMTP commands within a session.
// This is the main protocol processing interface.
type SessionHandler interface {
	// HandleCommand processes a parsed command and returns the result.
	HandleCommand(ctx context.Context, cmd Command, session Session) CommandResult

	// HandleData processes the DATA phase.
	// The reader provides the raw message data.
	// Returns when data transfer completes or an error occurs.
	HandleData(ctx context.Context, reader io.Reader, session Session) (Response, error)
}

// SessionConfig contains configuration for a session.
type SessionConfig struct {
	// ServerHostname is the hostname to use in greetings and Received headers.
	ServerHostname Hostname

	// Limits contains resource limits for this session.
	Limits SessionLimits

	// TLSPolicy specifies the TLS policy for this session.
	TLSPolicy TLSPolicy

	// TLSProvider provides TLS configuration if TLS is enabled.
	TLSProvider TLSProvider

	// Mailbox handles recipient validation.
	Mailbox Mailbox

	// SenderPolicy handles sender validation.
	// If nil, all senders are accepted.
	SenderPolicy SenderPolicy

	// Storage handles message persistence.
	Storage Storage

	// EnvelopeFactory creates envelope builders.
	// If nil, a default factory is used.
	EnvelopeFactory EnvelopeFactory

	// Extensions specifies which SMTP extensions are enabled.
	Extensions ExtensionSet

	// Hooks provides optional session lifecycle callbacks.
	Hooks SessionHooks

	// Logger receives session log events.
	// If nil, logging is disabled.
	Logger Logger
}

// SessionLimits contains resource limits for DoS protection.
type SessionLimits struct {
	// MaxMessageSize is the maximum message size in bytes (0 = unlimited).
	MaxMessageSize MessageSize

	// MaxRecipients is the maximum recipients per message (0 = unlimited).
	MaxRecipients RecipientCount

	// MaxCommandLength is the maximum length of a command line in bytes.
	// RFC 5321 specifies 512 bytes; including extensions, 1024 is common.
	MaxCommandLength CommandLength

	// MaxLineLength is the maximum length of a data line in bytes.
	// RFC 5321 specifies 998 bytes for message lines.
	MaxLineLength LineLength

	// CommandTimeout is the timeout for reading a command.
	CommandTimeout Duration

	// DataTimeout is the timeout for receiving message data.
	DataTimeout Duration

	// IdleTimeout is the timeout for an idle connection.
	IdleTimeout Duration

	// MaxErrors is the maximum consecutive errors before disconnection.
	MaxErrors ErrorCount

	// MaxTransactions is the maximum mail transactions per session (0 = unlimited).
	MaxTransactions TransactionCount

	// MaxAuthAttempts is the maximum authentication attempts per session.
	MaxAuthAttempts AuthAttemptCount
}

// CommandLength is the length of a command line in bytes.
type CommandLength = int

// LineLength is the length of a line in bytes.
type LineLength = int

// Duration is a time duration.
type Duration = time.Duration

// ErrorCount is a count of errors.
type ErrorCount = int

// TransactionCount is a count of mail transactions.
type TransactionCount = int

// AuthAttemptCount is a count of authentication attempts.
type AuthAttemptCount = int

// DefaultSessionLimits returns secure default limits.
func DefaultSessionLimits() SessionLimits {
	return SessionLimits{
		MaxMessageSize:   25 * 1024 * 1024, // 25 MB
		MaxRecipients:    100,
		MaxCommandLength: 512,
		MaxLineLength:    998,
		CommandTimeout:   5 * time.Minute,
		DataTimeout:      10 * time.Minute,
		IdleTimeout:      5 * time.Minute,
		MaxErrors:        10,
		MaxTransactions:  100,
		MaxAuthAttempts:  3,
	}
}

// ExtensionSet specifies which SMTP extensions are enabled.
type ExtensionSet struct {
	// STARTTLS enables the STARTTLS extension (RFC 3207).
	STARTTLS bool

	// SIZE enables the SIZE extension (RFC 1870).
	SIZE bool

	// 8BITMIME enables the 8BITMIME extension (RFC 6152).
	EightBitMIME bool

	// PIPELINING enables the PIPELINING extension (RFC 2920).
	PIPELINING bool

	// ENHANCEDSTATUSCODES enables enhanced status codes (RFC 2034).
	ENHANCEDSTATUSCODES bool

	// SMTPUTF8 enables internationalized email (RFC 6531).
	SMTPUTF8 bool

	// AUTH enables SMTP authentication (RFC 4954).
	AUTH bool

	// VRFY enables the VRFY command.
	VRFY bool

	// EXPN enables the EXPN command.
	EXPN bool

	// HELP enables the HELP command.
	HELP bool
}

// DefaultExtensions returns a default extension set.
func DefaultExtensions() ExtensionSet {
	return ExtensionSet{
		STARTTLS:            true,
		SIZE:                true,
		EightBitMIME:        true,
		PIPELINING:          true,
		ENHANCEDSTATUSCODES: true,
		SMTPUTF8:            false,
		AUTH:                false,
		VRFY:                false,
		EXPN:                false,
		HELP:                true,
	}
}

// SessionHooks provides callbacks for session lifecycle events.
type SessionHooks interface {
	// OnConnect is called when a new session starts.
	OnConnect(ctx context.Context, session SessionInfo)

	// OnDisconnect is called when a session ends.
	OnDisconnect(ctx context.Context, session SessionInfo, reason DisconnectReason)

	// OnCommand is called before processing each command.
	// Returning an error aborts command processing and sends the error response.
	OnCommand(ctx context.Context, cmd Command, session SessionInfo) error

	// OnMailFrom is called when MAIL FROM is accepted.
	OnMailFrom(ctx context.Context, sender MailPath, session SessionInfo)

	// OnRcptTo is called when RCPT TO is accepted.
	OnRcptTo(ctx context.Context, recipient MailPath, session SessionInfo)

	// OnDataStart is called when DATA command is accepted.
	OnDataStart(ctx context.Context, session SessionInfo)

	// OnDataEnd is called when message data is received.
	OnDataEnd(ctx context.Context, envelope Envelope, session SessionInfo)

	// OnTLSUpgrade is called after successful STARTTLS.
	OnTLSUpgrade(ctx context.Context, state TLSConnectionState, session SessionInfo)

	// OnError is called when an error occurs.
	OnError(ctx context.Context, err error, session SessionInfo)
}

// DisconnectReason indicates why a session was disconnected.
type DisconnectReason int

const (
	// DisconnectNormal indicates the client sent QUIT.
	DisconnectNormal DisconnectReason = iota

	// DisconnectTimeout indicates the session timed out.
	DisconnectTimeout

	// DisconnectError indicates an I/O or protocol error.
	DisconnectError

	// DisconnectPolicyViolation indicates a policy was violated.
	DisconnectPolicyViolation

	// DisconnectResourceLimit indicates a resource limit was exceeded.
	DisconnectResourceLimit

	// DisconnectTLSFailure indicates TLS handshake failed.
	DisconnectTLSFailure

	// DisconnectServerShutdown indicates the server is shutting down.
	DisconnectServerShutdown
)

// String returns a human-readable disconnect reason.
func (d DisconnectReason) String() string {
	switch d {
	case DisconnectNormal:
		return "Normal"
	case DisconnectTimeout:
		return "Timeout"
	case DisconnectError:
		return "Error"
	case DisconnectPolicyViolation:
		return "PolicyViolation"
	case DisconnectResourceLimit:
		return "ResourceLimit"
	case DisconnectTLSFailure:
		return "TLSFailure"
	case DisconnectServerShutdown:
		return "ServerShutdown"
	default:
		return "Unknown"
	}
}

// NullSessionHooks is a no-op implementation of SessionHooks.
type NullSessionHooks struct{}

func (NullSessionHooks) OnConnect(_ context.Context, _ SessionInfo)               {}
func (NullSessionHooks) OnDisconnect(_ context.Context, _ SessionInfo, _ DisconnectReason) {}
func (NullSessionHooks) OnCommand(_ context.Context, _ Command, _ SessionInfo) error { return nil }
func (NullSessionHooks) OnMailFrom(_ context.Context, _ MailPath, _ SessionInfo)  {}
func (NullSessionHooks) OnRcptTo(_ context.Context, _ MailPath, _ SessionInfo)    {}
func (NullSessionHooks) OnDataStart(_ context.Context, _ SessionInfo)             {}
func (NullSessionHooks) OnDataEnd(_ context.Context, _ Envelope, _ SessionInfo)   {}
func (NullSessionHooks) OnTLSUpgrade(_ context.Context, _ TLSConnectionState, _ SessionInfo) {}
func (NullSessionHooks) OnError(_ context.Context, _ error, _ SessionInfo)        {}

// SessionStats contains statistics for a session.
type SessionStats struct {
	// StartTime is when the session started.
	StartTime time.Time

	// EndTime is when the session ended (zero if still active).
	EndTime time.Time

	// BytesRead is the total bytes read from the client.
	BytesRead ByteCount

	// BytesWritten is the total bytes written to the client.
	BytesWritten ByteCount

	// CommandCount is the number of commands processed.
	CommandCount CommandCount

	// ErrorCount is the number of errors encountered.
	ErrorCount ErrorCount

	// TransactionCount is the number of completed mail transactions.
	TransactionCount TransactionCount

	// MessageCount is the number of messages received.
	MessageCount MessageCount

	// RecipientCount is the total recipients across all messages.
	RecipientCount RecipientCount
}

// CommandCount is a count of commands.
type CommandCount = int

// MessageCount is a count of messages.
type MessageCount = int

// SessionState contains the mutable state of a session.
type SessionState struct {
	// State is the current protocol state.
	State State

	// ClientHostname is the hostname from HELO/EHLO.
	ClientHostname Hostname

	// TLSActive indicates TLS is active.
	TLSActive bool

	// TLSState contains TLS connection state if active.
	TLSState *TLSConnectionState

	// Authenticated indicates successful authentication.
	Authenticated bool

	// AuthenticatedUser is the authenticated username.
	AuthenticatedUser Username

	// Envelope is the current envelope builder, if in a transaction.
	Envelope EnvelopeBuilder

	// ConsecutiveErrors tracks consecutive protocol errors.
	ConsecutiveErrors ErrorCount
}
