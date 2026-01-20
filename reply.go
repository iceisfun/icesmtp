package icesmtp

import (
	"fmt"
	"strings"
)

// ReplyCode represents a three-digit SMTP reply code.
// Reply codes are defined in RFC 5321 Section 4.2.
type ReplyCode int

// Reply code categories (first digit).
const (
	// ReplyPositivePreliminary (1yz): Positive Preliminary reply.
	// The command has been accepted but the requested action is being held
	// in abeyance, pending confirmation of the information in this reply.
	ReplyPositivePreliminary ReplyCode = 100

	// ReplyPositiveCompletion (2yz): Positive Completion reply.
	// The requested action has been successfully completed.
	ReplyPositiveCompletion ReplyCode = 200

	// ReplyPositiveIntermediate (3yz): Positive Intermediate reply.
	// The command has been accepted but the requested action is being held
	// in abeyance, pending receipt of further information.
	ReplyPositiveIntermediate ReplyCode = 300

	// ReplyTransientNegative (4yz): Transient Negative Completion reply.
	// The command was not accepted; the requested action did not occur.
	// The error condition is temporary.
	ReplyTransientNegative ReplyCode = 400

	// ReplyPermanentNegative (5yz): Permanent Negative Completion reply.
	// The command was not accepted; the requested action did not occur.
	// The error condition is permanent.
	ReplyPermanentNegative ReplyCode = 500
)

// Standard SMTP reply codes (RFC 5321).
const (
	// 2yz Positive Completion
	Reply211SystemStatus    ReplyCode = 211
	Reply214HelpMessage     ReplyCode = 214
	Reply220ServiceReady    ReplyCode = 220
	Reply221ServiceClosing  ReplyCode = 221
	Reply250OK              ReplyCode = 250
	Reply251UserNotLocal    ReplyCode = 251
	Reply252CannotVRFY      ReplyCode = 252

	// 3yz Positive Intermediate
	Reply354StartMailInput ReplyCode = 354

	// 4yz Transient Negative
	Reply421ServiceNotAvailable ReplyCode = 421
	Reply450MailboxUnavailable  ReplyCode = 450
	Reply451LocalError          ReplyCode = 451
	Reply452InsufficientStorage ReplyCode = 452
	Reply455ParamsNotAccommodated ReplyCode = 455

	// 5yz Permanent Negative
	Reply500SyntaxError          ReplyCode = 500
	Reply501SyntaxErrorParams    ReplyCode = 501
	Reply502CommandNotImplemented ReplyCode = 502
	Reply503BadSequence          ReplyCode = 503
	Reply504ParamNotImplemented  ReplyCode = 504
	Reply550MailboxUnavailable   ReplyCode = 550
	Reply551UserNotLocal         ReplyCode = 551
	Reply552ExceededStorage      ReplyCode = 552
	Reply553MailboxNameInvalid   ReplyCode = 553
	Reply554TransactionFailed    ReplyCode = 554
	Reply555ParamsNotRecognized  ReplyCode = 555
)

// IsPositive returns true if this is a positive (2xx or 3xx) reply code.
func (c ReplyCode) IsPositive() bool {
	return c >= 200 && c < 400
}

// IsNegative returns true if this is a negative (4xx or 5xx) reply code.
func (c ReplyCode) IsNegative() bool {
	return c >= 400
}

// IsTransient returns true if this is a transient (4xx) error.
func (c ReplyCode) IsTransient() bool {
	return c >= 400 && c < 500
}

// IsPermanent returns true if this is a permanent (5xx) error.
func (c ReplyCode) IsPermanent() bool {
	return c >= 500
}

// Category returns the category (first digit * 100) of this reply code.
func (c ReplyCode) Category() ReplyCode {
	return (c / 100) * 100
}

// EnhancedStatusCode represents an enhanced status code (RFC 3463).
// Format: class.subject.detail (e.g., 2.1.0).
type EnhancedStatusCode struct {
	Class   EnhancedStatusClass
	Subject EnhancedStatusSubject
	Detail  EnhancedStatusDetail
}

// EnhancedStatusClass is the first digit of an enhanced status code.
type EnhancedStatusClass int

const (
	// EnhancedSuccess (2.x.x) indicates success.
	EnhancedSuccess EnhancedStatusClass = 2
	// EnhancedPersistentTransient (4.x.x) indicates a persistent transient failure.
	EnhancedPersistentTransient EnhancedStatusClass = 4
	// EnhancedPermanent (5.x.x) indicates a permanent failure.
	EnhancedPermanent EnhancedStatusClass = 5
)

// EnhancedStatusSubject is the second component of an enhanced status code.
type EnhancedStatusSubject int

