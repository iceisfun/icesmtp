package icesmtp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Engine is the core SMTP protocol engine.
// It handles a single SMTP session over a Conn.
type Engine struct {
	config SessionConfig
	conn   *BufferedConn
	parser *Parser
	sm     *StateMachine
	state  *SessionState
	stats  SessionStats
	logger Logger

	// Session identification
	sessionID  SessionID
	clientIP   IPAddress
	clientAddr RemoteAddress

	// Current envelope being built
	envelope EnvelopeBuilder

	// Synchronization
	mu     sync.Mutex
	closed bool
}

// EngineOption configures an Engine.
type EngineOption func(*Engine)

// WithClientIP sets the client IP address.
func WithClientIP(ip IPAddress) EngineOption {
	return func(e *Engine) {
		e.clientIP = ip
	}
}

// WithClientAddr sets the client address.
func WithClientAddr(addr RemoteAddress) EngineOption {
	return func(e *Engine) {
		e.clientAddr = addr
	}
}

// WithSessionID sets a specific session ID.
func WithSessionID(id SessionID) EngineOption {
	return func(e *Engine) {
		e.sessionID = id
	}
}

// NewEngine creates a new SMTP engine from io.Reader/io.Writer.
// If the reader implements net.Conn (e.g., when passing a net.Conn as both
// reader and writer), it will be wrapped with NetConn for full timeout and
// TLS support. Otherwise, a PipeConn is used.
func NewEngine(r io.Reader, w io.Writer, config SessionConfig, opts ...EngineOption) *Engine {
	// Check if reader is a net.Conn - this handles the common case of
	// NewEngine(conn, conn, config) where conn is a net.Conn
	if netConn, ok := r.(net.Conn); ok {
		return NewEngineFromNetConn(netConn, config, opts...)
	}
	conn := WrapPipe(r, w)
	return NewEngineWithConn(conn, config, opts...)
}

// NewEngineFromNetConn creates a new SMTP engine from a net.Conn.
// This provides full timeout and TLS support.
func NewEngineFromNetConn(netConn net.Conn, config SessionConfig, opts ...EngineOption) *Engine {
	conn := WrapNetConn(netConn)
	return NewEngineWithConn(conn, config, opts...)
}

