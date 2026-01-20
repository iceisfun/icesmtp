package icesmtp

import (
	"context"
	"errors"
)

// Common limit-related errors.
var (
	// ErrMessageTooLarge indicates the message exceeds the size limit.
	ErrMessageTooLarge = errors.New("message exceeds maximum size")

	// ErrTooManyRecipients indicates too many recipients.
	ErrTooManyRecipients = errors.New("too many recipients")

	// ErrCommandTooLong indicates a command line is too long.
	ErrCommandTooLong = errors.New("command line too long")

	// ErrLineTooLong indicates a data line is too long.
	ErrLineTooLong = errors.New("line too long")

	// ErrTooManyErrors indicates too many consecutive errors.
	ErrTooManyErrors = errors.New("too many errors")

	// ErrTooManyTransactions indicates too many transactions in one session.
	ErrTooManyTransactions = errors.New("too many transactions")

	// ErrTimeout indicates a timeout occurred.
	ErrTimeout = errors.New("timeout")

	// ErrConnectionClosed indicates the connection was closed.
	ErrConnectionClosed = errors.New("connection closed")
)

// LimitChecker validates operations against configured limits.
type LimitChecker interface {
	// CheckMessageSize validates message size against the limit.
	CheckMessageSize(size MessageSize) error

	// CheckRecipientCount validates recipient count against the limit.
	CheckRecipientCount(count RecipientCount) error

	// CheckCommandLength validates command line length.
	CheckCommandLength(length CommandLength) error

	// CheckLineLength validates data line length.
	CheckLineLength(length LineLength) error

	// CheckErrorCount validates consecutive error count.
	CheckErrorCount(count ErrorCount) error

	// CheckTransactionCount validates transaction count.
	CheckTransactionCount(count TransactionCount) error
}

// StandardLimitChecker implements LimitChecker with SessionLimits.
type StandardLimitChecker struct {
	Limits SessionLimits
}

// CheckMessageSize validates message size.
func (c *StandardLimitChecker) CheckMessageSize(size MessageSize) error {
	if c.Limits.MaxMessageSize > 0 && size > c.Limits.MaxMessageSize {
		return ErrMessageTooLarge
	}
	return nil
}

// CheckRecipientCount validates recipient count.
func (c *StandardLimitChecker) CheckRecipientCount(count RecipientCount) error {
	if c.Limits.MaxRecipients > 0 && count > c.Limits.MaxRecipients {
		return ErrTooManyRecipients
	}
	return nil
}

// CheckCommandLength validates command line length.
func (c *StandardLimitChecker) CheckCommandLength(length CommandLength) error {
	if c.Limits.MaxCommandLength > 0 && length > c.Limits.MaxCommandLength {
		return ErrCommandTooLong
	}
	return nil
}

// CheckLineLength validates data line length.
func (c *StandardLimitChecker) CheckLineLength(length LineLength) error {
	if c.Limits.MaxLineLength > 0 && length > c.Limits.MaxLineLength {
		return ErrLineTooLong
	}
	return nil
}

// CheckErrorCount validates consecutive error count.
func (c *StandardLimitChecker) CheckErrorCount(count ErrorCount) error {
	if c.Limits.MaxErrors > 0 && count >= c.Limits.MaxErrors {
		return ErrTooManyErrors
	}
	return nil
}

// CheckTransactionCount validates transaction count.
func (c *StandardLimitChecker) CheckTransactionCount(count TransactionCount) error {
	if c.Limits.MaxTransactions > 0 && count >= c.Limits.MaxTransactions {
		return ErrTooManyTransactions
	}
	return nil
}

// RateLimitPolicy defines rate limiting behavior.
type RateLimitPolicy int

const (
	// RateLimitNone disables rate limiting.
	RateLimitNone RateLimitPolicy = iota

	// RateLimitDelay applies delays when rate is exceeded.
	RateLimitDelay

	// RateLimitReject rejects when rate is exceeded.
	RateLimitReject
)

