package icesmtp

import (
	"testing"
)

func TestStateMachine_InitialState(t *testing.T) {
	sm := NewStateMachine()
	if sm.State() != StateDisconnected {
		t.Errorf("expected initial state Disconnected, got %v", sm.State())
	}
}

func TestStateMachine_Connect(t *testing.T) {
	sm := NewStateMachine()

	if err := sm.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if sm.State() != StateConnected {
		t.Errorf("expected state Connected, got %v", sm.State())
	}
}

func TestStateMachine_ConnectTwice(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()

	err := sm.Connect()
	if err == nil {
		t.Error("expected error on second Connect")
	}
}

func TestStateMachine_Greet(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()

	if err := sm.Greet(); err != nil {
		t.Fatalf("Greet failed: %v", err)
	}

	if sm.State() != StateGreeted {
		t.Errorf("expected state Greeted, got %v", sm.State())
	}
}

func TestStateMachine_GreetWithoutConnect(t *testing.T) {
	sm := NewStateMachine()

	err := sm.Greet()
	if err == nil {
		t.Error("expected error on Greet without Connect")
	}
}

func TestStateMachine_HELOTransition(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()

	newState, err := sm.TransitionForCommand(CmdHELO, true)
	if err != nil {
		t.Fatalf("HELO transition failed: %v", err)
	}

	if newState != StateIdentified {
		t.Errorf("expected state Identified, got %v", newState)
	}
}

func TestStateMachine_EHLOTransition(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()

	newState, err := sm.TransitionForCommand(CmdEHLO, true)
	if err != nil {
		t.Fatalf("EHLO transition failed: %v", err)
	}

	if newState != StateIdentified {
		t.Errorf("expected state Identified, got %v", newState)
	}
}

func TestStateMachine_MAILTransition(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()
	sm.TransitionForCommand(CmdEHLO, true)

	newState, err := sm.TransitionForCommand(CmdMAIL, true)
	if err != nil {
		t.Fatalf("MAIL transition failed: %v", err)
	}

	if newState != StateMailFrom {
		t.Errorf("expected state MailFrom, got %v", newState)
	}
}

func TestStateMachine_RCPTTransition(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()
	sm.TransitionForCommand(CmdEHLO, true)
	sm.TransitionForCommand(CmdMAIL, true)

	newState, err := sm.TransitionForCommand(CmdRCPT, true)
	if err != nil {
		t.Fatalf("RCPT transition failed: %v", err)
	}

	if newState != StateRcptTo {
		t.Errorf("expected state RcptTo, got %v", newState)
	}
}

func TestStateMachine_MultipleRCPT(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()
	sm.TransitionForCommand(CmdEHLO, true)
	sm.TransitionForCommand(CmdMAIL, true)
	sm.TransitionForCommand(CmdRCPT, true)

	// Second RCPT should stay in RcptTo
	newState, err := sm.TransitionForCommand(CmdRCPT, true)
	if err != nil {
		t.Fatalf("second RCPT transition failed: %v", err)
	}

	if newState != StateRcptTo {
		t.Errorf("expected state RcptTo, got %v", newState)
	}
}

func TestStateMachine_DATATransition(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()
	sm.TransitionForCommand(CmdEHLO, true)
	sm.TransitionForCommand(CmdMAIL, true)
	sm.TransitionForCommand(CmdRCPT, true)

	newState, err := sm.TransitionForCommand(CmdDATA, true)
	if err != nil {
		t.Fatalf("DATA transition failed: %v", err)
	}

	if newState != StateData {
		t.Errorf("expected state Data, got %v", newState)
	}
}

func TestStateMachine_DataComplete(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()
	sm.TransitionForCommand(CmdEHLO, true)
	sm.TransitionForCommand(CmdMAIL, true)
	sm.TransitionForCommand(CmdRCPT, true)
	sm.TransitionForCommand(CmdDATA, true)

	if err := sm.DataComplete(); err != nil {
		t.Fatalf("DataComplete failed: %v", err)
	}

	if sm.State() != StateDataDone {
		t.Errorf("expected state DataDone, got %v", sm.State())
	}
}

func TestStateMachine_RSETFromMailFrom(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()
	sm.TransitionForCommand(CmdEHLO, true)
	sm.TransitionForCommand(CmdMAIL, true)

	sm.Reset()

	if sm.State() != StateIdentified {
		t.Errorf("expected state Identified after RSET, got %v", sm.State())
	}
}

func TestStateMachine_RSETFromRcptTo(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()
	sm.TransitionForCommand(CmdEHLO, true)
	sm.TransitionForCommand(CmdMAIL, true)
	sm.TransitionForCommand(CmdRCPT, true)

	sm.Reset()

	if sm.State() != StateIdentified {
		t.Errorf("expected state Identified after RSET, got %v", sm.State())
	}
}

func TestStateMachine_QUITTransition(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()
	sm.TransitionForCommand(CmdEHLO, true)

	newState, err := sm.TransitionForCommand(CmdQUIT, true)
	if err != nil {
		t.Fatalf("QUIT transition failed: %v", err)
	}

	if newState != StateTerminating {
		t.Errorf("expected state Terminating, got %v", newState)
	}
}

