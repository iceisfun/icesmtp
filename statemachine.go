package icesmtp

// StateMachine manages SMTP protocol state transitions.
// It enforces the valid command sequences defined by RFC 5321.
type StateMachine struct {
	state    State
	observer StateObserver
}

// StateObserver receives notifications of state transitions.
type StateObserver interface {
	// OnStateChange is called after a state transition.
	OnStateChange(transition StateTransition)
}

// NullStateObserver is a no-op StateObserver.
type NullStateObserver struct{}

func (NullStateObserver) OnStateChange(_ StateTransition) {}

// NewStateMachine creates a new state machine in the Disconnected state.
func NewStateMachine() *StateMachine {
	return &StateMachine{
		state:    StateDisconnected,
		observer: NullStateObserver{},
	}
}

// NewStateMachineWithObserver creates a state machine with an observer.
func NewStateMachineWithObserver(observer StateObserver) *StateMachine {
	return &StateMachine{
		state:    StateDisconnected,
		observer: observer,
	}
}

// State returns the current state.
func (sm *StateMachine) State() State {
	return sm.state
}

// SetObserver sets the state observer.
func (sm *StateMachine) SetObserver(observer StateObserver) {
	if observer == nil {
		observer = NullStateObserver{}
	}
	sm.observer = observer
}

// Transition attempts to transition to a new state.
// Returns an error if the transition is invalid.
func (sm *StateMachine) Transition(newState State) error {
	if !sm.canTransition(newState) {
		return &StateTransitionError{
			Current:   sm.state,
			Attempted: newState,
		}
	}

	transition := StateTransition{
		From:    sm.state,
		To:      newState,
		Success: true,
	}

	sm.state = newState
	sm.observer.OnStateChange(transition)

	return nil
}

// TransitionForCommand attempts to transition based on a successful command.
// Returns the new state and any error.
func (sm *StateMachine) TransitionForCommand(cmd CommandVerb, success bool) (State, error) {
	if !success {
		// Failed commands don't change state
		return sm.state, nil
	}

	newState := sm.nextStateForCommand(cmd)
	if newState == sm.state {
		return sm.state, nil
	}

	if err := sm.Transition(newState); err != nil {
		return sm.state, err
	}

	return sm.state, nil
}

// canTransition checks if a transition to the new state is valid.
func (sm *StateMachine) canTransition(newState State) bool {
	transitions := validTransitions[sm.state]
	for _, valid := range transitions {
		if valid == newState {
			return true
		}
	}
	return false
}

// nextStateForCommand returns the state after a successful command.
func (sm *StateMachine) nextStateForCommand(cmd CommandVerb) State {
	switch cmd {
	case CmdHELO, CmdEHLO:
		return StateIdentified
	case CmdMAIL:
		return StateMailFrom
	case CmdRCPT:
		return StateRcptTo
	case CmdDATA:
		return StateData
	case CmdRSET:
		if sm.state.InTransaction() {
			return StateIdentified
		}
		return sm.state
	case CmdQUIT:
		return StateTerminating
	case CmdSTARTTLS:
		return StateStartTLS
	default:
		// Other commands don't change state
		return sm.state
	}
}

// Reset resets the state machine to handle a new transaction.
// This is used after RSET or DATA completion.
func (sm *StateMachine) Reset() {
	if sm.state.InTransaction() || sm.state == StateDataDone {
		sm.Transition(StateIdentified)
	}
}

// Connect transitions from Disconnected to Connected.
func (sm *StateMachine) Connect() error {
	if sm.state != StateDisconnected {
		return &StateTransitionError{
			Current:   sm.state,
			Attempted: StateConnected,
			Message:   "already connected",
		}
	}
	return sm.Transition(StateConnected)
}

// Greet transitions from Connected to Greeted.
func (sm *StateMachine) Greet() error {
	if sm.state != StateConnected {
		return &StateTransitionError{
			Current:   sm.state,
			Attempted: StateGreeted,
			Message:   "not in connected state",
		}
	}
	return sm.Transition(StateGreeted)
}

// DataComplete transitions from Data to DataDone.
func (sm *StateMachine) DataComplete() error {
	if sm.state != StateData {
		return &StateTransitionError{
			Current:   sm.state,
			Attempted: StateDataDone,
			Message:   "not in data state",
		}
	}
	return sm.Transition(StateDataDone)
}

// TLSComplete transitions from StartTLS back to Greeted.
// After TLS upgrade, the client must re-identify with EHLO.
func (sm *StateMachine) TLSComplete() error {
	if sm.state != StateStartTLS {
		return &StateTransitionError{
			Current:   sm.state,
			Attempted: StateGreeted,
			Message:   "not in STARTTLS state",
		}
	}
	return sm.Transition(StateGreeted)
}

// Terminate transitions to the Terminated state.
func (sm *StateMachine) Terminate() error {
	return sm.Transition(StateTerminated)
}

// Abort transitions to the Aborted state.
func (sm *StateMachine) Abort() error {
	return sm.Transition(StateAborted)
}

// IsCommandAllowed checks if a command is valid in the current state.
func (sm *StateMachine) IsCommandAllowed(cmd CommandVerb) bool {
	return IsCommandAllowed(sm.state, cmd)
}

// validTransitions defines all valid state transitions.
var validTransitions = map[State][]State{
	StateDisconnected: {StateConnected},
	StateConnected:    {StateGreeted, StateTerminated, StateAborted},
	StateGreeted:      {StateIdentified, StateTerminating, StateAborted},
	StateIdentified:   {StateIdentified, StateMailFrom, StateStartTLS, StateTerminating, StateAborted},
	StateMailFrom:     {StateRcptTo, StateIdentified, StateTerminating, StateAborted},
	StateRcptTo:       {StateRcptTo, StateData, StateIdentified, StateTerminating, StateAborted},
	StateData:         {StateDataDone, StateAborted},
	StateDataDone:     {StateIdentified, StateTerminating, StateAborted},
	StateStartTLS:     {StateGreeted, StateAborted},
	StateTerminating:  {StateTerminated},
	StateTerminated:   {},
	StateAborted:      {},
}

// CommandStateRequirements defines the required state for each command.
var CommandStateRequirements = map[CommandVerb][]State{
	CmdHELO:     {StateGreeted, StateIdentified},
	CmdEHLO:     {StateGreeted, StateIdentified},
	CmdMAIL:     {StateIdentified},
	CmdRCPT:     {StateMailFrom, StateRcptTo},
	CmdDATA:     {StateRcptTo},
	CmdRSET:     {StateGreeted, StateIdentified, StateMailFrom, StateRcptTo},
	CmdNOOP:     {StateGreeted, StateIdentified, StateMailFrom, StateRcptTo},
	CmdQUIT:     {StateGreeted, StateIdentified, StateMailFrom, StateRcptTo},
	CmdVRFY:     {StateIdentified},
	CmdEXPN:     {StateIdentified},
	CmdHELP:     {StateGreeted, StateIdentified, StateMailFrom, StateRcptTo},
	CmdSTARTTLS: {StateIdentified},
	CmdAUTH:     {StateIdentified},
}

// IsStateValidForCommand checks if the state is valid for the command.
func IsStateValidForCommand(state State, cmd CommandVerb) bool {
	validStates, exists := CommandStateRequirements[cmd]
	if !exists {
		return false
	}
	for _, s := range validStates {
		if s == state {
			return true
		}
	}
	return false
}

// StateError returns the appropriate error response for a command in the wrong state.
func StateError(cmd CommandVerb, currentState State) Response {
	return NewResponse(Reply503BadSequence, "Bad sequence of commands")
}
