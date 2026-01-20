# Public Interfaces

This document describes all public interfaces in icesmtp.

## Core Interfaces

### Storage

The `Storage` interface handles durable message persistence.

```go
type Storage interface {
    // Store persists a finalized envelope.
    Store(ctx context.Context, envelope Envelope) (StorageReceipt, error)

    // StoreStream persists an envelope with streaming data.
    StoreStream(ctx context.Context, envelope Envelope, data io.Reader) (StorageReceipt, error)
}
```

**Implementation Notes:**
- Context should be respected for timeouts and cancellation
- Implementations may store to disk, database, message queue, or any backend
- Return `StorageReceipt` with assigned message ID on success
- Return `StorageError` with `Retryable` flag for transient failures

**Provided Implementations:**
- `NullStorage` - Discards all messages (testing)
- `mem.Storage` - In-memory storage (testing/development)

### Mailbox

The `Mailbox` interface handles recipient validation.

```go
type Mailbox interface {
    // ValidateRecipient checks whether a recipient address is valid.
    ValidateRecipient(ctx context.Context, recipient MailPath, session SessionInfo) RecipientResult
}
```

**Implementation Notes:**
- Called during RCPT TO processing
- May query databases, LDAP, APIs, or local configuration
- Return `RecipientAccepted`, `RecipientRejected`, or `RecipientDeferred`
- Include appropriate SMTP response in result

**Provided Implementations:**
- `AcceptAllMailbox` - Accepts all recipients (testing)
- `RejectAllMailbox` - Rejects all recipients (testing)
- `mem.Mailbox` - Static registry with catch-all support

### MailboxExtended

Optional extension for additional mailbox operations.

```go
type MailboxExtended interface {
    Mailbox

    // Exists checks if a mailbox exists.
    Exists(ctx context.Context, address EmailAddress) (bool, error)

    // CanReceive checks if the mailbox can currently receive mail.
    CanReceive(ctx context.Context, address EmailAddress) (bool, MailboxStatus, error)
}
```

### SenderPolicy

Optional interface for sender validation.

```go
type SenderPolicy interface {
    // ValidateSender checks whether a sender address is acceptable.
    ValidateSender(ctx context.Context, sender MailPath, session SessionInfo) SenderResult
}
```

**Provided Implementations:**
- `AcceptAllSenderPolicy` - Accepts all senders

### TLSProvider

The `TLSProvider` interface provides TLS configuration.

```go
type TLSProvider interface {
    // GetConfig returns the TLS configuration for a connection.
    GetConfig(ctx context.Context, hello *TLSClientHello) (*tls.Config, error)

    // Policy returns the TLS policy in effect.
    Policy() TLSPolicy
}
```

**Provided Implementations:**
- `StaticTLSProvider` - Static certificate
- `ReloadableTLSProvider` - Certificate reloading support
- `SNITLSProvider` - SNI-based certificate selection
- `NoTLSProvider` - TLS disabled

### Envelope

The `Envelope` interface represents a mail transaction.

```go
type Envelope interface {
    ID() EnvelopeID
    MailFrom() MailPath
    Recipients() []MailPath
    RecipientCount() RecipientCount
    ESMTPParams() ESMTPParams
    DeclaredSize() MessageSize
    ReceivedAt() time.Time
    Data() MessageData
    DataSize() MessageSize
    IsFinalized() bool
    Metadata() EnvelopeMetadata
}
```

### EnvelopeBuilder

Used to construct envelopes during a transaction.

```go
type EnvelopeBuilder interface {
    SetMailFrom(path MailPath, params ESMTPParams) error
    AddRecipient(path MailPath) error
    DataWriter() (io.WriteCloser, error)
    Finalize() (Envelope, error)
    Reset()
    Build() Envelope
}
```

### SessionInfo

Read-only session information for policy decisions.

```go
type SessionInfo interface {
    ID() SessionID
    State() State
    ClientHostname() Hostname
    ClientIP() IPAddress
    TLSActive() bool
    Authenticated() bool
    AuthenticatedUser() Username
    CurrentMailFrom() *MailPath
    CurrentRecipientCount() RecipientCount
}
```

### SessionHooks

Optional callbacks for session lifecycle events.

```go
type SessionHooks interface {
    OnConnect(ctx context.Context, session SessionInfo)
    OnDisconnect(ctx context.Context, session SessionInfo, reason DisconnectReason)
    OnCommand(ctx context.Context, cmd Command, session SessionInfo) error
    OnMailFrom(ctx context.Context, sender MailPath, session SessionInfo)
    OnRcptTo(ctx context.Context, recipient MailPath, session SessionInfo)
    OnDataStart(ctx context.Context, session SessionInfo)
    OnDataEnd(ctx context.Context, envelope Envelope, session SessionInfo)
    OnTLSUpgrade(ctx context.Context, state TLSConnectionState, session SessionInfo)
    OnError(ctx context.Context, err error, session SessionInfo)
}
```

**Provided Implementations:**
- `NullSessionHooks` - No-op implementation

### Logger

Logging interface for instrumentation.

```go
type Logger interface {
    Debug(ctx context.Context, msg string, attrs ...LogAttr)
    Info(ctx context.Context, msg string, attrs ...LogAttr)
    Warn(ctx context.Context, msg string, attrs ...LogAttr)
    Error(ctx context.Context, msg string, attrs ...LogAttr)
    WithAttrs(attrs ...LogAttr) Logger
    WithSession(sessionID SessionID) Logger
}
```

**Provided Implementations:**
- `NullLogger` - Discards all logs
- `StdLogger` - Standard library logger wrapper

## Type Aliases

For clarity and type safety, icesmtp uses type aliases extensively:

```go
type SessionID = string
type EnvelopeID = string
type EmailAddress = string
type Hostname = string
type Domain = string
type IPAddress = string
type Username = string
type MessageSize = int64
type RecipientCount = int
type CommandLength = int
type LineLength = int
type Duration = time.Duration
```

## Extensibility Points

1. **Storage Backend**: Implement `Storage` to persist messages anywhere
2. **Recipient Validation**: Implement `Mailbox` for custom validation logic
3. **Sender Policy**: Implement `SenderPolicy` for sender restrictions
4. **TLS Handling**: Implement `TLSProvider` for custom certificate management
5. **Session Hooks**: Implement `SessionHooks` for logging, metrics, or side effects
6. **Envelope Factory**: Implement `EnvelopeFactory` for custom envelope handling
