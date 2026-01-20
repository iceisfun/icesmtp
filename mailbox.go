package icesmtp

import "context"

// Mailbox defines the interface for recipient validation and mailbox operations.
// Implementations may back this with databases, LDAP, APIs, or static configuration.
type Mailbox interface {
	// ValidateRecipient checks whether a recipient address is valid and acceptable.
	// This is called during RCPT TO processing.
	// The context may be used for timeouts and cancellation.
	ValidateRecipient(ctx context.Context, recipient MailPath, session SessionInfo) RecipientResult
}

// SessionInfo provides read-only information about the current session.
// This is passed to Mailbox implementations for policy decisions.
type SessionInfo interface {
	// ID returns the session identifier.
	ID() SessionID

	// State returns the current session state.
	State() State

	// ClientHostname returns the hostname from HELO/EHLO.
	ClientHostname() Hostname

	// ClientIP returns the client's IP address.
	ClientIP() IPAddress

	// TLSActive returns true if TLS is active.
	TLSActive() bool

	// Authenticated returns true if the client has authenticated.
	Authenticated() bool

	// AuthenticatedUser returns the authenticated username, if any.
	AuthenticatedUser() Username

	// CurrentMailFrom returns the current envelope sender, if in a transaction.
	CurrentMailFrom() *MailPath

	// CurrentRecipientCount returns the number of accepted recipients so far.
	CurrentRecipientCount() RecipientCount
}

// MailboxExtended provides additional optional operations beyond basic validation.
type MailboxExtended interface {
	Mailbox

	// Exists checks if a mailbox exists without full validation.
	// May be used for VRFY command if enabled.
	Exists(ctx context.Context, address EmailAddress) (bool, error)

	// CanReceive checks if the mailbox can currently receive mail.
	// This may check quotas, account status, etc.
	CanReceive(ctx context.Context, address EmailAddress) (bool, MailboxStatus, error)
}

// MailboxStatus indicates the status of a mailbox.
type MailboxStatus int

const (
	// MailboxStatusOK indicates the mailbox can receive mail.
	MailboxStatusOK MailboxStatus = iota

	// MailboxStatusNotFound indicates the mailbox does not exist.
	MailboxStatusNotFound

	// MailboxStatusDisabled indicates the mailbox is disabled.
	MailboxStatusDisabled

	// MailboxStatusOverQuota indicates the mailbox is over quota.
	MailboxStatusOverQuota

	// MailboxStatusTemporarilyUnavailable indicates a transient error.
	MailboxStatusTemporarilyUnavailable
)

// String returns a human-readable status description.
func (s MailboxStatus) String() string {
	switch s {
	case MailboxStatusOK:
		return "OK"
	case MailboxStatusNotFound:
		return "NotFound"
	case MailboxStatusDisabled:
		return "Disabled"
	case MailboxStatusOverQuota:
		return "OverQuota"
	case MailboxStatusTemporarilyUnavailable:
		return "TemporarilyUnavailable"
	default:
		return "Unknown"
	}
}

// ToReplyCode converts a mailbox status to an appropriate SMTP reply code.
func (s MailboxStatus) ToReplyCode() ReplyCode {
	switch s {
	case MailboxStatusOK:
		return Reply250OK
	case MailboxStatusNotFound:
		return Reply550MailboxUnavailable
	case MailboxStatusDisabled:
		return Reply550MailboxUnavailable
	case MailboxStatusOverQuota:
		return Reply552ExceededStorage
	case MailboxStatusTemporarilyUnavailable:
		return Reply450MailboxUnavailable
	default:
		return Reply451LocalError
	}
}

// SenderPolicy defines the interface for sender (MAIL FROM) validation.
// This is separate from Mailbox to allow different validation strategies.
type SenderPolicy interface {
	// ValidateSender checks whether a sender address is acceptable.
	// This is called during MAIL FROM processing.
	ValidateSender(ctx context.Context, sender MailPath, session SessionInfo) SenderResult
}

// SenderResult contains the result of validating a sender.
type SenderResult struct {
	// Accepted indicates whether the sender was accepted.
	Accepted bool

	// Response is the SMTP response to send.
	Response Response

	// RequireAuth indicates authentication is required for this sender.
	RequireAuth bool
}

// SenderResultAccepted returns a successful sender validation result.
func SenderResultAccepted() SenderResult {
	return SenderResult{
		Accepted: true,
		Response: ResponseOK,
	}
}

// SenderResultRejected returns a rejection result with a custom response.
func SenderResultRejected(response Response) SenderResult {
	return SenderResult{
		Accepted: false,
		Response: response,
	}
}

// DomainPolicy defines the interface for domain-level policy decisions.
type DomainPolicy interface {
	// IsLocalDomain checks if a domain is considered local.
	// Local domains are domains this server accepts mail for.
	IsLocalDomain(ctx context.Context, domain Domain) (bool, error)

	// AcceptedDomains returns a list of all accepted domains.
	AcceptedDomains(ctx context.Context) ([]Domain, error)

	// RelayAllowed checks if relaying is allowed for a domain.
	// Relaying is delivering mail to non-local domains.
	RelayAllowed(ctx context.Context, domain Domain, session SessionInfo) (bool, error)
}

// AcceptAllMailbox is a Mailbox implementation that accepts all recipients.
// Useful for testing or open relay scenarios (use with caution).
type AcceptAllMailbox struct{}

// ValidateRecipient accepts all recipients unconditionally.
func (AcceptAllMailbox) ValidateRecipient(_ context.Context, recipient MailPath, _ SessionInfo) RecipientResult {
	return RecipientResult{
		Path:     recipient,
		Status:   RecipientAccepted,
		Response: ResponseOK,
	}
}

// RejectAllMailbox is a Mailbox implementation that rejects all recipients.
// Useful for testing.
type RejectAllMailbox struct{}

// ValidateRecipient rejects all recipients unconditionally.
func (RejectAllMailbox) ValidateRecipient(_ context.Context, recipient MailPath, _ SessionInfo) RecipientResult {
	return RecipientResult{
		Path:     recipient,
		Status:   RecipientRejected,
		Response: ResponseMailboxUnavailable,
	}
}

// AcceptAllSenderPolicy is a SenderPolicy that accepts all senders.
type AcceptAllSenderPolicy struct{}

// ValidateSender accepts all senders unconditionally.
func (AcceptAllSenderPolicy) ValidateSender(_ context.Context, _ MailPath, _ SessionInfo) SenderResult {
	return SenderResultAccepted()
}
