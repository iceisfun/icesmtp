// Package main demonstrates a minimal SMTP server using icesmtp.
//
// This example creates a simple SMTP server that:
// - Accepts connections on port 2525
// - Accepts mail for any address at example.com
// - Stores messages in memory
// - Logs to stdout
//
// Usage:
//
//	go run main.go
//
// Test with:
//
//	telnet localhost 2525
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/iceisfun/icesmtp"
	"github.com/iceisfun/icesmtp/mem"
)

func main() {
	// Create in-memory storage and mailbox
	storage := mem.NewStorage()
	mailbox := mem.NewMailboxWithDomains("example.com")
	mailbox.SetCatchAll(true) // Accept any address at example.com

	// Create a logger
	logger := icesmtp.NewStdLogger(os.Stdout, icesmtp.LogLevelInfo)

	// Configure session settings
	config := icesmtp.SessionConfig{
		ServerHostname: "mail.example.com",
		Limits:         icesmtp.DefaultSessionLimits(),
		Extensions:     icesmtp.DefaultExtensions(),
		Mailbox:        mailbox,
		Storage:        storage,
		Logger:         logger,
	}

	// Start listening
	listener, err := net.Listen("tcp", ":2525")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	log.Println("SMTP server listening on :2525")
	log.Println("Press Ctrl+C to stop")

	// Handle graceful shutdown
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

		// Handle each connection in a goroutine
		go handleConnection(ctx, conn, config)
	}
}

func handleConnection(ctx context.Context, conn net.Conn, config icesmtp.SessionConfig) {
	defer conn.Close()

	// Create engine with client info
	engine := icesmtp.NewEngine(conn, conn, config,
		icesmtp.WithClientAddr(conn.RemoteAddr().String()),
		icesmtp.WithClientIP(conn.RemoteAddr().(*net.TCPAddr).IP.String()))

	// Run the session
	if err := engine.Run(ctx); err != nil && err != context.Canceled {
		log.Printf("Session error: %v", err)
	}
}