// NewEngineWithConn creates a new SMTP engine with a Conn.
func NewEngineWithConn(conn Conn, config SessionConfig, opts ...EngineOption) *Engine {
	e := &Engine{
		config:    config,
		conn:      NewBufferedConn(conn),
		parser:    NewParser(),
		sm:        NewStateMachine(),
		state:     &SessionState{State: StateDisconnected},
		stats:     SessionStats{StartTime: time.Now()},
		sessionID: generateSessionID(),
	}

	if config.Logger != nil {
		e.logger = config.Logger.WithSession(e.sessionID)
	} else {
		e.logger = NullLogger{}
	}

	e.parser.MaxCommandLength = config.Limits.MaxCommandLength
	if e.parser.MaxCommandLength == 0 {
		e.parser.MaxCommandLength = 512
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// generateSessionID creates a unique session identifier.
func generateSessionID() SessionID {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Run executes the SMTP session.
func (e *Engine) Run(ctx context.Context) error {
	// Connect and send greeting
	if err := e.sm.Connect(); err != nil {
		return err
	}

	// Call connect hook
	if e.config.Hooks != nil {
		e.config.Hooks.OnConnect(ctx, e)
	}

	// Send greeting
	greeting := e.buildGreeting()
	if err := e.writeResponse(ctx, greeting); err != nil {
		return e.handleDisconnect(ctx, DisconnectError, err)
	}

	if err := e.sm.Greet(); err != nil {
		return err
	}
	e.state.State = StateGreeted

	e.logger.Info(ctx, "session started",
		Attr(AttrClientIP, e.clientIP))

	// Main command loop
	for {
		select {
		case <-ctx.Done():
			return e.handleDisconnect(ctx, DisconnectTimeout, ctx.Err())
		default:
		}

		// Check if we're in a terminal state
		if e.sm.State().IsTerminal() {
			break
		}

		// Check if we need to perform TLS upgrade
		if e.state.State == StateStartTLS {
			if err := e.performTLSUpgrade(ctx); err != nil {
				e.logger.Error(ctx, "TLS upgrade failed", Attr(AttrError, err))
				return e.handleDisconnect(ctx, DisconnectTLSFailure, err)
			}
			continue
		}

		// Read and process command with timeout
		if err := e.processOneCommand(ctx); err != nil {
			if e.sm.State().IsTerminal() {
				break
			}

			// Check for timeout
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrDeadlineExceeded) || isTimeoutError(err) {
				return e.handleDisconnect(ctx, DisconnectTimeout, err)
			}

			// Check if this is a protocol error vs. I/O error
			if isIOError(err) {
				return e.handleDisconnect(ctx, DisconnectError, err)
			}
			// Protocol errors are handled, continue
		}
	}

	return e.handleDisconnect(ctx, DisconnectNormal, nil)
}

// processOneCommand reads and processes a single SMTP command.
func (e *Engine) processOneCommand(ctx context.Context) error {
	// Determine timeout to use
	timeout := e.config.Limits.CommandTimeout
	if timeout == 0 {
		timeout = e.config.Limits.IdleTimeout
	}

	// Read command line with timeout
	line, err := e.readLine(ctx, timeout)
	if err != nil {
		return err
	}

	e.stats.CommandCount++

	// Parse command
	cmd, err := e.parser.ParseCommand(line)
	if err != nil {
		e.state.ConsecutiveErrors++
		if checkErr := e.checkErrorLimit(); checkErr != nil {
			e.writeResponse(ctx, NewResponse(Reply421ServiceNotAvailable, "Too many errors, closing connection"))
			e.sm.Abort()
			return checkErr
		}
		e.writeResponse(ctx, ResponseSyntaxError)
		return err
	}

	e.logger.Debug(ctx, "received command",
		Attr(AttrCommand, cmd.Verb.String()),
		Attr(AttrState, e.sm.State().String()))

	// Call command hook
	if e.config.Hooks != nil {
		if err := e.config.Hooks.OnCommand(ctx, *cmd, e); err != nil {
			e.writeResponse(ctx, ResponseTransactionFailed)
			return err
		}
	}

	// Check if command is allowed in current state
	if !e.sm.IsCommandAllowed(cmd.Verb) {
		e.state.ConsecutiveErrors++
		e.writeResponse(ctx, ResponseBadSequence)
		return nil
	}

	// Handle the command
	response := e.handleCommand(ctx, cmd)

	// Write response
	if err := e.writeResponse(ctx, response); err != nil {
		return err
	}

	// Reset error count on successful command
	if response.Code.IsPositive() {
		e.state.ConsecutiveErrors = 0
	}

	return nil
}

// handleCommand processes a command and returns the response.
func (e *Engine) handleCommand(ctx context.Context, cmd *Command) Response {
	switch cmd.Verb {
	case CmdHELO:
		return e.handleHELO(ctx, cmd)
	case CmdEHLO:
		return e.handleEHLO(ctx, cmd)
	case CmdMAIL:
		return e.handleMAIL(ctx, cmd)
	case CmdRCPT:
		return e.handleRCPT(ctx, cmd)
	case CmdDATA:
		return e.handleDATA(ctx, cmd)
	case CmdRSET:
		return e.handleRSET(ctx, cmd)
	case CmdNOOP:
		return e.handleNOOP(ctx, cmd)
	case CmdQUIT:
		return e.handleQUIT(ctx, cmd)
	case CmdVRFY:
		return e.handleVRFY(ctx, cmd)
	case CmdHELP:
		return e.handleHELP(ctx, cmd)
	case CmdSTARTTLS:
		return e.handleSTARTTLS(ctx, cmd)
	default:
		return ResponseCommandNotImplemented
	}
}

func (e *Engine) handleHELO(ctx context.Context, cmd *Command) Response {
	hostname, err := ParseHeloHostname(cmd.Argument)
	if err != nil {
		return ResponseSyntaxErrorParams
	}

	e.state.ClientHostname = hostname
	e.sm.TransitionForCommand(CmdHELO, true)
	e.state.State = StateIdentified

	// Reset any existing transaction
	e.resetTransaction()

	return NewResponse(Reply250OK, fmt.Sprintf("%s Hello %s", e.config.ServerHostname, hostname))
}

func (e *Engine) handleEHLO(ctx context.Context, cmd *Command) Response {
	hostname, err := ParseHeloHostname(cmd.Argument)
	if err != nil {
		return ResponseSyntaxErrorParams
	}

	e.state.ClientHostname = hostname
	e.sm.TransitionForCommand(CmdEHLO, true)
	e.state.State = StateIdentified

	// Reset any existing transaction
	e.resetTransaction()

	// Build EHLO response with extensions
	lines := []string{fmt.Sprintf("%s Hello %s", e.config.ServerHostname, hostname)}

	ext := e.config.Extensions
	if ext.SIZE && e.config.Limits.MaxMessageSize > 0 {
		lines = append(lines, fmt.Sprintf("SIZE %d", e.config.Limits.MaxMessageSize))
	}
	if ext.STARTTLS && e.config.TLSPolicy != TLSDisabled && !e.state.TLSActive {
		lines = append(lines, "STARTTLS")
	}
	if ext.EightBitMIME {
		lines = append(lines, "8BITMIME")
	}
	if ext.PIPELINING {
		lines = append(lines, "PIPELINING")
	}
	if ext.ENHANCEDSTATUSCODES {
		lines = append(lines, "ENHANCEDSTATUSCODES")
	}
	if ext.SMTPUTF8 {
		lines = append(lines, "SMTPUTF8")
	}
	if ext.HELP {
		lines = append(lines, "HELP")
	}

	return NewMultilineResponse(Reply250OK, lines...)
}

func (e *Engine) handleMAIL(ctx context.Context, cmd *Command) Response {
	// Check TLS requirement
	if e.config.TLSPolicy == TLSRequired && !e.state.TLSActive {
		return NewResponse(Reply530AuthRequired, "Must issue STARTTLS first")
	}

	// Check transaction limit
	if e.config.Limits.MaxTransactions > 0 && e.stats.TransactionCount >= e.config.Limits.MaxTransactions {
		return NewResponse(Reply421ServiceNotAvailable, "Too many transactions")
	}

	// Parse the mail path
	path, err := ParseMailPath(cmd.Argument, "FROM")
	if err != nil {
		return ResponseSyntaxErrorParams
	}

	// Check SIZE parameter
	if e.config.Extensions.SIZE && e.config.Limits.MaxMessageSize > 0 {
		if sizeStr, ok := cmd.Params["SIZE"]; ok {
			var size int64
			fmt.Sscanf(sizeStr, "%d", &size)
			if size > e.config.Limits.MaxMessageSize {
				return NewResponse(Reply552ExceededStorage, "Message size exceeds fixed maximum message size")
			}
		}
	}

	// Validate sender if policy is configured
	if e.config.SenderPolicy != nil {
		result := e.config.SenderPolicy.ValidateSender(ctx, *path, e)
		if !result.Accepted {
			return result.Response
		}
	}

	// Create new envelope
	metadata := EnvelopeMetadata{
		SessionID:         e.sessionID,
		ClientHostname:    e.state.ClientHostname,
		ClientIP:          e.clientIP,
		ServerHostname:    e.config.ServerHostname,
		TLSActive:         e.state.TLSActive,
		AuthenticatedUser: e.state.AuthenticatedUser,
	}
	if e.state.TLSState != nil {
		metadata.TLSVersion = e.state.TLSState.VersionString()
		metadata.TLSCipherSuite = e.state.TLSState.CipherSuiteString()
	}

	if e.config.EnvelopeFactory != nil {
		e.envelope = e.config.EnvelopeFactory.NewBuilder(metadata)
	} else {
		e.envelope = NewStandardEnvelopeBuilder(metadata)
	}

	if err := e.envelope.SetMailFrom(*path, cmd.Params); err != nil {
		return ResponseTransactionFailed
	}

	e.sm.TransitionForCommand(CmdMAIL, true)
	e.state.State = StateMailFrom

	if e.config.Hooks != nil {
		e.config.Hooks.OnMailFrom(ctx, *path, e)
	}

	e.logger.Info(ctx, "mail from accepted",
		Attr(AttrMailFrom, path.Address))

	return ResponseOK
}

func (e *Engine) handleRCPT(ctx context.Context, cmd *Command) Response {
	// Parse the recipient path
	path, err := ParseMailPath(cmd.Argument, "TO")
	if err != nil {
		return ResponseSyntaxErrorParams
	}

	// Check recipient limit
	if e.config.Limits.MaxRecipients > 0 {
		if e.envelope.Build().RecipientCount() >= e.config.Limits.MaxRecipients {
			return NewResponse(Reply452InsufficientStorage, "Too many recipients")
		}
	}

	// Validate recipient
	result := e.config.Mailbox.ValidateRecipient(ctx, *path, e)
	if result.Status != RecipientAccepted {
		return result.Response
	}

	// Add recipient to envelope
	if err := e.envelope.AddRecipient(*path); err != nil {
		return ResponseTransactionFailed
	}

	e.sm.TransitionForCommand(CmdRCPT, true)
	e.state.State = StateRcptTo

	if e.config.Hooks != nil {
		e.config.Hooks.OnRcptTo(ctx, *path, e)
	}

	e.logger.Info(ctx, "recipient accepted",
		Attr(AttrRcptTo, path.Address))

	return ResponseOK
}

func (e *Engine) handleDATA(ctx context.Context, cmd *Command) Response {
	// Transition to DATA state
	e.sm.TransitionForCommand(CmdDATA, true)
	e.state.State = StateData

	if e.config.Hooks != nil {
		e.config.Hooks.OnDataStart(ctx, e)
	}

	// Send intermediate response
	if err := e.writeResponse(ctx, ResponseStartMailInput); err != nil {
		e.sm.Abort()
		return Response{} // Already sent, error handled
	}

	// Determine data timeout
	dataTimeout := e.config.Limits.DataTimeout
	if dataTimeout == 0 {
		dataTimeout = 10 * time.Minute // Default
	}

	// Read message data with timeout
	data, err := e.readData(ctx, dataTimeout)
	if err != nil {
		e.logger.Error(ctx, "error receiving message data", Attr(AttrError, err))
		e.sm.Reset()
		e.state.State = StateIdentified
		e.envelope = nil

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrDeadlineExceeded) || isTimeoutError(err) {
			return NewResponse(Reply451LocalError, "Timeout receiving message data")
		}
		return NewResponse(Reply451LocalError, "Error receiving message data")
	}

	// Check message size
	if e.config.Limits.MaxMessageSize > 0 && int64(len(data)) > e.config.Limits.MaxMessageSize {
		e.sm.Reset()
		e.state.State = StateIdentified
		e.envelope = nil
		return NewResponse(Reply552ExceededStorage, "Message size exceeds limit")
	}

	// Write data to envelope - CHECK ERRORS
	writer, err := e.envelope.DataWriter()
	if err != nil {
		e.logger.Error(ctx, "failed to get data writer", Attr(AttrError, err))
		e.sm.Reset()
		e.state.State = StateIdentified
		e.envelope = nil
		return NewResponse(Reply451LocalError, "Unable to accept message")
	}

	n, err := writer.Write(data)
	if err != nil {
		e.logger.Error(ctx, "failed to write message data", Attr(AttrError, err))
		writer.Close()
		e.sm.Reset()
		e.state.State = StateIdentified
		e.envelope = nil
		return NewResponse(Reply451LocalError, "Error writing message data")
	}
	if n != len(data) {
		e.logger.Error(ctx, "short write to message data",
			Attr("expected", len(data)),
			Attr("wrote", n))
		writer.Close()
		e.sm.Reset()
		e.state.State = StateIdentified
		e.envelope = nil
		return NewResponse(Reply451LocalError, "Error writing message data")
	}

	if err := writer.Close(); err != nil {
		e.logger.Error(ctx, "failed to close data writer", Attr(AttrError, err))
		e.sm.Reset()
		e.state.State = StateIdentified
		e.envelope = nil
		return NewResponse(Reply451LocalError, "Error finalizing message data")
	}

	// Finalize envelope
	envelope, err := e.envelope.Finalize()
	if err != nil {
		e.logger.Error(ctx, "failed to finalize envelope", Attr(AttrError, err))
		e.sm.Reset()
		e.state.State = StateIdentified
		e.envelope = nil
		return NewResponse(Reply451LocalError, "Unable to finalize message")
	}

	// Store message
	if e.config.Storage != nil {
		receipt, err := e.config.Storage.Store(ctx, envelope)
		if err != nil {
			e.sm.Reset()
			e.state.State = StateIdentified
			e.envelope = nil
			e.logger.Error(ctx, "storage error", Attr(AttrError, err))
			return NewResponse(Reply451LocalError, "Unable to store message")
		}
		e.logger.Debug(ctx, "message stored",
			Attr("storage_id", receipt.MessageID),
			Attr("bytes_written", receipt.BytesWritten))
	}

	// Update stats
	e.stats.MessageCount++
	e.stats.TransactionCount++
	e.stats.RecipientCount += envelope.RecipientCount()

	e.sm.DataComplete()
	e.sm.Reset()
	e.state.State = StateIdentified
	e.envelope = nil

	if e.config.Hooks != nil {
		e.config.Hooks.OnDataEnd(ctx, envelope, e)
	}

	e.logger.Info(ctx, "message received",
		Attr(AttrEnvelopeID, envelope.ID()),
		Attr(AttrMessageSize, envelope.DataSize()),
		Attr(AttrRecipients, envelope.RecipientCount()))

	return NewResponse(Reply250OK, fmt.Sprintf("OK, message %s accepted", envelope.ID()))
}

func (e *Engine) handleRSET(ctx context.Context, cmd *Command) Response {
	e.resetTransaction()
	e.sm.Reset()
	if e.sm.State() == StateGreeted || e.sm.State() == StateIdentified {
		e.state.State = e.sm.State()
	} else {
		e.state.State = StateIdentified
	}

	return ResponseOK
}

func (e *Engine) handleNOOP(ctx context.Context, cmd *Command) Response {
	return ResponseOK
}

func (e *Engine) handleQUIT(ctx context.Context, cmd *Command) Response {
	e.sm.TransitionForCommand(CmdQUIT, true)
	e.sm.Terminate()
	return ResponseBye
}

func (e *Engine) handleVRFY(ctx context.Context, cmd *Command) Response {
	if !e.config.Extensions.VRFY {
		return ResponseCommandNotImplemented
	}

	// VRFY is often disabled for security reasons
	return NewResponse(Reply252CannotVRFY, "Cannot VRFY user; try RCPT to attempt delivery")
}

func (e *Engine) handleHELP(ctx context.Context, cmd *Command) Response {
	if !e.config.Extensions.HELP {
		return ResponseCommandNotImplemented
	}

	// Build command list dynamically based on what's actually available
	basicCmds := "HELO EHLO MAIL RCPT DATA"
	additionalCmds := "RSET NOOP QUIT HELP"

	// Only include STARTTLS if it's actually available
	// (same conditions as EHLO advertisement)
	if e.config.Extensions.STARTTLS && e.config.TLSPolicy != TLSDisabled && e.config.TLSProvider != nil && !e.state.TLSActive {
		additionalCmds += " STARTTLS"
	}

	return NewMultilineResponse(Reply214HelpMessage,
		"Supported commands:",
		basicCmds,
		additionalCmds,
		"For more information, consult RFC 5321",
	)
}

func (e *Engine) handleSTARTTLS(ctx context.Context, cmd *Command) Response {
	if e.config.TLSPolicy == TLSDisabled {
		return ResponseCommandNotImplemented
	}

	if e.state.TLSActive {
		return NewResponse(Reply503BadSequence, "TLS already active")
	}

	if e.config.TLSProvider == nil {
		return NewResponse(Reply454TLSNotAvailable, "TLS not available")
	}

	// Transition to STARTTLS state - actual upgrade happens in main loop
	e.sm.TransitionForCommand(CmdSTARTTLS, true)
	e.state.State = StateStartTLS

	return NewResponse(Reply220ServiceReady, "Ready to start TLS")
}

// performTLSUpgrade performs the TLS handshake after STARTTLS response is sent.
func (e *Engine) performTLSUpgrade(ctx context.Context) error {
	// Get TLS config from provider
	tlsConfig, err := e.config.TLSProvider.GetConfig(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to get TLS config: %w", err)
	}

	// Determine handshake timeout - use CommandTimeout or a reasonable default
	handshakeTimeout := e.config.Limits.CommandTimeout
	if handshakeTimeout == 0 {
		handshakeTimeout = 30 * time.Second // Default TLS handshake timeout
	}

	// Set deadline for the handshake to prevent DoS attacks where client
	// sends STARTTLS but never completes the handshake
	deadline := time.Now().Add(handshakeTimeout)
	e.conn.SetReadDeadline(deadline)
	e.conn.SetWriteDeadline(deadline)

	// Perform the TLS handshake
	tlsState, err := e.conn.UpgradeTLS(tlsConfig)

	// Clear deadlines regardless of success/failure
	e.conn.SetReadDeadline(time.Time{})
	e.conn.SetWriteDeadline(time.Time{})

	if err != nil {
		return fmt.Errorf("TLS handshake failed: %w", err)
	}

	// Update state
	e.state.TLSActive = true
	e.state.TLSState = &tlsState

	// Reset the buffered reader since the underlying connection changed
	e.conn.ResetReader()

	// Per RFC 3207, after STARTTLS the session returns to initial state
	// Client must send EHLO again
	e.sm.TLSComplete()
	e.state.State = StateGreeted

	// Reset any transaction state
	e.resetTransaction()
	e.state.ClientHostname = ""

	if e.config.Hooks != nil {
		e.config.Hooks.OnTLSUpgrade(ctx, tlsState, e)
	}

	e.logger.Info(ctx, "TLS upgraded",
		Attr(AttrTLSVersion, tlsState.VersionString()),
		Attr(AttrCipherSuite, tlsState.CipherSuiteString()))

	return nil
}

// readLine reads a line from the client with timeout.
func (e *Engine) readLine(ctx context.Context, timeout time.Duration) ([]byte, error) {
	line, err := e.conn.ReadLine(timeout)
	if err != nil {
		return nil, err
	}
	e.stats.BytesRead += int64(len(line))
	return line, nil
}

// readData reads message data until the terminator with timeout.
func (e *Engine) readData(ctx context.Context, timeout time.Duration) ([]byte, error) {
	var buf bytes.Buffer
	reader := NewDataLineReader()

	// Set initial deadline
	deadline := time.Now().Add(timeout)
	e.conn.SetReadDeadline(deadline)
	defer e.conn.SetReadDeadline(time.Time{}) // Clear deadline when done

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line, err := e.conn.Reader().ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		e.stats.BytesRead += int64(len(line))

		// Extend deadline after each line received (reset timeout)
		deadline = time.Now().Add(timeout)
		e.conn.SetReadDeadline(deadline)

		// Check for terminator
		if reader.IsTerminator(line) {
			break
		}

		// Check line length
		if e.config.Limits.MaxLineLength > 0 && len(line) > e.config.Limits.MaxLineLength {
			return nil, ErrLineTooLong
		}

		// Check total size
		if e.config.Limits.MaxMessageSize > 0 && int64(buf.Len()+len(line)) > e.config.Limits.MaxMessageSize {
			return nil, ErrMessageTooLarge
		}

		// Unstuff and write
		buf.Write(reader.UnstuffLine(line))
	}

	return buf.Bytes(), nil
}