// RateLimiter controls the rate of operations.
type RateLimiter interface {
	// Allow checks if an operation is allowed.
	// Returns true if allowed, false if rate limit exceeded.
	Allow(ctx context.Context, key RateLimitKey) bool

	// AllowN checks if n operations are allowed.
	AllowN(ctx context.Context, key RateLimitKey, n int) bool
}

// RateLimitKey identifies what is being rate limited.
type RateLimitKey = string

// ConnectionPolicy defines connection acceptance policy.
type ConnectionPolicy interface {
	// Accept checks if a connection should be accepted.
	// Returns true to accept, false to reject with the given response.
	Accept(ctx context.Context, info ConnectionInfo) (bool, Response)
}

// ConnectionInfo contains information about an incoming connection.
type ConnectionInfo struct {
	// RemoteAddr is the remote address (IP:port).
	RemoteAddr RemoteAddress

	// RemoteIP is just the IP portion.
	RemoteIP IPAddress

	// LocalAddr is the local address (IP:port).
	LocalAddr LocalAddress

	// TLS indicates if this is a TLS connection (SMTPS).
	TLS bool
}

// RemoteAddress is a remote address string (IP:port).
type RemoteAddress = string

// LocalAddress is a local address string (IP:port).
type LocalAddress = string

// GracePeriod is a time duration for grace periods.
type GracePeriod = Duration

// PolicyDecision represents a policy check result.
type PolicyDecision int

const (
	// PolicyAllow indicates the action is allowed.
	PolicyAllow PolicyDecision = iota

	// PolicyDeny indicates the action is denied (permanent).
	PolicyDeny

	// PolicyDefer indicates the decision is deferred (try later).
	PolicyDefer

	// PolicyContinue indicates no decision; continue to next policy.
	PolicyContinue
)

// PolicyResult contains the result of a policy check.
type PolicyResult struct {
	// Decision is the policy decision.
	Decision PolicyDecision

	// Response is the SMTP response to send if not PolicyAllow.
	Response Response

	// Reason is a human-readable explanation.
	Reason PolicyReason
}

// PolicyReason is a human-readable policy reason.
type PolicyReason = string

// PolicyAllowed returns a PolicyResult indicating the action is allowed.
func PolicyAllowed() PolicyResult {
	return PolicyResult{Decision: PolicyAllow}
}

// PolicyDenied returns a PolicyResult indicating permanent denial.
func PolicyDenied(response Response, reason PolicyReason) PolicyResult {
	return PolicyResult{
		Decision: PolicyDeny,
		Response: response,
		Reason:   reason,
	}
}

// PolicyDeferred returns a PolicyResult indicating temporary denial.
func PolicyDeferred(response Response, reason PolicyReason) PolicyResult {
	return PolicyResult{
		Decision: PolicyDefer,
		Response: response,
		Reason:   reason,
	}
}

// LimitViolation records details of a limit violation.
type LimitViolation struct {
	// Type identifies what limit was violated.
	Type LimitType

	// Limit is the configured limit value.
	Limit int64

	// Actual is the actual value that violated the limit.
	Actual int64

	// SessionID identifies the session.
	SessionID SessionID
}

// LimitType identifies a type of limit.
type LimitType = string

const (
	// LimitTypeMessageSize is the message size limit.
	LimitTypeMessageSize LimitType = "MessageSize"

	// LimitTypeRecipients is the recipient count limit.
	LimitTypeRecipients LimitType = "Recipients"

	// LimitTypeCommandLength is the command line length limit.
	LimitTypeCommandLength LimitType = "CommandLength"

	// LimitTypeLineLength is the data line length limit.
	LimitTypeLineLength LimitType = "LineLength"

	// LimitTypeErrors is the consecutive error limit.
	LimitTypeErrors LimitType = "Errors"

	// LimitTypeTransactions is the transaction count limit.
	LimitTypeTransactions LimitType = "Transactions"
)
