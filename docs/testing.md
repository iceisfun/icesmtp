# Testing Guide

This document describes how to test SMTP implementations using icesmtp.

## Design Philosophy

icesmtp is designed for **socket-free testing**. All SMTP sessions operate over `io.Reader`/`io.Writer` pairs, enabling:

- Deterministic unit tests
- Replayable SMTP transcripts
- Fuzzing and fault injection
- No network dependencies

## Test Harness

The `harness` package provides a complete test environment:

```go
import "github.com/iceisfun/icesmtp/harness"

func TestMyFeature(t *testing.T) {
    h := harness.NewHarness()
    h.Mailbox.AddAddress("user@example.com")
    defer h.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    h.Start(ctx)

    // Send commands and verify responses
    h.Expect(icesmtp.Reply220ServiceReady)
    h.Send("EHLO test.example.com")
    h.Expect(icesmtp.Reply250OK)
}
```

### Harness Components

- **Input**: `PipeBuffer` for sending client data
- **Output**: `PipeBuffer` for receiving server responses
- **Storage**: `mem.Storage` for inspecting stored messages
- **Mailbox**: `mem.Mailbox` for configuring valid recipients
- **Transcript**: Records the full SMTP conversation

### Configuration

```go
h := harness.NewHarness(
    harness.WithServerHostname("mail.example.com"),
    harness.WithLimits(icesmtp.SessionLimits{
        MaxRecipients: 10,
    }),
)
```

## Testing Patterns

### Basic Conversation Test

```go
func TestBasicConversation(t *testing.T) {
    h := harness.NewHarness()
    h.Mailbox.AddAddress("user@example.com")
    defer h.Close()

    ctx := context.Background()
    h.Start(ctx)

    h.Expect(icesmtp.Reply220ServiceReady)

    h.Send("EHLO client.example.com")
    h.Expect(icesmtp.Reply250OK)

    h.Send("MAIL FROM:<sender@example.com>")
    h.Expect(icesmtp.Reply250OK)

    h.Send("RCPT TO:<user@example.com>")
    h.Expect(icesmtp.Reply250OK)

    h.Send("DATA")
    h.Expect(icesmtp.Reply354StartMailInput)

    h.Send("Subject: Test")
    h.Send("")
    h.Send("Body text.")
    h.Send(".")
    h.Expect(icesmtp.Reply250OK)

    h.Send("QUIT")
    h.Expect(icesmtp.Reply221ServiceClosing)
}
```

### Testing Error Conditions

```go
func TestInvalidRecipient(t *testing.T) {
    h := harness.NewHarness()
    // Don't add any addresses
    defer h.Close()

    ctx := context.Background()
    h.Start(ctx)

    h.Expect(icesmtp.Reply220ServiceReady)
    h.Send("EHLO client.example.com")
    h.Expect(icesmtp.Reply250OK)

    h.Send("MAIL FROM:<sender@example.com>")
    h.Expect(icesmtp.Reply250OK)

    // Should reject unknown recipient
    h.Send("RCPT TO:<nobody@example.com>")
    _, err := h.Expect(icesmtp.Reply550MailboxUnavailable)
    if err != nil {
        t.Fatalf("expected 550: %v", err)
    }
}
```

### Testing State Machine

```go
func TestBadSequence(t *testing.T) {
    h := harness.NewHarness()
    defer h.Close()

    ctx := context.Background()
    h.Start(ctx)

    h.Expect(icesmtp.Reply220ServiceReady)

    // Try MAIL before EHLO
    h.Send("MAIL FROM:<sender@example.com>")
    _, err := h.Expect(icesmtp.Reply503BadSequence)
    if err != nil {
        t.Fatalf("expected 503: %v", err)
    }
}
```

### Testing Message Storage

```go
func TestMessageStored(t *testing.T) {
    h := harness.NewHarness()
    h.Mailbox.AddAddress("user@example.com")
    defer h.Close()

    // ... send message ...

    // Verify storage
    if h.MessageCount() != 1 {
        t.Errorf("expected 1 message, got %d", h.MessageCount())
    }

    messages := h.Messages()
    if len(messages) > 0 {
        msg := messages[0]
        if msg.Envelope.MailFrom().Address != "sender@example.com" {
            t.Errorf("wrong sender")
        }
        if msg.Envelope.RecipientCount() != 1 {
            t.Errorf("wrong recipient count")
        }
    }
}
```

### Testing RSET

```go
func TestRSET(t *testing.T) {
    h := harness.NewHarness()
    h.Mailbox.AddAddress("user@example.com")
    defer h.Close()

    ctx := context.Background()
    h.Start(ctx)

    h.Expect(icesmtp.Reply220ServiceReady)
    h.Send("EHLO client.example.com")
    h.Expect(icesmtp.Reply250OK)

    h.Send("MAIL FROM:<sender@example.com>")
    h.Expect(icesmtp.Reply250OK)

    h.Send("RCPT TO:<user@example.com>")
    h.Expect(icesmtp.Reply250OK)

    // RSET should reset transaction
    h.Send("RSET")
    h.Expect(icesmtp.Reply250OK)

    // RCPT should now fail (not in transaction)
    h.Send("RCPT TO:<user@example.com>")
    h.Expect(icesmtp.Reply503BadSequence)
}
```

## Transcript Inspection

The harness records all communication:

```go
h := harness.NewHarness()
// ... run test ...

// Print transcript
t.Logf("Transcript:\n%s", h.Transcript.String())

// Inspect entries
for _, entry := range h.Transcript.Entries() {
    if entry.Direction == harness.DirectionClient {
        // Client sent this
    } else {
        // Server sent this
    }
}
```

## Unit Testing Components

### State Machine

```go
func TestStateMachine(t *testing.T) {
    sm := icesmtp.NewStateMachine()

    sm.Connect()
    sm.Greet()

    if sm.State() != icesmtp.StateGreeted {
        t.Error("expected Greeted state")
    }

    sm.TransitionForCommand(icesmtp.CmdEHLO, true)

    if sm.State() != icesmtp.StateIdentified {
        t.Error("expected Identified state")
    }
}
```

### Command Parser

```go
func TestParser(t *testing.T) {
    p := icesmtp.NewParser()

    cmd, err := p.ParseCommand([]byte("MAIL FROM:<user@example.com>\r\n"))
    if err != nil {
        t.Fatal(err)
    }

    if cmd.Verb != icesmtp.CmdMAIL {
        t.Error("expected MAIL command")
    }
}
```

### Mail Path Parser

```go
func TestMailPath(t *testing.T) {
    path, err := icesmtp.ParseMailPath("FROM:<user@example.com>", "FROM")
    if err != nil {
        t.Fatal(err)
    }

    if path.Address != "user@example.com" {
        t.Errorf("wrong address: %s", path.Address)
    }
}
```

## Test Coverage Goals

Aim to test:

1. **Happy paths**: Normal SMTP conversations
2. **Error conditions**: Invalid commands, unknown recipients, etc.
3. **State transitions**: All valid and invalid transitions
4. **Edge cases**: Empty messages, dot-stuffing, null sender
5. **Limits**: Message size, recipient count, etc.
6. **Security**: Malformed input, attack vectors

## Fuzzing

The I/O abstraction enables fuzzing:

```go
func FuzzParser(f *testing.F) {
    f.Add([]byte("HELO test\r\n"))
    f.Add([]byte("MAIL FROM:<a@b>\r\n"))

    f.Fuzz(func(t *testing.T, data []byte) {
        p := icesmtp.NewParser()
        p.ParseCommand(data)
        // Should not panic
    })
}
```