// writeResponse writes an SMTP response.
func (e *Engine) writeResponse(ctx context.Context, resp Response) error {
	data := resp.Bytes()
	n, err := e.conn.Write(data)
	e.stats.BytesWritten += int64(n)

	e.logger.Debug(ctx, "sent response",
		Attr(AttrReplyCode, int(resp.Code)))

	return err
}

// resetTransaction resets the current mail transaction.
func (e *Engine) resetTransaction() {
	if e.envelope != nil {
		e.envelope.Reset()
		e.envelope = nil
	}
}

// checkErrorLimit checks if the error limit has been exceeded.
func (e *Engine) checkErrorLimit() error {
	checker := &StandardLimitChecker{Limits: e.config.Limits}
	return checker.CheckErrorCount(e.state.ConsecutiveErrors)
}

// handleDisconnect handles session termination.
func (e *Engine) handleDisconnect(ctx context.Context, reason DisconnectReason, err error) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()

	e.stats.EndTime = time.Now()

	if e.config.Hooks != nil {
		e.config.Hooks.OnDisconnect(ctx, e, reason)
	}

	e.logger.Info(ctx, "session ended",
		Attr("reason", reason.String()),
		Attr("commands", e.stats.CommandCount),
		Attr("messages", e.stats.MessageCount))

	return err
}

