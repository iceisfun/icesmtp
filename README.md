# icesmtp

A pure Go SMTP protocol framework providing protocol-correct, testable, and instrumentable SMTP handling.

## Overview

**icesmtp** is a protocol engine, not a mail server. It provides SMTP correctness, explicit control, and testability over convenience.

The library allows you to implement your own mail infrastructure (mailboxes, storage backends, policy engines, and certificate handling) without re-implementing SMTP itself.

## Design Philosophy

- **Protocol correctness**: Strict RFC 5321 compliance with explicit state machine
- **Testability**: Zero dependency on network sockets for correctness testing
- **Explicit control**: Clean interfaces for all mutable behavior
- **Security awareness**: Built-in DoS protection and configurable limits

## Features

- Full SMTP command handling (HELO, EHLO, MAIL, RCPT, DATA, RSET, NOOP, VRFY, HELP, QUIT, STARTTLS)
- Explicit protocol state machine with documented transitions
- Clean interfaces for Storage, Mailbox, Envelope, and TLS handling
- I/O abstraction over `io.Reader`/`io.Writer` for socket-free testing
- Context-based timeouts and cancellation
- Configurable limits for DoS protection
- ESMTP extension support (SIZE, 8BITMIME, PIPELINING, STARTTLS, etc.)

## Installation

```bash
go get icesmtp
```

## Quick Start

```go
package main

import (
    "context"
    "net"
    "log"

    "icesmtp"
    "icesmtp/mem"
)

func main() {
    // Create in-memory storage and mailbox
    storage := mem.NewStorage()
    mailbox := mem.NewMailbox()
    mailbox.AddAddress("user@example.com")

    // Configure the session
    config := icesmtp.SessionConfig{
        ServerHostname: "mail.example.com",
        Limits:         icesmtp.DefaultSessionLimits(),
        Extensions:     icesmtp.DefaultExtensions(),
        Mailbox:        mailbox,
        Storage:        storage,
    }

    // Listen for connections
    listener, err := net.Listen("tcp", ":2525")
    if err != nil {
        log.Fatal(err)
    }
    defer listener.Close()

    log.Println("SMTP server listening on :2525")

    for {
        conn, err := listener.Accept()
        if err != nil {
            log.Printf("Accept error: %v", err)
            continue
        }

        go func(c net.Conn) {
            defer c.Close()

            engine := icesmtp.NewEngine(c, c, config,
                icesmtp.WithClientAddr(c.RemoteAddr().String()))

            ctx := context.Background()
            if err := engine.Run(ctx); err != nil {
                log.Printf("Session error: %v", err)
            }
        }(conn)
    }
}
```

## Core Interfaces

### Storage

Responsible for durable message persistence:

```go
type Storage interface {
    Store(ctx context.Context, envelope Envelope) (StorageReceipt, error)
    StoreStream(ctx context.Context, envelope Envelope, data io.Reader) (StorageReceipt, error)
}
```

### Mailbox

Responsible for recipient validation:

```go
type Mailbox interface {
    ValidateRecipient(ctx context.Context, recipient MailPath, session SessionInfo) RecipientResult
}
```

### TLSProvider

Responsible for TLS configuration:

```go
type TLSProvider interface {
    GetConfig(ctx context.Context, hello *TLSClientHello) (*tls.Config, error)
    Policy() TLSPolicy
}
```

## Testing

The library is designed for socket-free testing using the harness package:

```go
package main

import (
    "context"
    "testing"
    "time"

    "icesmtp"
    "icesmtp/harness"
)

func TestSMTPConversation(t *testing.T) {
    h := harness.NewHarness()
    h.Mailbox.AddAddress("user@example.com")
    defer h.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    h.Start(ctx)

    // Expect greeting
    h.Expect(icesmtp.Reply220ServiceReady)

    // Send EHLO
    h.Send("EHLO client.example.com")
    h.Expect(icesmtp.Reply250OK)

    // Send message
    h.Send("MAIL FROM:<sender@example.com>")
    h.Expect(icesmtp.Reply250OK)

    h.Send("RCPT TO:<user@example.com>")
    h.Expect(icesmtp.Reply250OK)

    h.Send("DATA")
    h.Expect(icesmtp.Reply354StartMailInput)

    h.Send("Subject: Test")
    h.Send("")
    h.Send("Hello, World!")
    h.Send(".")
    h.Expect(icesmtp.Reply250OK)

    h.Send("QUIT")
    h.Expect(icesmtp.Reply221ServiceClosing)

    // Verify message was stored
    if h.MessageCount() != 1 {
        t.Errorf("expected 1 message, got %d", h.MessageCount())
    }
}
```

## Configuration

### Session Limits

```go
limits := icesmtp.SessionLimits{
    MaxMessageSize:   25 * 1024 * 1024, // 25 MB
    MaxRecipients:    100,
    MaxCommandLength: 512,
    MaxLineLength:    998,
    CommandTimeout:   5 * time.Minute,
    DataTimeout:      10 * time.Minute,
    IdleTimeout:      5 * time.Minute,
    MaxErrors:        10,
    MaxTransactions:  100,
}
```

### Extensions

```go
extensions := icesmtp.ExtensionSet{
    STARTTLS:            true,
    SIZE:                true,
    EightBitMIME:        true,
    PIPELINING:          true,
    ENHANCEDSTATUSCODES: true,
    HELP:                true,
}
```

## Protocol State Machine

The SMTP state machine follows these states:

```
Disconnected -> Connected -> Greeted -> Identified
                                           |
                                           v
                                       MailFrom -> RcptTo -> Data -> DataDone
                                           ^                            |
                                           +----------------------------+
                                           (via RSET or transaction complete)
```

See `docs/protocol.md` for detailed state transition documentation.

## Non-Goals

- Full MTA implementation (queueing, retries, relaying)
- Spam filtering or DKIM/DMARC/SPF
- POP3/IMAP
- Web UI or management plane

## Documentation

- `docs/protocol.md` - SMTP state machine and flow
- `docs/interfaces.md` - Public interfaces and contracts
- `docs/security.md` - DoS considerations and mitigations
- `docs/testing.md` - Test harness and philosophy
- `docs/tls.md` - Certificate and STARTTLS handling

## License

MIT License - see LICENSE file for details.
