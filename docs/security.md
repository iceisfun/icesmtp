# Security and DoS Considerations

This document describes the security features and DoS mitigations in icesmtp.

## Denial of Service Vectors

SMTP servers are frequent targets for DoS attacks. icesmtp addresses the following vectors:

### Slow Client Attacks

**Attack**: Client sends data very slowly but not slow enough to trigger idle timeouts.

**Mitigations**:
- `CommandTimeout`: Maximum time to receive a complete command line
- `DataTimeout`: Maximum time to receive message data
- `IdleTimeout`: Maximum time between commands
- Context-based cancellation for all operations

```go
limits := icesmtp.SessionLimits{
    CommandTimeout: 5 * time.Minute,
    DataTimeout:    10 * time.Minute,
    IdleTimeout:    5 * time.Minute,
}
```

### Command Drip Attacks

**Attack**: Client sends commands one byte at a time.

**Mitigations**:
- Command timeout applies to entire command line
- Buffered reading with deadline enforcement

### Oversized Payloads

**Attack**: Client sends extremely large messages to exhaust memory or disk.

**Mitigations**:
- `MaxMessageSize`: Maximum message size in bytes
- SIZE extension allows early rejection
- Streaming data handling to avoid buffering

```go
limits := icesmtp.SessionLimits{
    MaxMessageSize: 25 * 1024 * 1024, // 25 MB
}
```

### Excessive Recipients

**Attack**: Client specifies thousands of recipients per message.

**Mitigations**:
- `MaxRecipients`: Maximum recipients per message
- Early rejection at limit

```go
limits := icesmtp.SessionLimits{
    MaxRecipients: 100,
}
```

### Command Line Length

**Attack**: Client sends very long command lines.

**Mitigations**:
- `MaxCommandLength`: Maximum command line length (RFC 5321 specifies 512)

```go
limits := icesmtp.SessionLimits{
    MaxCommandLength: 512,
}
```

### Data Line Length

**Attack**: Client sends very long lines in message data.

**Mitigations**:
- `MaxLineLength`: Maximum data line length (RFC 5321 specifies 998)

```go
limits := icesmtp.SessionLimits{
    MaxLineLength: 998,
}
```

### Error Flooding

**Attack**: Client sends many invalid commands to consume resources.

**Mitigations**:
- `MaxErrors`: Maximum consecutive errors before disconnection

```go
limits := icesmtp.SessionLimits{
    MaxErrors: 10,
}
```

### Transaction Flooding

**Attack**: Client sends many transactions in a single session.

**Mitigations**:
- `MaxTransactions`: Maximum transactions per session

```go
limits := icesmtp.SessionLimits{
    MaxTransactions: 100,
}
```

## TLS Security

### Minimum TLS Version

icesmtp defaults to TLS 1.2 as the minimum version:

```go
config := icesmtp.SecureTLSConfig()
// Sets MinVersion = TLS 1.2
```

### Cipher Suite Selection

Default cipher suites prioritize modern, secure options:

```go
suites := icesmtp.SecureCipherSuites()
// Returns ECDHE suites with AES-GCM and ChaCha20-Poly1305
```

### TLS Policy Options

```go
type TLSPolicy int

const (
    TLSDisabled  // No TLS available
    TLSOptional  // STARTTLS available but not required
    TLSRequired  // Must use STARTTLS before MAIL
    TLSImmediate // Connection starts with TLS (SMTPS)
)
```

## Input Validation

### Email Address Validation

- Basic RFC compliance checking
- Domain validation
- Local part validation
- Protection against injection attacks

### Command Parsing

- Strict parsing according to RFC 5321
- Unknown commands rejected
- Malformed commands rejected
- State validation before command execution

## Context and Cancellation

All operations respect `context.Context`:

```go
func (e *Engine) Run(ctx context.Context) error
```

This enables:
- Graceful shutdown
- Request timeouts
- Resource cleanup
- Cancellation propagation

## Best Practices

### Production Configuration

```go
config := icesmtp.SessionConfig{
    Limits: icesmtp.SessionLimits{
        MaxMessageSize:   25 * 1024 * 1024,
        MaxRecipients:    100,
        MaxCommandLength: 512,
        MaxLineLength:    998,
        CommandTimeout:   5 * time.Minute,
        DataTimeout:      10 * time.Minute,
        IdleTimeout:      5 * time.Minute,
        MaxErrors:        10,
        MaxTransactions:  100,
        MaxAuthAttempts:  3,
    },
    TLSPolicy: icesmtp.TLSRequired,
}
```

### Logging and Monitoring

Use `SessionHooks` to monitor:
- Connection attempts
- Authentication failures
- Policy violations
- Error rates

```go
type SecurityHooks struct {
    icesmtp.NullSessionHooks
}

func (h *SecurityHooks) OnError(ctx context.Context, err error, session icesmtp.SessionInfo) {
    // Log error with session context
    log.Printf("Session %s error: %v", session.ID(), err)
}

func (h *SecurityHooks) OnDisconnect(ctx context.Context, session icesmtp.SessionInfo, reason icesmtp.DisconnectReason) {
    if reason == icesmtp.DisconnectPolicyViolation {
        // Alert on policy violations
        log.Printf("Policy violation from %s", session.ClientIP())
    }
}
```

### Rate Limiting

While icesmtp doesn't include built-in rate limiting, you can implement it via hooks or external middleware:

```go
type RateLimitingHooks struct {
    icesmtp.NullSessionHooks
    limiter *rate.Limiter
}

func (h *RateLimitingHooks) OnConnect(ctx context.Context, session icesmtp.SessionInfo) {
    if !h.limiter.Allow() {
        // Rate limit exceeded
    }
}
```

## Reporting Security Issues

If you discover a security vulnerability, please report it privately to the maintainers before public disclosure.