const (
	// EnhancedSubjectOther (x.0.x) indicates other or undefined status.
	EnhancedSubjectOther EnhancedStatusSubject = 0
	// EnhancedSubjectAddressing (x.1.x) indicates addressing status.
	EnhancedSubjectAddressing EnhancedStatusSubject = 1
	// EnhancedSubjectMailbox (x.2.x) indicates mailbox status.
	EnhancedSubjectMailbox EnhancedStatusSubject = 2
	// EnhancedSubjectMailSystem (x.3.x) indicates mail system status.
	EnhancedSubjectMailSystem EnhancedStatusSubject = 3
	// EnhancedSubjectNetwork (x.4.x) indicates network and routing status.
	EnhancedSubjectNetwork EnhancedStatusSubject = 4
	// EnhancedSubjectDelivery (x.5.x) indicates mail delivery protocol status.
	EnhancedSubjectDelivery EnhancedStatusSubject = 5
	// EnhancedSubjectContent (x.6.x) indicates message content or media status.
	EnhancedSubjectContent EnhancedStatusSubject = 6
	// EnhancedSubjectPolicy (x.7.x) indicates security or policy status.
	EnhancedSubjectPolicy EnhancedStatusSubject = 7
)

// EnhancedStatusDetail is the third component of an enhanced status code.
type EnhancedStatusDetail int

// String returns the enhanced status code as a string (e.g., "2.1.0").
func (e EnhancedStatusCode) String() string {
	return fmt.Sprintf("%d.%d.%d", e.Class, e.Subject, e.Detail)
}

// Response represents a complete SMTP response including code and text.
type Response struct {
	// Code is the three-digit reply code.
	Code ReplyCode

	// EnhancedCode is the optional enhanced status code (RFC 3463).
	// May be nil if enhanced status codes are not used.
	EnhancedCode *EnhancedStatusCode

	// Lines contains the response text lines.
	// Each line will be prefixed with the reply code when sent.
	Lines []ResponseLine
}

// ResponseLine is a single line of response text.
type ResponseLine = string

// String returns the formatted SMTP response ready to send.
// Multi-line responses use code-hyphen-text for intermediate lines
// and code-space-text for the final line.
func (r Response) String() string {
	if len(r.Lines) == 0 {
		if r.EnhancedCode != nil {
			return fmt.Sprintf("%d %s\r\n", r.Code, r.EnhancedCode.String())
		}
		return fmt.Sprintf("%d\r\n", r.Code)
	}

	var b strings.Builder
	lastIdx := len(r.Lines) - 1

	for i, line := range r.Lines {
		if i == lastIdx {
			if r.EnhancedCode != nil {
				b.WriteString(fmt.Sprintf("%d %s %s\r\n", r.Code, r.EnhancedCode.String(), line))
			} else {
				b.WriteString(fmt.Sprintf("%d %s\r\n", r.Code, line))
			}
		} else {
			if r.EnhancedCode != nil {
				b.WriteString(fmt.Sprintf("%d-%s %s\r\n", r.Code, r.EnhancedCode.String(), line))
			} else {
				b.WriteString(fmt.Sprintf("%d-%s\r\n", r.Code, line))
			}
		}
	}

	return b.String()
}

// Bytes returns the formatted SMTP response as bytes.
func (r Response) Bytes() []byte {
	return []byte(r.String())
}

// NewResponse creates a simple single-line response.
func NewResponse(code ReplyCode, text string) Response {
	return Response{
		Code:  code,
		Lines: []ResponseLine{text},
	}
}

// NewEnhancedResponse creates a response with an enhanced status code.
func NewEnhancedResponse(code ReplyCode, enhanced EnhancedStatusCode, text string) Response {
	return Response{
		Code:         code,
		EnhancedCode: &enhanced,
		Lines:        []ResponseLine{text},
	}
}

// NewMultilineResponse creates a multi-line response.
func NewMultilineResponse(code ReplyCode, lines ...string) Response {
	return Response{
		Code:  code,
		Lines: lines,
	}
}

// Common pre-built responses.
var (
	// ResponseServiceReady is the standard 220 greeting.
	ResponseServiceReady = NewResponse(Reply220ServiceReady, "Service ready")

	// ResponseBye is the standard 221 closing response.
	ResponseBye = NewResponse(Reply221ServiceClosing, "Bye")

	// ResponseOK is the standard 250 OK response.
	ResponseOK = NewResponse(Reply250OK, "OK")

	// ResponseStartMailInput is the standard 354 response before DATA.
	ResponseStartMailInput = NewResponse(Reply354StartMailInput, "Start mail input; end with <CRLF>.<CRLF>")

	// ResponseSyntaxError is a generic 500 syntax error.
	ResponseSyntaxError = NewResponse(Reply500SyntaxError, "Syntax error, command unrecognized")

	// ResponseSyntaxErrorParams is a generic 501 parameter syntax error.
	ResponseSyntaxErrorParams = NewResponse(Reply501SyntaxErrorParams, "Syntax error in parameters or arguments")

	// ResponseCommandNotImplemented is a 502 not implemented response.
	ResponseCommandNotImplemented = NewResponse(Reply502CommandNotImplemented, "Command not implemented")

	// ResponseBadSequence is a 503 bad sequence response.
	ResponseBadSequence = NewResponse(Reply503BadSequence, "Bad sequence of commands")

	// ResponseMailboxUnavailable is a 550 mailbox unavailable response.
	ResponseMailboxUnavailable = NewResponse(Reply550MailboxUnavailable, "Mailbox unavailable")

	// ResponseTransactionFailed is a 554 transaction failed response.
	ResponseTransactionFailed = NewResponse(Reply554TransactionFailed, "Transaction failed")
)