func TestStateMachine_Terminate(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()
	sm.TransitionForCommand(CmdEHLO, true)
	sm.TransitionForCommand(CmdQUIT, true)

	if err := sm.Terminate(); err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}

	if sm.State() != StateTerminated {
		t.Errorf("expected state Terminated, got %v", sm.State())
	}

	if !sm.State().IsTerminal() {
		t.Error("Terminated state should be terminal")
	}
}

func TestStateMachine_Abort(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()

	if err := sm.Abort(); err != nil {
		t.Fatalf("Abort failed: %v", err)
	}

	if sm.State() != StateAborted {
		t.Errorf("expected state Aborted, got %v", sm.State())
	}

	if !sm.State().IsTerminal() {
		t.Error("Aborted state should be terminal")
	}
}

func TestStateMachine_FailedCommand(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()

	// Failed command should not change state
	originalState := sm.State()
	newState, err := sm.TransitionForCommand(CmdHELO, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if newState != originalState {
		t.Errorf("failed command should not change state, was %v now %v", originalState, newState)
	}
}

func TestStateMachine_NOOPDoesNotChangeState(t *testing.T) {
	sm := NewStateMachine()
	sm.Connect()
	sm.Greet()
	sm.TransitionForCommand(CmdEHLO, true)

	originalState := sm.State()
	newState, err := sm.TransitionForCommand(CmdNOOP, true)
	if err != nil {
		t.Fatalf("NOOP failed: %v", err)
	}

	if newState != originalState {
		t.Errorf("NOOP should not change state, was %v now %v", originalState, newState)
	}
}

func TestStateMachine_Observer(t *testing.T) {
	var transitions []StateTransition

	observer := &testObserver{
		onStateChange: func(tr StateTransition) {
			transitions = append(transitions, tr)
		},
	}

	sm := NewStateMachineWithObserver(observer)
	sm.Connect()
	sm.Greet()
	sm.TransitionForCommand(CmdEHLO, true)

	if len(transitions) != 3 {
		t.Errorf("expected 3 transitions, got %d", len(transitions))
	}

	// Check first transition
	if transitions[0].From != StateDisconnected || transitions[0].To != StateConnected {
		t.Errorf("unexpected first transition: %+v", transitions[0])
	}
}

func TestIsCommandAllowed(t *testing.T) {
	tests := []struct {
		state   State
		cmd     CommandVerb
		allowed bool
	}{
		{StateGreeted, CmdHELO, true},
		{StateGreeted, CmdEHLO, true},
		{StateGreeted, CmdMAIL, false},
		{StateGreeted, CmdRCPT, false},
		{StateGreeted, CmdDATA, false},
		{StateGreeted, CmdQUIT, true},
		{StateGreeted, CmdNOOP, true},

		{StateIdentified, CmdHELO, true},
		{StateIdentified, CmdEHLO, true},
		{StateIdentified, CmdMAIL, true},
		{StateIdentified, CmdRCPT, false},
		{StateIdentified, CmdDATA, false},
		{StateIdentified, CmdQUIT, true},
		{StateIdentified, CmdNOOP, true},
		{StateIdentified, CmdRSET, true},
		{StateIdentified, CmdSTARTTLS, true},

		{StateMailFrom, CmdMAIL, false},
		{StateMailFrom, CmdRCPT, true},
		{StateMailFrom, CmdDATA, false},
		{StateMailFrom, CmdRSET, true},
		{StateMailFrom, CmdQUIT, true},

		{StateRcptTo, CmdRCPT, true},
		{StateRcptTo, CmdDATA, true},
		{StateRcptTo, CmdRSET, true},
		{StateRcptTo, CmdQUIT, true},
		{StateRcptTo, CmdMAIL, false},

		{StateData, CmdRCPT, false},
		{StateData, CmdMAIL, false},
		{StateData, CmdQUIT, false},
	}

	for _, tt := range tests {
		result := IsCommandAllowed(tt.state, tt.cmd)
		if result != tt.allowed {
			t.Errorf("IsCommandAllowed(%v, %v) = %v, want %v",
				tt.state, tt.cmd, result, tt.allowed)
		}
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateDisconnected, "Disconnected"},
		{StateConnected, "Connected"},
		{StateGreeted, "Greeted"},
		{StateIdentified, "Identified"},
		{StateMailFrom, "MailFrom"},
		{StateRcptTo, "RcptTo"},
		{StateData, "Data"},
		{StateDataDone, "DataDone"},
		{StateStartTLS, "StartTLS"},
		{StateTerminating, "Terminating"},
		{StateTerminated, "Terminated"},
		{StateAborted, "Aborted"},
		{State(999), "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

func TestState_InTransaction(t *testing.T) {
	inTransaction := []State{StateMailFrom, StateRcptTo, StateData}
	notInTransaction := []State{StateDisconnected, StateConnected, StateGreeted, StateIdentified, StateDataDone, StateTerminated}

	for _, s := range inTransaction {
		if !s.InTransaction() {
			t.Errorf("%v.InTransaction() = false, want true", s)
		}
	}

	for _, s := range notInTransaction {
		if s.InTransaction() {
			t.Errorf("%v.InTransaction() = true, want false", s)
		}
	}
}

type testObserver struct {
	onStateChange func(StateTransition)
}

func (o *testObserver) OnStateChange(tr StateTransition) {
	if o.onStateChange != nil {
		o.onStateChange(tr)
	}
}
