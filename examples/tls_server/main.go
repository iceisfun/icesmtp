// Package main demonstrates a TLS-enabled SMTP server using icesmtp.
//
// This example creates an SMTP server with:
// - STARTTLS support
// - Optional TLS (clients can choose to upgrade)
// - Certificate reloading on SIGHUP
//
// Usage:
//
//	# Generate test certificates first:
//	openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 365 -nodes -subj "/CN=localhost"
//
//	# Run the server:
//	go run main.go
//
// Test with:
//
//	openssl s_client -starttls smtp -connect localhost:2525
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"icesmtp"
	"icesmtp/mem"
)

func main() {
	// Check for certificate files
	certFile := "cert.pem"
	keyFile := "key.pem"

	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		log.Println("Certificate not found. Generate with:")
		log.Println("  openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 365 -nodes -subj \"/CN=localhost\"")
		log.Println("")
		log.Println("Starting without TLS...")
		startWithoutTLS()
		return
	}

	// Create reloadable TLS provider
	tlsProvider, err := icesmtp.NewReloadableTLSProvider(certFile, keyFile, icesmtp.TLSOptional)
	if err != nil {
		log.Fatalf("Failed to load TLS: %v", err)
	}

	// Set up certificate reload on SIGHUP
	go func() {
		sighup := make(chan os.Signal, 1)
		signal.Notify(sighup, syscall.SIGHUP)
		for range sighup {
			log.Println("Reloading TLS certificates...")
			if err := tlsProvider.Reload(context.Background()); err != nil {
				log.Printf("Failed to reload certificates: %v", err)
			} else {
				log.Println("Certificates reloaded successfully")
			}
		}
	}()

	// Create storage and mailbox
	storage := mem.NewStorage()
	mailbox := mem.NewMailboxWithDomains("localhost", "example.com")
	mailbox.SetCatchAll(true)

	// Configure session with TLS
	config := icesmtp.SessionConfig{
		ServerHostname: "mail.localhost",
		Limits:         icesmtp.DefaultSessionLimits(),
		Extensions: icesmtp.ExtensionSet{
			STARTTLS:            true,
			SIZE:                true,
			EightBitMIME:        true,
			PIPELINING:          true,
			ENHANCEDSTATUSCODES: true,
			HELP:                true,
		},
		TLSPolicy:   icesmtp.TLSOptional,
		TLSProvider: tlsProvider,
		Mailbox:     mailbox,
		Storage:     storage,
		Hooks:       &TLSLoggingHooks{},
	}

	// Start server
	listener, err := net.Listen("tcp", ":2525")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	log.Println("SMTP server with STARTTLS listening on :2525")
	log.Println("Send SIGHUP to reload certificates")
	log.Println("")
	log.Println("Test with:")
	log.Println("  openssl s_client -starttls smtp -connect localhost:2525")

	// Handle shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		cancel()
		listener.Close()
	}()

	// Accept connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("Accept error: %v", err)
				continue
			}
		}

		go handleConnection(ctx, conn, config)
	}
}

func startWithoutTLS() {
	storage := mem.NewStorage()
	mailbox := mem.NewMailboxWithDomains("localhost", "example.com")
	mailbox.SetCatchAll(true)

	config := icesmtp.SessionConfig{
		ServerHostname: "mail.localhost",
		Limits:         icesmtp.DefaultSessionLimits(),
		Extensions:     icesmtp.DefaultExtensions(),
		TLSPolicy:      icesmtp.TLSDisabled,
		Mailbox:        mailbox,
		Storage:        storage,
	}

	listener, err := net.Listen("tcp", ":2525")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	log.Println("SMTP server (no TLS) listening on :2525")

	ctx := context.Background()
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(ctx, conn, config)
	}
}

func handleConnection(ctx context.Context, conn net.Conn, config icesmtp.SessionConfig) {
	defer conn.Close()

	engine := icesmtp.NewEngine(conn, conn, config,
		icesmtp.WithClientAddr(conn.RemoteAddr().String()))

	if err := engine.Run(ctx); err != nil && err != context.Canceled {
		log.Printf("Session error: %v", err)
	}
}

// TLSLoggingHooks logs TLS-related events.
type TLSLoggingHooks struct {
	icesmtp.NullSessionHooks
}

func (h *TLSLoggingHooks) OnConnect(ctx context.Context, session icesmtp.SessionInfo) {
	log.Printf("[%s] Connection from %s", session.ID()[:8], session.ClientIP())
}

func (h *TLSLoggingHooks) OnTLSUpgrade(ctx context.Context, state icesmtp.TLSConnectionState, session icesmtp.SessionInfo) {
	log.Printf("[%s] TLS upgraded: %s, cipher: %s",
		session.ID()[:8],
		state.VersionString(),
		state.CipherSuiteString())
}

func (h *TLSLoggingHooks) OnDisconnect(ctx context.Context, session icesmtp.SessionInfo, reason icesmtp.DisconnectReason) {
	tlsStatus := "plain"
	if session.TLSActive() {
		tlsStatus = "TLS"
	}
	log.Printf("[%s] Disconnected (%s): %s", session.ID()[:8], tlsStatus, reason)
}

func (h *TLSLoggingHooks) OnDataEnd(ctx context.Context, envelope icesmtp.Envelope, session icesmtp.SessionInfo) {
	tlsStatus := "plain"
	if envelope.Metadata().TLSActive {
		tlsStatus = "TLS"
	}
	log.Printf("[%s] Message received over %s: %s (%d bytes)",
		session.ID()[:8], tlsStatus, envelope.ID(), envelope.DataSize())
}
