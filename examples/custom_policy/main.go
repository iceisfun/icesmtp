// Package main demonstrates custom policy enforcement with icesmtp.
//
// This example shows how to implement:
// - Custom sender validation
// - Custom recipient validation with aliasing
// - Session hooks for logging and metrics
// - Message inspection after storage
//
// Usage:
//
//	go run main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"icesmtp"
	"icesmtp/mem"
)

func main() {
	// Create storage
	storage := mem.NewStorage()

	// Create custom mailbox with aliases
	mailbox := &AliasedMailbox{
		addresses: map[string]bool{
			"admin@example.com":   true,
			"support@example.com": true,
			"user@example.com":    true,
		},
		aliases: map[string]string{
			"postmaster@example.com": "admin@example.com",
			"webmaster@example.com":  "admin@example.com",
			"help@example.com":       "support@example.com",
		},
		domains: map[string]bool{
			"example.com": true,
		},
	}

	// Create custom sender policy
	senderPolicy := &RestrictedSenderPolicy{
		blockedDomains: []string{"spam.example", "blocked.org"},
	}

	// Create hooks for logging
	hooks := &LoggingHooks{}

	// Configure session
	config := icesmtp.SessionConfig{
		ServerHostname: "mail.example.com",
		Limits: icesmtp.SessionLimits{
			MaxMessageSize:   10 * 1024 * 1024, // 10 MB
			MaxRecipients:    50,
			MaxCommandLength: 512,
			MaxLineLength:    998,
			CommandTimeout:   2 * time.Minute,
			DataTimeout:      5 * time.Minute,
			IdleTimeout:      2 * time.Minute,
			MaxErrors:        5,
			MaxTransactions:  20,
		},
		Extensions: icesmtp.ExtensionSet{
			SIZE:                true,
			EightBitMIME:        true,
			PIPELINING:          true,
			ENHANCEDSTATUSCODES: true,
			HELP:                true,
		},
		Mailbox:      mailbox,
		SenderPolicy: senderPolicy,
		Storage:      storage,
		Hooks:        hooks,
	}

	// Start server
	listener, err := net.Listen("tcp", ":2525")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	log.Println("SMTP server with custom policies listening on :2525")

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

			// Inspect stored messages
			for _, msg := range storage.List() {
				log.Printf("Stored message %s from %s to %v (%d bytes)",
					msg.Envelope.ID(),
					msg.Envelope.MailFrom().Address,
					msg.Envelope.Recipients(),
					len(msg.Data))
			}
		}(conn)
	}
}

// AliasedMailbox implements icesmtp.Mailbox with alias support.
type AliasedMailbox struct {
	addresses map[string]bool
	aliases   map[string]string
	domains   map[string]bool
}

func (m *AliasedMailbox) ValidateRecipient(ctx context.Context, recipient icesmtp.MailPath, session icesmtp.SessionInfo) icesmtp.RecipientResult {
	addr := strings.ToLower(recipient.Address)

	// Check if it's a direct address
	if m.addresses[addr] {
		return icesmtp.RecipientResult{
			Path:     recipient,
			Status:   icesmtp.RecipientAccepted,
			Response: icesmtp.ResponseOK,
		}
	}

	// Check if it's an alias
	if target, ok := m.aliases[addr]; ok {
		// Accept the alias, delivery will resolve to target
		log.Printf("Alias %s -> %s", addr, target)
		return icesmtp.RecipientResult{
			Path:     recipient,
			Status:   icesmtp.RecipientAccepted,
			Response: icesmtp.ResponseOK,
		}
	}

	// Check if domain is handled
	if idx := strings.LastIndex(addr, "@"); idx != -1 {
		domain := addr[idx+1:]
		if !m.domains[domain] {
			return icesmtp.RecipientResult{
				Path:   recipient,
				Status: icesmtp.RecipientRejected,
				Response: icesmtp.NewResponse(icesmtp.Reply550MailboxUnavailable,
					"Domain not handled"),
			}
		}
	}

	return icesmtp.RecipientResult{
		Path:   recipient,
		Status: icesmtp.RecipientRejected,
		Response: icesmtp.NewResponse(icesmtp.Reply550MailboxUnavailable,
			"No such user"),
	}
}

// RestrictedSenderPolicy implements icesmtp.SenderPolicy with domain blocking.
type RestrictedSenderPolicy struct {
	blockedDomains []string
}

func (p *RestrictedSenderPolicy) ValidateSender(ctx context.Context, sender icesmtp.MailPath, session icesmtp.SessionInfo) icesmtp.SenderResult {
	// Allow null sender (bounces)
	if sender.IsNull {
		return icesmtp.SenderResultAccepted()
	}

	addr := strings.ToLower(sender.Address)

	// Check blocked domains
	for _, blocked := range p.blockedDomains {
		if strings.HasSuffix(addr, "@"+blocked) {
			return icesmtp.SenderResult{
				Accepted: false,
				Response: icesmtp.NewResponse(icesmtp.Reply550MailboxUnavailable,
					fmt.Sprintf("Sender domain %s is blocked", blocked)),
			}
		}
	}

	return icesmtp.SenderResultAccepted()
}

// LoggingHooks implements icesmtp.SessionHooks for logging.
type LoggingHooks struct{}

func (h *LoggingHooks) OnConnect(ctx context.Context, session icesmtp.SessionInfo) {
	log.Printf("[%s] Connection from %s", session.ID()[:8], session.ClientIP())
}

func (h *LoggingHooks) OnDisconnect(ctx context.Context, session icesmtp.SessionInfo, reason icesmtp.DisconnectReason) {
	log.Printf("[%s] Disconnected: %s", session.ID()[:8], reason)
}

func (h *LoggingHooks) OnCommand(ctx context.Context, cmd icesmtp.Command, session icesmtp.SessionInfo) error {
	log.Printf("[%s] Command: %s %s", session.ID()[:8], cmd.Verb, cmd.Argument)
	return nil
}

func (h *LoggingHooks) OnMailFrom(ctx context.Context, sender icesmtp.MailPath, session icesmtp.SessionInfo) {
	log.Printf("[%s] MAIL FROM: %s", session.ID()[:8], sender.Address)
}

func (h *LoggingHooks) OnRcptTo(ctx context.Context, recipient icesmtp.MailPath, session icesmtp.SessionInfo) {
	log.Printf("[%s] RCPT TO: %s", session.ID()[:8], recipient.Address)
}

func (h *LoggingHooks) OnDataStart(ctx context.Context, session icesmtp.SessionInfo) {
	log.Printf("[%s] DATA started", session.ID()[:8])
}

func (h *LoggingHooks) OnDataEnd(ctx context.Context, envelope icesmtp.Envelope, session icesmtp.SessionInfo) {
	log.Printf("[%s] DATA completed: %s (%d bytes, %d recipients)",
		session.ID()[:8], envelope.ID(), envelope.DataSize(), envelope.RecipientCount())
}

func (h *LoggingHooks) OnTLSUpgrade(ctx context.Context, state icesmtp.TLSConnectionState, session icesmtp.SessionInfo) {
	log.Printf("[%s] TLS: %s", session.ID()[:8], state.VersionString())
}

func (h *LoggingHooks) OnError(ctx context.Context, err error, session icesmtp.SessionInfo) {
	log.Printf("[%s] Error: %v", session.ID()[:8], err)
}
