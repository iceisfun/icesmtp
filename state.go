// Package icesmtp provides a pure Go SMTP protocol framework.
//
// icesmtp is a protocol engine, not a mail server. It provides SMTP correctness,
// explicit control, and testability over convenience.
package icesmtp

// State represents the current state of an SMTP session.
// The SMTP protocol is stateful; commands are only valid in certain states.
type State int

const (
	// StateDisconnected indicates no active connection.
	// This is the initial state before a connection is established.
	StateDisconnected State = iota

	// StateConnected indicates a TCP connection has been established
	// but the server has not yet sent its greeting.
	StateConnected

	// StateGreeted indicates the server has sent its 220 greeting.
	// The client must now send HELO or EHLO to identify itself.
	StateGreeted

	// StateIdentified indicates the client has successfully sent HELO or EHLO.
	// The client may now begin a mail transaction with MAIL FROM.
	StateIdentified

	// StateMailFrom indicates a MAIL FROM command has been accepted.
	// The client must now provide at least one RCPT TO.
	StateMailFrom

	// StateRcptTo indicates at least one RCPT TO has been accepted.
	// The client may add more recipients or proceed to DATA.
	StateRcptTo

	// StateData indicates the DATA command has been accepted.
	// The server is now receiving message content until <CRLF>.<CRLF>.
	StateData

	// StateDataDone indicates message data has been fully received.
	// The transaction is complete; the session returns to StateIdentified.
	StateDataDone

	// StateStartTLS indicates STARTTLS has been initiated.
	// TLS negotiation is in progress.
	StateStartTLS

	// StateTerminating indicates QUIT has been received.
	// The server will send a 221 response and close the connection.
	StateTerminating

	// StateTerminated indicates the session has ended.
	// No further commands will be processed.
	StateTerminated

	// StateAborted indicates the session was forcibly terminated
	// due to a policy violation, timeout, or error.
	StateAborted
)

// String returns the human-readable name of the state.
func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "Disconnected"
	case StateConnected:
		return "Connected"
	case StateGreeted:
		return "Greeted"
	case StateIdentified:
		return "Identified"
	case StateMailFrom:
		return "MailFrom"
	case StateRcptTo:
		return "RcptTo"
	case StateData:
		return "Data"
	case StateDataDone:
		return "DataDone"
	case StateStartTLS:
		return "StartTLS"
	case StateTerminating:
		return "Terminating"
	case StateTerminated:
		return "Terminated"
	case StateAborted:
		return "Aborted"
	default:
		return "Unknown"
	}
}

// IsTerminal returns true if this state represents a final state
// from which no further transitions are possible.
func (s State) IsTerminal() bool {
	return s == StateTerminated || s == StateAborted
}

// CanAcceptCommands returns true if this state can accept SMTP commands.
func (s State) CanAcceptCommands() bool {
	switch s {
	case StateGreeted, StateIdentified, StateMailFrom, StateRcptTo:
		return true
	default:
		return false
	}
}

// InTransaction returns true if the session is currently within a mail transaction.
// A mail transaction begins with MAIL FROM and ends with DATA completion or RSET.
func (s State) InTransaction() bool {
	return s == StateMailFrom || s == StateRcptTo || s == StateData
}

// StateTransition represents a transition from one state to another.
type StateTransition struct {
	From    State
	To      State
	Command CommandVerb
	Success bool
}

// StateTransitionError indicates an invalid state transition was attempted.
type StateTransitionError struct {
	Current   State
	Attempted State
	Command   CommandVerb
	Message   string
}

func (e *StateTransitionError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "invalid state transition from " + e.Current.String() + " to " + e.Attempted.String()
}
