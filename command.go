package icesmtp

import (
	"strings"
)

// CommandVerb represents an SMTP command verb.
// Commands are case-insensitive per RFC 5321, but stored uppercase internally.
type CommandVerb string

const (
	// CmdHELO identifies the client with a simple hostname (RFC 5321).
	CmdHELO CommandVerb = "HELO"

	// CmdEHLO identifies the client and requests extended SMTP (RFC 5321).
	CmdEHLO CommandVerb = "EHLO"

	// CmdMAIL initiates a mail transaction with MAIL FROM (RFC 5321).
	CmdMAIL CommandVerb = "MAIL"

	// CmdRCPT specifies a recipient with RCPT TO (RFC 5321).
	CmdRCPT CommandVerb = "RCPT"

	// CmdDATA indicates the client is ready to send message content (RFC 5321).
	CmdDATA CommandVerb = "DATA"

	// CmdRSET aborts the current mail transaction and resets state (RFC 5321).
	CmdRSET CommandVerb = "RSET"

	// CmdNOOP performs no operation; used to keep connection alive (RFC 5321).
	CmdNOOP CommandVerb = "NOOP"

	// CmdQUIT terminates the session (RFC 5321).
	CmdQUIT CommandVerb = "QUIT"

	// CmdVRFY verifies a user or mailbox name (RFC 5321).
	// Often disabled or restricted for security reasons.
	CmdVRFY CommandVerb = "VRFY"

	// CmdEXPN expands a mailing list (RFC 5321).
	// Often disabled or restricted for security reasons.
	CmdEXPN CommandVerb = "EXPN"

	// CmdHELP requests help information (RFC 5321).
	CmdHELP CommandVerb = "HELP"

	// CmdSTARTTLS initiates TLS negotiation (RFC 3207).
	CmdSTARTTLS CommandVerb = "STARTTLS"

	// CmdAUTH initiates SASL authentication (RFC 4954).
	CmdAUTH CommandVerb = "AUTH"

	// CmdUnknown represents an unrecognized command.
	CmdUnknown CommandVerb = ""
)

// String returns the command verb as a string.
func (c CommandVerb) String() string {
	return string(c)
}

// ParseCommandVerb parses a command verb string into a CommandVerb.
// The input is normalized to uppercase.
func ParseCommandVerb(s string) CommandVerb {
	verb := CommandVerb(strings.ToUpper(strings.TrimSpace(s)))
	switch verb {
	case CmdHELO, CmdEHLO, CmdMAIL, CmdRCPT, CmdDATA, CmdRSET,
		CmdNOOP, CmdQUIT, CmdVRFY, CmdEXPN, CmdHELP, CmdSTARTTLS, CmdAUTH:
		return verb
	default:
		return CmdUnknown
	}
}

// Command represents a parsed SMTP command with its verb and arguments.
type Command struct {
	// Verb is the command verb (e.g., HELO, MAIL, RCPT).
	Verb CommandVerb

	// Raw is the original command line as received, including CRLF.
	Raw CommandLine

	// Argument is the full argument string after the verb.
	// For MAIL FROM:<addr>, this would be "FROM:<addr>".
	Argument CommandArgument

	// Params contains parsed ESMTP parameters for MAIL and RCPT commands.
	// For MAIL FROM:<addr> SIZE=1000, Params would contain {"SIZE": "1000"}.
	Params ESMTPParams
}

// CommandLine represents a raw SMTP command line as received from the client.
type CommandLine = string

// CommandArgument represents the argument portion of an SMTP command.
type CommandArgument = string

// ESMTPParams represents ESMTP extension parameters.
// Keys are parameter names (uppercase), values are parameter values.
type ESMTPParams map[ESMTPParamName]ESMTPParamValue

// ESMTPParamName is the name of an ESMTP parameter (e.g., SIZE, BODY).
type ESMTPParamName = string

// ESMTPParamValue is the value of an ESMTP parameter.
type ESMTPParamValue = string

// MailPath represents a parsed reverse-path or forward-path.
// In SMTP, paths can include source routes, though these are rarely used.
type MailPath struct {
	// Address is the email address portion (local-part@domain).
	Address EmailAddress

	// SourceRoute contains any source routing information (deprecated).
	// Modern implementations typically ignore this.
	SourceRoute SourceRoute

	// IsNull indicates this is the null reverse-path (<>).
	// Used for bounce messages and delivery status notifications.
	IsNull bool
}

// EmailAddress represents an email address in the form local-part@domain.
type EmailAddress = string

// SourceRoute represents the deprecated source routing portion of a path.
// Per RFC 5321, source routes should be ignored.
type SourceRoute = string

// LocalPart represents the local part of an email address (before the @).
type LocalPart = string

// Domain represents the domain part of an email address (after the @).
type Domain = string

// Hostname represents a hostname used in HELO/EHLO commands.
type Hostname = string

// AddressLiteral represents an address literal [x.x.x.x] or [IPv6:...].
type AddressLiteral = string

// CommandResult represents the outcome of processing a command.
type CommandResult struct {
	// Response is the SMTP response to send to the client.
	Response Response

	// NewState is the state to transition to after this command.
	// If nil, the state remains unchanged.
	NewState *State

	// CloseConnection indicates the connection should be closed after responding.
	CloseConnection bool

	// StartTLS indicates TLS negotiation should begin after responding.
	StartTLS bool

	// Envelope contains envelope modifications from this command.
	Envelope *EnvelopeUpdate
}

// EnvelopeUpdate contains changes to the current envelope from a command.
type EnvelopeUpdate struct {
	// SetMailFrom sets the envelope sender.
	SetMailFrom *MailPath

	// AddRecipient adds a recipient to the envelope.
	AddRecipient *MailPath

	// Reset indicates the envelope should be cleared.
	Reset bool
}

// AllowedCommands returns the commands allowed in the given state.
func AllowedCommands(state State) []CommandVerb {
	switch state {
	case StateGreeted:
		return []CommandVerb{CmdHELO, CmdEHLO, CmdQUIT, CmdNOOP, CmdHELP, CmdRSET}
	case StateIdentified:
		return []CommandVerb{CmdHELO, CmdEHLO, CmdMAIL, CmdQUIT, CmdNOOP, CmdHELP, CmdRSET, CmdVRFY, CmdEXPN, CmdSTARTTLS, CmdAUTH}
	case StateMailFrom:
		return []CommandVerb{CmdRCPT, CmdRSET, CmdQUIT, CmdNOOP, CmdHELP}
	case StateRcptTo:
		return []CommandVerb{CmdRCPT, CmdDATA, CmdRSET, CmdQUIT, CmdNOOP, CmdHELP}
	case StateData:
		// In DATA state, no commands are accepted; only message content.
		return nil
	default:
		return nil
	}
}

// IsCommandAllowed checks if a command is allowed in the given state.
func IsCommandAllowed(state State, cmd CommandVerb) bool {
	allowed := AllowedCommands(state)
	for _, c := range allowed {
		if c == cmd {
			return true
		}
	}
	return false
}

// CommandRequiresArgument returns true if the command requires an argument.
func CommandRequiresArgument(cmd CommandVerb) bool {
	switch cmd {
	case CmdHELO, CmdEHLO, CmdMAIL, CmdRCPT:
		return true
	default:
		return false
	}
}

// CommandForbidsArgument returns true if the command must not have an argument.
func CommandForbidsArgument(cmd CommandVerb) bool {
	switch cmd {
	case CmdDATA, CmdRSET, CmdQUIT, CmdSTARTTLS:
		return true
	default:
		return false
	}
}
