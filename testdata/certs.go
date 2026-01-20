// Package testdata provides test fixtures for icesmtp tests.
package testdata

import (
	"crypto/tls"
	"path/filepath"
	"runtime"
)

// TestDataDir returns the path to the testdata directory.
func TestDataDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

// CertFile returns the path to the test certificate file.
func CertFile() string {
	return filepath.Join(TestDataDir(), "server.crt")
}

// KeyFile returns the path to the test key file.
func KeyFile() string {
	return filepath.Join(TestDataDir(), "server.key")
}

// LoadTestCertificate loads the test certificate and key.
func LoadTestCertificate() (tls.Certificate, error) {
	return tls.LoadX509KeyPair(CertFile(), KeyFile())
}

// TestTLSConfig returns a TLS config using the test certificate.
func TestTLSConfig() (*tls.Config, error) {
	cert, err := LoadTestCertificate()
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
