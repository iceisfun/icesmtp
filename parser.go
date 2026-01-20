package icesmtp

import (
	"bytes"
	"errors"
	"strings"
)

// Parser errors.
var (
	// ErrEmptyCommand indicates an empty command line.
	ErrEmptyCommand = errors.New("empty command")

	// ErrInvalidCommand indicates an unrecognized command.
	ErrInvalidCommand = errors.New("invalid command")

	// ErrMissingArgument indicates a required argument is missing.
	ErrMissingArgument = errors.New("missing required argument")

	// ErrUnexpectedArgument indicates an argument was provided when not allowed.
	ErrUnexpectedArgument = errors.New("unexpected argument")

	// ErrInvalidPath indicates an invalid mail path.
	ErrInvalidPath = errors.New("invalid mail path")

	// ErrInvalidAddress indicates an invalid email address.
	ErrInvalidAddress = errors.New("invalid email address")

	// ErrMissingColon indicates missing colon in MAIL/RCPT command.
	ErrMissingColon = errors.New("missing colon after FROM or TO")

	// ErrInvalidSyntax indicates general syntax error.
	ErrInvalidSyntax = errors.New("syntax error")
)

// ParseError contains details about a parsing error.
type ParseError struct {
	// Err is the underlying error.
	Err error

	// Position is the byte position where the error occurred.
	Position ParsePosition

	// Context is additional context about the error.
	Context string

	// Input is the original input that failed to parse.
	Input string
}

// ParsePosition is a position in the input.
type ParsePosition = int

func (e *ParseError) Error() string {
	if e.Context != "" {
		return e.Err.Error() + ": " + e.Context
	}
	return e.Err.Error()
}

func (e *ParseError) Unwrap() error {
	return e.Err
}

// Parser parses SMTP commands.
type Parser struct {
	// MaxCommandLength is the maximum allowed command line length.
	MaxCommandLength CommandLength
}

// NewParser creates a new parser with default settings.
func NewParser() *Parser {
	return &Parser{
		MaxCommandLength: 512, // RFC 5321
	}
}

// ParseCommand parses a single SMTP command line.
// The input should include the trailing CRLF.
func (p *Parser) ParseCommand(line []byte) (*Command, error) {
	// Check length limit
	if p.MaxCommandLength > 0 && len(line) > p.MaxCommandLength {
		return nil, &ParseError{
			Err:   ErrCommandTooLong,
			Input: string(line),
		}
	}

	// Trim CRLF
	line = bytes.TrimSuffix(line, []byte("\r\n"))
	line = bytes.TrimSuffix(line, []byte("\n"))

	if len(line) == 0 {
		return nil, &ParseError{Err: ErrEmptyCommand}
	}

	// Split into verb and argument
	verb, arg := splitCommand(line)

	cmdVerb := ParseCommandVerb(string(verb))
	if cmdVerb == CmdUnknown {
		return nil, &ParseError{
			Err:     ErrInvalidCommand,
			Input:   string(line),
			Context: string(verb),
		}
	}

	// Validate argument requirements
	argStr := strings.TrimSpace(string(arg))

	if CommandRequiresArgument(cmdVerb) && argStr == "" {
		return nil, &ParseError{
			Err:     ErrMissingArgument,
			Context: cmdVerb.String() + " requires an argument",
		}
	}

	if CommandForbidsArgument(cmdVerb) && argStr != "" {
		return nil, &ParseError{
			Err:     ErrUnexpectedArgument,
			Context: cmdVerb.String() + " does not accept arguments",
		}
	}

	cmd := &Command{
		Verb:     cmdVerb,
		Raw:      string(line),
		Argument: argStr,
	}

	// Parse ESMTP parameters for MAIL and RCPT
	if cmdVerb == CmdMAIL || cmdVerb == CmdRCPT {
		params, err := parseESMTPParams(argStr)
		if err == nil {
			cmd.Params = params
		}
	}

	return cmd, nil
}

// splitCommand splits a command line into verb and argument parts.
func splitCommand(line []byte) (verb []byte, arg []byte) {
	idx := bytes.IndexByte(line, ' ')
	if idx == -1 {
		return line, nil
	}
	return line[:idx], line[idx+1:]
}

// parseESMTPParams extracts ESMTP parameters from the argument string.
// For "FROM:<addr> SIZE=1000 BODY=8BITMIME", returns {"SIZE": "1000", "BODY": "8BITMIME"}.
func parseESMTPParams(arg string) (ESMTPParams, error) {
	// Find the end of the path (after the >)
	closeIdx := strings.Index(arg, ">")
	if closeIdx == -1 {
		return nil, nil // No path, no params
	}

	remainder := strings.TrimSpace(arg[closeIdx+1:])
	if remainder == "" {
		return nil, nil
	}

	params := make(ESMTPParams)
	parts := strings.Fields(remainder)

	for _, part := range parts {
		eqIdx := strings.Index(part, "=")
		if eqIdx == -1 {
			// Keyword without value
			params[strings.ToUpper(part)] = ""
		} else {
			key := strings.ToUpper(part[:eqIdx])
			value := part[eqIdx+1:]
			params[key] = value
		}
	}

	return params, nil
}

