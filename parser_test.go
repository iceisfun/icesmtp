package icesmtp

import (
	"errors"
	"testing"
)

func TestParser_ParseCommand(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name        string
		input       string
		wantVerb    CommandVerb
		wantArg     string
		wantErr     bool
		errContains string
	}{
		{
			name:     "HELO with hostname",
			input:    "HELO example.com\r\n",
			wantVerb: CmdHELO,
			wantArg:  "example.com",
		},
		{
			name:     "EHLO with hostname",
			input:    "EHLO mail.example.com\r\n",
			wantVerb: CmdEHLO,
			wantArg:  "mail.example.com",
		},
		{
			name:     "EHLO lowercase",
			input:    "ehlo example.com\r\n",
			wantVerb: CmdEHLO,
			wantArg:  "example.com",
		},
		{
			name:     "MAIL FROM",
			input:    "MAIL FROM:<user@example.com>\r\n",
			wantVerb: CmdMAIL,
			wantArg:  "FROM:<user@example.com>",
		},
		{
			name:     "MAIL FROM with SIZE",
			input:    "MAIL FROM:<user@example.com> SIZE=1000\r\n",
			wantVerb: CmdMAIL,
			wantArg:  "FROM:<user@example.com> SIZE=1000",
		},
		{
			name:     "RCPT TO",
			input:    "RCPT TO:<recipient@example.com>\r\n",
			wantVerb: CmdRCPT,
			wantArg:  "TO:<recipient@example.com>",
		},
		{
			name:     "DATA",
			input:    "DATA\r\n",
			wantVerb: CmdDATA,
			wantArg:  "",
		},
		{
			name:     "RSET",
			input:    "RSET\r\n",
			wantVerb: CmdRSET,
			wantArg:  "",
		},
		{
			name:     "NOOP",
			input:    "NOOP\r\n",
			wantVerb: CmdNOOP,
			wantArg:  "",
		},
		{
			name:     "QUIT",
			input:    "QUIT\r\n",
			wantVerb: CmdQUIT,
			wantArg:  "",
		},
		{
			name:     "VRFY",
			input:    "VRFY user\r\n",
			wantVerb: CmdVRFY,
			wantArg:  "user",
		},
		{
			name:     "HELP",
			input:    "HELP\r\n",
			wantVerb: CmdHELP,
			wantArg:  "",
		},
		{
			name:     "STARTTLS",
			input:    "STARTTLS\r\n",
			wantVerb: CmdSTARTTLS,
			wantArg:  "",
		},
		{
			name:        "Empty line",
			input:       "\r\n",
			wantErr:     true,
			errContains: "empty",
		},
		{
			name:        "Unknown command",
			input:       "UNKNOWN arg\r\n",
			wantErr:     true,
			errContains: "invalid",
		},
		{
			name:        "HELO without argument",
			input:       "HELO\r\n",
			wantErr:     true,
			errContains: "missing",
		},
		{
			name:        "DATA with argument",
			input:       "DATA extra\r\n",
			wantErr:     true,
			errContains: "unexpected",
		},
		{
			name:        "QUIT with argument",
			input:       "QUIT now\r\n",
			wantErr:     true,
			errContains: "unexpected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := p.ParseCommand([]byte(tt.input))

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if tt.errContains != "" {
					var parseErr *ParseError
					if errors.As(err, &parseErr) {
						// OK
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cmd.Verb != tt.wantVerb {
				t.Errorf("verb = %v, want %v", cmd.Verb, tt.wantVerb)
			}

			if cmd.Argument != tt.wantArg {
				t.Errorf("argument = %q, want %q", cmd.Argument, tt.wantArg)
			}
		})
	}
}

func TestParser_CommandTooLong(t *testing.T) {
	p := NewParser()
	p.MaxCommandLength = 20

	longLine := "HELO verylonghostname.example.com\r\n"
	_, err := p.ParseCommand([]byte(longLine))

	if err == nil {
		t.Fatal("expected error for long command")
	}
}

func TestParser_ParseMailPath_From(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantAddr   string
		wantNull   bool
		wantErr    bool
	}{
		{
			name:     "Simple address",
			input:    "FROM:<user@example.com>",
			wantAddr: "user@example.com",
		},
		{
			name:     "With spaces",
			input:    "FROM: <user@example.com>",
			wantAddr: "user@example.com",
		},
		{
			name:   "Null sender",
			input:  "FROM:<>",
			wantNull: true,
		},
		{
			name:     "Lowercase from",
			input:    "from:<user@example.com>",
			wantAddr: "user@example.com",
		},
		{
			name:     "With ESMTP params",
			input:    "FROM:<user@example.com> SIZE=1000",
			wantAddr: "user@example.com",
		},
		{
			name:    "Missing colon",
			input:   "FROM<user@example.com>",
			wantErr: true,
		},
		{
			name:    "Missing angle bracket",
			input:   "FROM:user@example.com",
			wantErr: true,
		},
		{
			name:    "Unclosed angle bracket",
			input:   "FROM:<user@example.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := ParseMailPath(tt.input, "FROM")

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantNull {
				if !path.IsNull {
					t.Error("expected null path")
				}
				return
			}

			if path.Address != tt.wantAddr {
				t.Errorf("address = %q, want %q", path.Address, tt.wantAddr)
			}
		})
	}
}

func TestParser_ParseMailPath_To(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantAddr string
		wantErr  bool
	}{
		{
			name:     "Simple address",
			input:    "TO:<recipient@example.com>",
			wantAddr: "recipient@example.com",
		},
		{
			name:     "Lowercase to",
			input:    "to:<recipient@example.com>",
			wantAddr: "recipient@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := ParseMailPath(tt.input, "TO")

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if path.Address != tt.wantAddr {
				t.Errorf("address = %q, want %q", path.Address, tt.wantAddr)
			}
		})
	}
}