// buildGreeting builds the initial server greeting.
func (e *Engine) buildGreeting() Response {
	return NewResponse(Reply220ServiceReady, fmt.Sprintf("%s ESMTP icesmtp", e.config.ServerHostname))
}

// isIOError checks if an error is an I/O error.
func isIOError(err error) bool {
	return err == io.EOF || err == io.ErrUnexpectedEOF || err == io.ErrClosedPipe
}

// isTimeoutError checks if an error is a timeout error.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// SessionInfo interface implementation

func (e *Engine) ID() SessionID               { return e.sessionID }
func (e *Engine) State() State                { return e.state.State }
func (e *Engine) ClientHostname() Hostname    { return e.state.ClientHostname }
func (e *Engine) ClientIP() IPAddress         { return e.clientIP }
func (e *Engine) TLSActive() bool             { return e.state.TLSActive }
func (e *Engine) Authenticated() bool         { return e.state.Authenticated }
func (e *Engine) AuthenticatedUser() Username { return e.state.AuthenticatedUser }
func (e *Engine) CurrentRecipientCount() RecipientCount {
	if e.envelope == nil {
		return 0
	}
	return e.envelope.Build().RecipientCount()
}
func (e *Engine) CurrentMailFrom() *MailPath {
	if e.envelope == nil {
		return nil
	}
	env := e.envelope.Build()
	from := env.MailFrom()
	return &from
}

// Close terminates the session.
func (e *Engine) Close() error {
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	e.sm.Abort()
	return e.conn.Close()
}

// Reply code for TLS not available.
const Reply454TLSNotAvailable ReplyCode = 454

// Reply code for authentication required.
const Reply530AuthRequired ReplyCode = 530