// ParseMailPath parses a mail path from MAIL FROM or RCPT TO arguments.
// Input should be "FROM:<path>" or "TO:<path>".
func ParseMailPath(arg string, prefix string) (*MailPath, error) {
	arg = strings.TrimSpace(arg)

	// Check for prefix (FROM: or TO:)
	upperArg := strings.ToUpper(arg)
	if !strings.HasPrefix(upperArg, prefix+":") {
		return nil, &ParseError{
			Err:     ErrMissingColon,
			Context: "expected " + prefix + ":",
			Input:   arg,
		}
	}

	// Extract the path portion after "FROM:" or "TO:"
	pathPart := strings.TrimSpace(arg[len(prefix)+1:])

	// Extract just the <path> portion, ignoring any ESMTP params after
	path, err := extractPath(pathPart)
	if err != nil {
		return nil, err
	}

	return path, nil
}

// extractPath extracts the path from a string that may contain ESMTP params.
// Input should be "<path> [params]" or "<>" for null sender.
func extractPath(s string) (*MailPath, error) {
	s = strings.TrimSpace(s)

	if !strings.HasPrefix(s, "<") {
		return nil, &ParseError{
			Err:     ErrInvalidPath,
			Context: "path must start with <",
			Input:   s,
		}
	}

	closeIdx := strings.Index(s, ">")
	if closeIdx == -1 {
		return nil, &ParseError{
			Err:     ErrInvalidPath,
			Context: "path must end with >",
			Input:   s,
		}
	}

	inner := s[1:closeIdx]

	// Check for null path
	if inner == "" {
		return &MailPath{IsNull: true}, nil
	}

	// Check for source route (deprecated but must be parsed)
	var sourceRoute SourceRoute
	if strings.HasPrefix(inner, "@") {
		colonIdx := strings.Index(inner, ":")
		if colonIdx != -1 {
			sourceRoute = inner[:colonIdx+1]
			inner = inner[colonIdx+1:]
		}
	}

	// Validate the address portion
	if !isValidAddress(inner) {
		return nil, &ParseError{
			Err:     ErrInvalidAddress,
			Context: "invalid address format",
			Input:   inner,
		}
	}

	return &MailPath{
		Address:     inner,
		SourceRoute: sourceRoute,
		IsNull:      false,
	}, nil
}

// isValidAddress performs basic validation of an email address.
// This is not a complete RFC 5321 validation but catches common errors.
func isValidAddress(addr string) bool {
	if addr == "" {
		return false
	}

	// Must have exactly one @
	atIdx := strings.LastIndex(addr, "@")
	if atIdx == -1 || atIdx == 0 || atIdx == len(addr)-1 {
		return false
	}

	localPart := addr[:atIdx]
	domain := addr[atIdx+1:]

	// Basic checks
	if localPart == "" || domain == "" {
		return false
	}

	// Domain must not start or end with dot or hyphen
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	if strings.HasPrefix(domain, "-") || strings.HasSuffix(domain, "-") {
		return false
	}

	// Domain must contain only valid characters
	for _, c := range domain {
		if !isValidDomainChar(c) {
			return false
		}
	}

	return true
}

// isValidDomainChar checks if a character is valid in a domain name.
func isValidDomainChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '.'
}

// ParseHeloHostname validates a HELO/EHLO hostname.
func ParseHeloHostname(arg string) (Hostname, error) {
	hostname := strings.TrimSpace(arg)
	if hostname == "" {
		return "", &ParseError{
			Err:     ErrMissingArgument,
			Context: "hostname required",
		}
	}

	// Check for address literal [x.x.x.x]
	if strings.HasPrefix(hostname, "[") {
		if !strings.HasSuffix(hostname, "]") {
			return "", &ParseError{
				Err:     ErrInvalidSyntax,
				Context: "unclosed address literal",
				Input:   hostname,
			}
		}
		// Accept address literals without further validation
		return hostname, nil
	}

	// Validate as hostname
	if !isValidHostname(hostname) {
		return "", &ParseError{
			Err:     ErrInvalidSyntax,
			Context: "invalid hostname",
			Input:   hostname,
		}
	}

	return hostname, nil
}

// isValidHostname checks if a string is a valid hostname.
func isValidHostname(s string) bool {
	if s == "" || len(s) > 255 {
		return false
	}

	labels := strings.Split(s, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return false
		}
		// Label must start and end with alphanumeric
		if !isAlphanumeric(rune(label[0])) || !isAlphanumeric(rune(label[len(label)-1])) {
			return false
		}
		// All characters must be alphanumeric or hyphen
		for _, c := range label {
			if !isAlphanumeric(c) && c != '-' {
				return false
			}
		}
	}

	return true
}

func isAlphanumeric(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

// DataLineReader provides methods for reading DATA content.
type DataLineReader struct {
	// MaxLineLength is the maximum line length.
	MaxLineLength LineLength
}

// NewDataLineReader creates a new data line reader.
func NewDataLineReader() *DataLineReader {
	return &DataLineReader{
		MaxLineLength: 998, // RFC 5321
	}
}

// IsTerminator checks if a line is the DATA terminator (single dot).
func (r *DataLineReader) IsTerminator(line []byte) bool {
	// Remove CRLF
	line = bytes.TrimSuffix(line, []byte("\r\n"))
	line = bytes.TrimSuffix(line, []byte("\n"))
	return len(line) == 1 && line[0] == '.'
}

// UnstuffLine removes dot-stuffing from a line.
// If the line starts with a dot, the first dot is removed.
func (r *DataLineReader) UnstuffLine(line []byte) []byte {
	if len(line) > 0 && line[0] == '.' {
		return line[1:]
	}
	return line
}

// StuffLine adds dot-stuffing to a line if necessary.
// If the line starts with a dot, a dot is prepended.
func (r *DataLineReader) StuffLine(line []byte) []byte {
	if len(line) > 0 && line[0] == '.' {
		result := make([]byte, len(line)+1)
		result[0] = '.'
		copy(result[1:], line)
		return result
	}
	return line
}
