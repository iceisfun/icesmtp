# TLS and STARTTLS Handling

This document describes TLS support in icesmtp.

## TLS Policies

icesmtp supports four TLS policies:

```go
type TLSPolicy int

const (
    TLSDisabled  TLSPolicy = iota  // No TLS available
    TLSOptional                     // STARTTLS available but not required
    TLSRequired                     // Must use STARTTLS before MAIL
    TLSImmediate                    // Connection starts with TLS (SMTPS)
)
```

### TLSDisabled

- STARTTLS is not advertised in EHLO response
- STARTTLS command returns 502 Not Implemented
- Use for internal networks or testing

### TLSOptional

- STARTTLS is advertised in EHLO response
- Clients may choose to upgrade or not
- Common default for public servers

### TLSRequired

- STARTTLS is advertised in EHLO response
- MAIL command rejected with 530 if TLS not active
- Recommended for security-sensitive deployments

### TLSImmediate

- Connection is TLS from the start (port 465)
- No STARTTLS negotiation
- Legacy SMTPS mode

## TLS Providers

### StaticTLSProvider

Simplest provider with a static certificate:

```go
// From tls.Config
config := &tls.Config{
    Certificates: []tls.Certificate{cert},
}
provider := icesmtp.NewStaticTLSProvider(config, icesmtp.TLSOptional)

// From certificate
cert, _ := tls.LoadX509KeyPair("cert.pem", "key.pem")
provider := icesmtp.NewStaticTLSProviderFromCert(cert, icesmtp.TLSOptional)

// From files
provider, err := icesmtp.NewStaticTLSProviderFromFiles(
    "cert.pem", "key.pem", icesmtp.TLSRequired)
```

### ReloadableTLSProvider

Supports certificate reloading without restart:

```go
provider, err := icesmtp.NewReloadableTLSProvider(
    "cert.pem", "key.pem", icesmtp.TLSOptional)

// Reload on SIGHUP
go func() {
    sighup := make(chan os.Signal, 1)
    signal.Notify(sighup, syscall.SIGHUP)
    for range sighup {
        provider.Reload(context.Background())
    }
}()
```

### SNITLSProvider

Selects certificates based on Server Name Indication:

```go
provider := icesmtp.NewSNITLSProvider(icesmtp.TLSOptional)

// Add certificates for different domains
provider.AddCertificateFromFiles("mail.example.com", "example.pem", "example.key")
provider.AddCertificateFromFiles("mail.other.org", "other.pem", "other.key")

// Set default for unknown names
defaultCert, _ := tls.LoadX509KeyPair("default.pem", "default.key")
provider.SetDefaultCertificate(defaultCert)
```

### Custom Provider

Implement the TLSProvider interface:

```go
type TLSProvider interface {
    GetConfig(ctx context.Context, hello *TLSClientHello) (*tls.Config, error)
    Policy() TLSPolicy
}

type MyProvider struct{}

func (p *MyProvider) GetConfig(ctx context.Context, hello *TLSClientHello) (*tls.Config, error) {
    // Custom certificate selection logic
    return &tls.Config{...}, nil
}

func (p *MyProvider) Policy() icesmtp.TLSPolicy {
    return icesmtp.TLSRequired
}
```

## STARTTLS Flow

```
C: EHLO client.example.com
S: 250-mail.example.com
S: 250-SIZE 25000000
S: 250-STARTTLS
S: 250 OK
C: STARTTLS
S: 220 Ready to start TLS
[TLS handshake]
C: EHLO client.example.com
S: 250-mail.example.com
S: 250 OK
```

**Important**: After STARTTLS, the client MUST re-send EHLO. The session state returns to Greeted.

## Secure Configuration

### Recommended Settings

```go
config := icesmtp.SecureTLSConfig()
// Sets:
// - MinVersion: TLS 1.2
// - Secure cipher suites

// Or manually:
config := &tls.Config{
    MinVersion: tls.VersionTLS12,
    CipherSuites: []uint16{
        tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
        tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
        tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
        tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
        tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
        tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
    },
}
```

### Minimum TLS Version

TLS 1.2 is the recommended minimum:

```go
minVersion := icesmtp.MinTLSVersion() // Returns TLS 1.2
```

### Secure Cipher Suites

```go
suites := icesmtp.SecureCipherSuites()
```

Includes only modern, secure cipher suites with:
- Forward secrecy (ECDHE)
- Authenticated encryption (GCM, ChaCha20-Poly1305)

## TLS Connection State

After STARTTLS, session metadata includes TLS information:

```go
type TLSConnectionState struct {
    Version          TLSVersionNumber
    CipherSuite      TLSCipherSuiteID
    ServerName       ServerName
    PeerCertificates bool
    VerifiedChains   bool
}
```

Access via hooks:

```go
func (h *MyHooks) OnTLSUpgrade(ctx context.Context, state icesmtp.TLSConnectionState, session icesmtp.SessionInfo) {
    log.Printf("TLS: %s, cipher: %s",
        state.VersionString(),
        state.CipherSuiteString())
}
```

## Client Certificate Authentication

icesmtp supports client certificate verification:

```go
config := &tls.Config{
    ClientAuth: tls.RequireAndVerifyClientCert,
    ClientCAs:  certPool,
}
```

Check verification in hooks:

```go
func (h *MyHooks) OnTLSUpgrade(ctx context.Context, state icesmtp.TLSConnectionState, session icesmtp.SessionInfo) {
    if state.VerifiedChains {
        // Client certificate was verified
    }
}
```

## Let's Encrypt / ACME

For automatic certificate management, implement a custom TLSProvider that integrates with an ACME library:

```go
type ACMETLSProvider struct {
    manager *autocert.Manager
    policy  icesmtp.TLSPolicy
}

func (p *ACMETLSProvider) GetConfig(ctx context.Context, hello *icesmtp.TLSClientHello) (*tls.Config, error) {
    return &tls.Config{
        GetCertificate: p.manager.GetCertificate,
    }, nil
}

func (p *ACMETLSProvider) Policy() icesmtp.TLSPolicy {
    return p.policy
}
```

## Testing TLS

The test harness doesn't support TLS directly, but you can test TLS-related logic:

```go
func TestTLSRequired(t *testing.T) {
    h := harness.NewHarness(
        harness.WithExtensions(icesmtp.ExtensionSet{STARTTLS: true}),
    )
    // Note: Actual TLS negotiation requires real connections
    // Test the policy enforcement instead
}
```

For full TLS testing, use integration tests with actual network connections.