func TestParser_ParseHeloHostname(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantErr  bool
	}{
		{
			name:  "Simple hostname",
			input: "mail.example.com",
			want:  "mail.example.com",
		},
		{
			name:  "Single label",
			input: "localhost",
			want:  "localhost",
		},
		{
			name:  "IPv4 literal",
			input: "[192.168.1.1]",
			want:  "[192.168.1.1]",
		},
		{
			name:  "IPv6 literal",
			input: "[IPv6:2001:db8::1]",
			want:  "[IPv6:2001:db8::1]",
		},
		{
			name:    "Empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "Unclosed literal",
			input:   "[192.168.1.1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostname, err := ParseHeloHostname(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if hostname != tt.want {
				t.Errorf("hostname = %q, want %q", hostname, tt.want)
			}
		})
	}
}

func TestParser_ParseESMTPParams(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   map[string]string
	}{
		{
			name:  "SIZE parameter",
			input: "FROM:<user@example.com> SIZE=1000",
			want:  map[string]string{"SIZE": "1000"},
		},
		{
			name:  "Multiple parameters",
			input: "FROM:<user@example.com> SIZE=1000 BODY=8BITMIME",
			want:  map[string]string{"SIZE": "1000", "BODY": "8BITMIME"},
		},
		{
			name:  "No parameters",
			input: "FROM:<user@example.com>",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := parseESMTPParams(tt.input)

			if tt.want == nil {
				if params != nil && len(params) > 0 {
					t.Errorf("expected nil params, got %v", params)
				}
				return
			}

			for k, v := range tt.want {
				if params[k] != v {
					t.Errorf("params[%q] = %q, want %q", k, params[k], v)
				}
			}
		})
	}
}

func TestDataLineReader_IsTerminator(t *testing.T) {
	reader := NewDataLineReader()

	tests := []struct {
		input    string
		expected bool
	}{
		{".\r\n", true},
		{".\n", true},
		{"..\r\n", false},
		{"text\r\n", false},
		{". \r\n", false},
	}

	for _, tt := range tests {
		result := reader.IsTerminator([]byte(tt.input))
		if result != tt.expected {
			t.Errorf("IsTerminator(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestDataLineReader_UnstuffLine(t *testing.T) {
	reader := NewDataLineReader()

	tests := []struct {
		input    string
		expected string
	}{
		{"..text", ".text"},
		{".text", "text"},
		{"text", "text"},
		{".", ""},
	}

	for _, tt := range tests {
		result := reader.UnstuffLine([]byte(tt.input))
		if string(result) != tt.expected {
			t.Errorf("UnstuffLine(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestDataLineReader_StuffLine(t *testing.T) {
	reader := NewDataLineReader()

	tests := []struct {
		input    string
		expected string
	}{
		{".text", "..text"},
		{"text", "text"},
		{".", ".."},
	}

	for _, tt := range tests {
		result := reader.StuffLine([]byte(tt.input))
		if string(result) != tt.expected {
			t.Errorf("StuffLine(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestCommandVerb_String(t *testing.T) {
	if CmdHELO.String() != "HELO" {
		t.Errorf("CmdHELO.String() = %q, want %q", CmdHELO.String(), "HELO")
	}
	if CmdEHLO.String() != "EHLO" {
		t.Errorf("CmdEHLO.String() = %q, want %q", CmdEHLO.String(), "EHLO")
	}
}

func TestParseCommandVerb(t *testing.T) {
	tests := []struct {
		input string
		want  CommandVerb
	}{
		{"HELO", CmdHELO},
		{"helo", CmdHELO},
		{"Helo", CmdHELO},
		{"EHLO", CmdEHLO},
		{"MAIL", CmdMAIL},
		{"RCPT", CmdRCPT},
		{"DATA", CmdDATA},
		{"QUIT", CmdQUIT},
		{"UNKNOWN", CmdUnknown},
		{"", CmdUnknown},
	}

	for _, tt := range tests {
		result := ParseCommandVerb(tt.input)
		if result != tt.want {
			t.Errorf("ParseCommandVerb(%q) = %v, want %v", tt.input, result, tt.want)
		}
	}
}

func TestCommandRequiresArgument(t *testing.T) {
	requiresArg := []CommandVerb{CmdHELO, CmdEHLO, CmdMAIL, CmdRCPT}
	noArg := []CommandVerb{CmdDATA, CmdRSET, CmdNOOP, CmdQUIT}

	for _, cmd := range requiresArg {
		if !CommandRequiresArgument(cmd) {
			t.Errorf("%v should require argument", cmd)
		}
	}

	for _, cmd := range noArg {
		if CommandRequiresArgument(cmd) {
			t.Errorf("%v should not require argument", cmd)
		}
	}
}

func TestCommandForbidsArgument(t *testing.T) {
	forbidsArg := []CommandVerb{CmdDATA, CmdRSET, CmdQUIT, CmdSTARTTLS}
	allowsArg := []CommandVerb{CmdHELO, CmdEHLO, CmdNOOP, CmdVRFY}

	for _, cmd := range forbidsArg {
		if !CommandForbidsArgument(cmd) {
			t.Errorf("%v should forbid argument", cmd)
		}
	}

	for _, cmd := range allowsArg {
		if CommandForbidsArgument(cmd) {
			t.Errorf("%v should not forbid argument", cmd)
		}
	}
}
