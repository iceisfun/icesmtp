package icesmtp

import (
	"context"
	"crypto/tls"
	"io"
)

// TLSPolicy defines when TLS should be used.
type TLSPolicy int

const (
	// TLSDisabled indicates TLS is not available.
	// STARTTLS will not be advertised.
	TLSDisabled TLSPolicy = iota

	// TLSOptional indicates TLS is available but not required.
	// STARTTLS will be advertised; clients may or may not upgrade.
	TLSOptional

	// TLSRequired indicates TLS must be used before MAIL command.
	// STARTTLS will be advertised; MAIL will be rejected without TLS.
	TLSRequired

	// TLSImmediate indicates the connection starts with TLS (SMTPS).
	// No STARTTLS negotiation occurs; connection is TLS from the start.
	TLSImmediate
)

// String returns a human-readable policy description.
func (p TLSPolicy) String() string {
	switch p {
	case TLSDisabled:
		return "Disabled"
	case TLSOptional:
		return "Optional"
	case TLSRequired:
		return "Required"
	case TLSImmediate:
		return "Immediate"
	default:
		return "Unknown"
	}
}

// TLSProvider provides TLS configuration for SMTP sessions.
// Implementations may provide static certificates, dynamic loading, or ACME.
type TLSProvider interface {
	// GetConfig returns the TLS configuration for a connection.
	// The hello contains the ClientHelloInfo if available (for SNI).
	// Implementations may use this to select certificates dynamically.
	GetConfig(ctx context.Context, hello *TLSClientHello) (*tls.Config, error)

	// Policy returns the TLS policy in effect.
	Policy() TLSPolicy
}

// TLSClientHello contains information from the TLS ClientHello message.
// This is used for SNI-based certificate selection.
type TLSClientHello struct {
	// ServerName is the server name from SNI (Server Name Indication).
	ServerName ServerName

	// SupportedVersions are the TLS versions the client supports.
	SupportedVersions []TLSVersionNumber

	// CipherSuites are the cipher suites the client supports.
	CipherSuites []TLSCipherSuiteID
}

// ServerName is the server name from TLS SNI.
type ServerName = string

// TLSVersionNumber is a TLS version number.
type TLSVersionNumber = uint16

// TLSCipherSuiteID is a TLS cipher suite identifier.
type TLSCipherSuiteID = uint16

// TLS version constants.
const (
	TLSVersion10 TLSVersionNumber = 0x0301
	TLSVersion11 TLSVersionNumber = 0x0302
	TLSVersion12 TLSVersionNumber = 0x0303
	TLSVersion13 TLSVersionNumber = 0x0304
)

// TLSUpgrader handles the STARTTLS upgrade process.
// This abstracts the TLS handshake from the session handler.
type TLSUpgrader interface {
	// Upgrade performs the TLS handshake on the given connection.
	// The reader/writer are wrapped with TLS and returned.
	// On success, returns the wrapped streams and connection state.
	// On failure, returns an error; the connection should be closed.
	Upgrade(ctx context.Context, r io.Reader, w io.Writer) (TLSUpgradeResult, error)
}

// TLSUpgradeResult contains the result of a successful TLS upgrade.
type TLSUpgradeResult struct {
	// Reader is the TLS-wrapped reader.
	Reader io.Reader

	// Writer is the TLS-wrapped writer.
	Writer io.Writer

	// State contains information about the negotiated TLS connection.
	State TLSConnectionState
}

// TLSConnectionState contains information about the negotiated TLS connection.
type TLSConnectionState struct {
	// Version is the negotiated TLS version.
	Version TLSVersionNumber

	// CipherSuite is the negotiated cipher suite.
	CipherSuite TLSCipherSuiteID

	// ServerName is the server name from SNI.
	ServerName ServerName

	// PeerCertificates indicates if the client presented certificates.
	PeerCertificates bool

	// VerifiedChains indicates if client certificates were verified.
	VerifiedChains bool
}

// VersionString returns a human-readable version string.
func (s TLSConnectionState) VersionString() TLSVersion {
	switch s.Version {
	case TLSVersion10:
		return "TLS 1.0"
	case TLSVersion11:
		return "TLS 1.1"
	case TLSVersion12:
		return "TLS 1.2"
	case TLSVersion13:
		return "TLS 1.3"
	default:
		return "Unknown"
	}
}

// CipherSuiteString returns a human-readable cipher suite name.
func (s TLSConnectionState) CipherSuiteString() TLSCipherSuite {
	return tls.CipherSuiteName(s.CipherSuite)
}

// TLSError represents a TLS-related error.
type TLSError struct {
	// Phase indicates where the error occurred.
	Phase TLSErrorPhase

	// Cause is the underlying error.
	Cause error

	// Message is a human-readable description.
	Message string
}

// TLSErrorPhase indicates the phase of TLS handling where an error occurred.
type TLSErrorPhase = string

const (
	// TLSErrorPhaseConfig indicates an error loading TLS configuration.
	TLSErrorPhaseConfig TLSErrorPhase = "Config"

	// TLSErrorPhaseHandshake indicates a handshake failure.
	TLSErrorPhaseHandshake TLSErrorPhase = "Handshake"

	// TLSErrorPhaseCertificate indicates a certificate error.
	TLSErrorPhaseCertificate TLSErrorPhase = "Certificate"
)

func (e *TLSError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *TLSError) Unwrap() error {
	return e.Cause
}

// CertificateProvider provides certificates for TLS.
// This allows dynamic certificate loading and rotation.
type CertificateProvider interface {
	// GetCertificate returns a certificate for the given server name.
	// This is called during TLS handshake for SNI support.
	GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
}

// CertificateReloader extends CertificateProvider with reload capability.
type CertificateReloader interface {
	CertificateProvider

	// Reload reloads certificates from their source.
	// This may be called on SIGHUP or certificate file changes.
	Reload(ctx context.Context) error
}

// MinTLSVersion returns the minimum TLS version that should be accepted.
// Returns TLS 1.2 as the minimum secure version.
func MinTLSVersion() TLSVersionNumber {
	return TLSVersion12
}

// SecureCipherSuites returns a list of secure cipher suites.
// These are suitable for production use and follow current best practices.
func SecureCipherSuites() []TLSCipherSuiteID {
	return []TLSCipherSuiteID{
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	}
}

// SecureTLSConfig returns a tls.Config with secure defaults.
// This can be used as a starting point for custom configurations.
func SecureTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:   MinTLSVersion(),
		CipherSuites: SecureCipherSuites(),
	}
}
