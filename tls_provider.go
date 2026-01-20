package icesmtp

import (
	"context"
	"crypto/tls"
	"sync"
)

// StaticTLSProvider is a TLS provider with a static certificate.
type StaticTLSProvider struct {
	config *tls.Config
	policy TLSPolicy
}

// NewStaticTLSProvider creates a TLS provider with a static configuration.
func NewStaticTLSProvider(config *tls.Config, policy TLSPolicy) *StaticTLSProvider {
	return &StaticTLSProvider{
		config: config,
		policy: policy,
	}
}

// NewStaticTLSProviderFromCert creates a TLS provider from a certificate.
func NewStaticTLSProviderFromCert(cert tls.Certificate, policy TLSPolicy) *StaticTLSProvider {
	config := SecureTLSConfig()
	config.Certificates = []tls.Certificate{cert}
	return NewStaticTLSProvider(config, policy)
}

// NewStaticTLSProviderFromFiles creates a TLS provider from certificate files.
func NewStaticTLSProviderFromFiles(certFile, keyFile string, policy TLSPolicy) (*StaticTLSProvider, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, &TLSError{
			Phase:   TLSErrorPhaseCertificate,
			Cause:   err,
			Message: "failed to load certificate",
		}
	}
	return NewStaticTLSProviderFromCert(cert, policy), nil
}

// GetConfig returns the TLS configuration.
func (p *StaticTLSProvider) GetConfig(ctx context.Context, hello *TLSClientHello) (*tls.Config, error) {
	return p.config, nil
}

// Policy returns the TLS policy.
func (p *StaticTLSProvider) Policy() TLSPolicy {
	return p.policy
}

// ReloadableTLSProvider is a TLS provider that supports certificate reloading.
type ReloadableTLSProvider struct {
	mu       sync.RWMutex
	certFile string
	keyFile  string
	config   *tls.Config
	policy   TLSPolicy
}

// NewReloadableTLSProvider creates a reloadable TLS provider.
func NewReloadableTLSProvider(certFile, keyFile string, policy TLSPolicy) (*ReloadableTLSProvider, error) {
	p := &ReloadableTLSProvider{
		certFile: certFile,
		keyFile:  keyFile,
		policy:   policy,
	}

	if err := p.Reload(context.Background()); err != nil {
		return nil, err
	}

	return p, nil
}

// GetConfig returns the TLS configuration.
func (p *ReloadableTLSProvider) GetConfig(ctx context.Context, hello *TLSClientHello) (*tls.Config, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config, nil
}

// Policy returns the TLS policy.
func (p *ReloadableTLSProvider) Policy() TLSPolicy {
	return p.policy
}

// Reload reloads the certificate from files.
func (p *ReloadableTLSProvider) Reload(ctx context.Context) error {
	cert, err := tls.LoadX509KeyPair(p.certFile, p.keyFile)
	if err != nil {
		return &TLSError{
			Phase:   TLSErrorPhaseCertificate,
			Cause:   err,
			Message: "failed to reload certificate",
		}
	}

	config := SecureTLSConfig()
	config.Certificates = []tls.Certificate{cert}

	p.mu.Lock()
	p.config = config
	p.mu.Unlock()

	return nil
}

// GetCertificate implements the tls.Config GetCertificate callback.
func (p *ReloadableTLSProvider) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.config.Certificates) > 0 {
		return &p.config.Certificates[0], nil
	}
	return nil, &TLSError{
		Phase:   TLSErrorPhaseCertificate,
		Message: "no certificate available",
	}
}

// SNITLSProvider is a TLS provider that selects certificates based on SNI.
type SNITLSProvider struct {
	mu           sync.RWMutex
	certificates map[ServerName]*tls.Certificate
	defaultCert  *tls.Certificate
	policy       TLSPolicy
}

// NewSNITLSProvider creates a new SNI-aware TLS provider.
func NewSNITLSProvider(policy TLSPolicy) *SNITLSProvider {
	return &SNITLSProvider{
		certificates: make(map[ServerName]*tls.Certificate),
		policy:       policy,
	}
}

// AddCertificate adds a certificate for a server name.
func (p *SNITLSProvider) AddCertificate(serverName ServerName, cert tls.Certificate) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.certificates[serverName] = &cert
}

// AddCertificateFromFiles adds a certificate from files.
func (p *SNITLSProvider) AddCertificateFromFiles(serverName ServerName, certFile, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return &TLSError{
			Phase:   TLSErrorPhaseCertificate,
			Cause:   err,
			Message: "failed to load certificate for " + serverName,
		}
	}
	p.AddCertificate(serverName, cert)
	return nil
}

// SetDefaultCertificate sets the default certificate for unknown server names.
func (p *SNITLSProvider) SetDefaultCertificate(cert tls.Certificate) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defaultCert = &cert
}

// GetConfig returns the TLS configuration with SNI support.
func (p *SNITLSProvider) GetConfig(ctx context.Context, hello *TLSClientHello) (*tls.Config, error) {
	config := SecureTLSConfig()
	config.GetCertificate = p.GetCertificate
	return config, nil
}

// Policy returns the TLS policy.
func (p *SNITLSProvider) Policy() TLSPolicy {
	return p.policy
}

// GetCertificate selects a certificate based on SNI.
func (p *SNITLSProvider) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if cert, ok := p.certificates[hello.ServerName]; ok {
		return cert, nil
	}

	if p.defaultCert != nil {
		return p.defaultCert, nil
	}

	return nil, &TLSError{
		Phase:   TLSErrorPhaseCertificate,
		Message: "no certificate for server name: " + hello.ServerName,
	}
}

// NoTLSProvider is a TLS provider that disables TLS.
type NoTLSProvider struct{}

// GetConfig returns an error since TLS is disabled.
func (NoTLSProvider) GetConfig(ctx context.Context, hello *TLSClientHello) (*tls.Config, error) {
	return nil, &TLSError{
		Phase:   TLSErrorPhaseConfig,
		Message: "TLS is disabled",
	}
}

// Policy returns TLSDisabled.
func (NoTLSProvider) Policy() TLSPolicy {
	return TLSDisabled
}

// Ensure providers implement the interface.
var (
	_ TLSProvider         = (*StaticTLSProvider)(nil)
	_ TLSProvider         = (*ReloadableTLSProvider)(nil)
	_ CertificateReloader = (*ReloadableTLSProvider)(nil)
	_ TLSProvider         = (*SNITLSProvider)(nil)
	_ TLSProvider         = NoTLSProvider{}
)
