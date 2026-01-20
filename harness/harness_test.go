package harness

import (
	"context"
	"testing"
	"time"

	"github.com/iceisfun/icesmtp"
)

func TestHarness_BasicConversation(t *testing.T) {
	h := NewHarness()
	h.Mailbox.AddAddress("user@example.com")
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	// Expect greeting
	lines, err := h.Expect(icesmtp.Reply220ServiceReady)
	if err != nil {
		t.Fatalf("greeting: %v", err)
	}
	t.Logf("Greeting: %v", lines)

	// Send EHLO
	h.Send("EHLO client.example.com")
	_, err = h.Expect(icesmtp.Reply250OK)
	if err != nil {
		t.Fatalf("EHLO: %v", err)
	}

	// Send QUIT
	h.Send("QUIT")
	_, err = h.Expect(icesmtp.Reply221ServiceClosing)
	if err != nil {
		t.Fatalf("QUIT: %v", err)
	}

	t.Logf("Transcript:\n%s", h.Transcript.String())
}

func TestHarness_FullMailTransaction(t *testing.T) {
	h := NewHarness()
	h.Mailbox.AddAddress("recipient@example.com")
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	// Greeting
	h.Expect(icesmtp.Reply220ServiceReady)

	// EHLO
	h.Send("EHLO client.example.com")
	h.Expect(icesmtp.Reply250OK)

	// MAIL FROM
	h.Send("MAIL FROM:<sender@example.com>")
	_, err := h.Expect(icesmtp.Reply250OK)
	if err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}

	// RCPT TO
	h.Send("RCPT TO:<recipient@example.com>")
	_, err = h.Expect(icesmtp.Reply250OK)
	if err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}

	// DATA
	h.Send("DATA")
	_, err = h.Expect(icesmtp.Reply354StartMailInput)
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}

	// Send message
	h.Send("Subject: Test")
	h.Send("")
	h.Send("This is a test message.")
	h.Send(".")

	_, err = h.Expect(icesmtp.Reply250OK)
	if err != nil {
		t.Fatalf("DATA complete: %v", err)
	}

	// QUIT
	h.Send("QUIT")
	h.Expect(icesmtp.Reply221ServiceClosing)

	// Check that message was stored
	if h.MessageCount() != 1 {
		t.Errorf("expected 1 message, got %d", h.MessageCount())
	}

	messages := h.Messages()
	if len(messages) > 0 {
		msg := messages[0]
		t.Logf("Stored message ID: %s", msg.Envelope.ID())
		t.Logf("From: %s", msg.Envelope.MailFrom().Address)
		t.Logf("To: %v", msg.Envelope.Recipients())
		t.Logf("Data size: %d bytes", len(msg.Data))
	}

	t.Logf("Transcript:\n%s", h.Transcript.String())
}

func TestHarness_InvalidRecipient(t *testing.T) {
	h := NewHarness()
	// Don't add any addresses - all recipients should be rejected
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	h.Expect(icesmtp.Reply220ServiceReady)

	h.Send("EHLO client.example.com")
	h.Expect(icesmtp.Reply250OK)

	h.Send("MAIL FROM:<sender@example.com>")
	h.Expect(icesmtp.Reply250OK)

	// RCPT TO should fail - mailbox doesn't exist
	h.Send("RCPT TO:<nobody@example.com>")
	_, err := h.Expect(icesmtp.Reply550MailboxUnavailable)
	if err != nil {
		t.Fatalf("expected 550 for unknown recipient: %v", err)
	}
}

func TestHarness_BadSequence(t *testing.T) {
	h := NewHarness()
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	h.Expect(icesmtp.Reply220ServiceReady)

	// Try MAIL before EHLO
	h.Send("MAIL FROM:<sender@example.com>")
	_, err := h.Expect(icesmtp.Reply503BadSequence)
	if err != nil {
		t.Fatalf("expected 503 for bad sequence: %v", err)
	}
}

func TestHarness_RSET(t *testing.T) {
	h := NewHarness()
	h.Mailbox.AddAddress("recipient@example.com")
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	h.Expect(icesmtp.Reply220ServiceReady)

	h.Send("EHLO client.example.com")
	h.Expect(icesmtp.Reply250OK)

	h.Send("MAIL FROM:<sender@example.com>")
	h.Expect(icesmtp.Reply250OK)

	h.Send("RCPT TO:<recipient@example.com>")
	h.Expect(icesmtp.Reply250OK)

	// RSET should reset the transaction
	h.Send("RSET")
	h.Expect(icesmtp.Reply250OK)

	// Now RCPT should fail because we're not in a transaction
	h.Send("RCPT TO:<recipient@example.com>")
	_, err := h.Expect(icesmtp.Reply503BadSequence)
	if err != nil {
		t.Fatalf("expected 503 after RSET: %v", err)
	}
}

func TestHarness_MultipleRecipients(t *testing.T) {
	h := NewHarness()
	h.Mailbox.AddAddress("recipient1@example.com")
	h.Mailbox.AddAddress("recipient2@example.com")
	h.Mailbox.AddAddress("recipient3@example.com")
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	h.Expect(icesmtp.Reply220ServiceReady)

	h.Send("EHLO client.example.com")
	h.Expect(icesmtp.Reply250OK)

	h.Send("MAIL FROM:<sender@example.com>")
	h.Expect(icesmtp.Reply250OK)

	h.Send("RCPT TO:<recipient1@example.com>")
	h.Expect(icesmtp.Reply250OK)

	h.Send("RCPT TO:<recipient2@example.com>")
	h.Expect(icesmtp.Reply250OK)

	h.Send("RCPT TO:<recipient3@example.com>")
	h.Expect(icesmtp.Reply250OK)

	h.Send("DATA")
	h.Expect(icesmtp.Reply354StartMailInput)

	h.Send("Subject: Multi-recipient test")
	h.Send("")
	h.Send("Message to multiple recipients.")
	h.Send(".")

	h.Expect(icesmtp.Reply250OK)

	h.Send("QUIT")
	h.Expect(icesmtp.Reply221ServiceClosing)

	// Check recipients
	messages := h.Messages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	if messages[0].Envelope.RecipientCount() != 3 {
		t.Errorf("expected 3 recipients, got %d", messages[0].Envelope.RecipientCount())
	}
}

func TestHarness_HELO(t *testing.T) {
	h := NewHarness()
	h.Mailbox.AddAddress("user@example.com")
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	h.Expect(icesmtp.Reply220ServiceReady)

	// Use HELO instead of EHLO
	h.Send("HELO client.example.com")
	_, err := h.Expect(icesmtp.Reply250OK)
	if err != nil {
		t.Fatalf("HELO: %v", err)
	}

	h.Send("QUIT")
	h.Expect(icesmtp.Reply221ServiceClosing)
}

func TestHarness_NOOP(t *testing.T) {
	h := NewHarness()
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	h.Expect(icesmtp.Reply220ServiceReady)

	h.Send("EHLO client.example.com")
	h.Expect(icesmtp.Reply250OK)

	// NOOP should succeed
	h.Send("NOOP")
	_, err := h.Expect(icesmtp.Reply250OK)
	if err != nil {
		t.Fatalf("NOOP: %v", err)
	}

	h.Send("QUIT")
	h.Expect(icesmtp.Reply221ServiceClosing)
}

func TestHarness_UnknownCommand(t *testing.T) {
	h := NewHarness()
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	h.Expect(icesmtp.Reply220ServiceReady)

	h.Send("EHLO client.example.com")
	h.Expect(icesmtp.Reply250OK)

	// Unknown command
	h.Send("XYZZY")
	_, err := h.Expect(icesmtp.Reply500SyntaxError)
	if err != nil {
		t.Fatalf("expected 500 for unknown command: %v", err)
	}
}

func TestHarness_MultipleTransactions(t *testing.T) {
	h := NewHarness()
	h.Mailbox.AddAddress("user@example.com")
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	h.Expect(icesmtp.Reply220ServiceReady)

	h.Send("EHLO client.example.com")
	h.Expect(icesmtp.Reply250OK)

	// First transaction
	h.Send("MAIL FROM:<sender1@example.com>")
	h.Expect(icesmtp.Reply250OK)
	h.Send("RCPT TO:<user@example.com>")
	h.Expect(icesmtp.Reply250OK)
	h.Send("DATA")
	h.Expect(icesmtp.Reply354StartMailInput)
	h.Send("Subject: First message")
	h.Send("")
	h.Send("First.")
	h.Send(".")
	h.Expect(icesmtp.Reply250OK)

	// Second transaction
	h.Send("MAIL FROM:<sender2@example.com>")
	h.Expect(icesmtp.Reply250OK)
	h.Send("RCPT TO:<user@example.com>")
	h.Expect(icesmtp.Reply250OK)
	h.Send("DATA")
	h.Expect(icesmtp.Reply354StartMailInput)
	h.Send("Subject: Second message")
	h.Send("")
	h.Send("Second.")
	h.Send(".")
	h.Expect(icesmtp.Reply250OK)

	h.Send("QUIT")
	h.Expect(icesmtp.Reply221ServiceClosing)

	if h.MessageCount() != 2 {
		t.Errorf("expected 2 messages, got %d", h.MessageCount())
	}
}

func TestHarness_NullSender(t *testing.T) {
	h := NewHarness()
	h.Mailbox.AddAddress("user@example.com")
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	h.Expect(icesmtp.Reply220ServiceReady)

	h.Send("EHLO client.example.com")
	h.Expect(icesmtp.Reply250OK)

	// Null sender (for bounces)
	h.Send("MAIL FROM:<>")
	_, err := h.Expect(icesmtp.Reply250OK)
	if err != nil {
		t.Fatalf("MAIL FROM:<>: %v", err)
	}

	h.Send("RCPT TO:<user@example.com>")
	h.Expect(icesmtp.Reply250OK)

	h.Send("DATA")
	h.Expect(icesmtp.Reply354StartMailInput)
	h.Send("Subject: Bounce notification")
	h.Send("")
	h.Send("This is a bounce.")
	h.Send(".")
	h.Expect(icesmtp.Reply250OK)

	h.Send("QUIT")
	h.Expect(icesmtp.Reply221ServiceClosing)

	messages := h.Messages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	if !messages[0].Envelope.MailFrom().IsNull {
		t.Error("expected null sender")
	}
}

func TestHarness_DotStuffing(t *testing.T) {
	h := NewHarness()
	h.Mailbox.AddAddress("user@example.com")
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.Start(ctx)

	h.Expect(icesmtp.Reply220ServiceReady)

	h.Send("EHLO client.example.com")
	h.Expect(icesmtp.Reply250OK)

	h.Send("MAIL FROM:<sender@example.com>")
	h.Expect(icesmtp.Reply250OK)

	h.Send("RCPT TO:<user@example.com>")
	h.Expect(icesmtp.Reply250OK)

	h.Send("DATA")
	h.Expect(icesmtp.Reply354StartMailInput)

	h.Send("Subject: Dot test")
	h.Send("")
	h.Send("A line starting with dot:")
	h.Send("..This line started with a dot")
	h.Send("Normal line")
	h.Send(".")

	h.Expect(icesmtp.Reply250OK)

	h.Send("QUIT")
	h.Expect(icesmtp.Reply221ServiceClosing)

	messages := h.Messages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	data := string(messages[0].Data)
	if !containsString(data, ".This line started with a dot") {
		t.Errorf("dot-stuffed line not properly unstuffed: %s", data)
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestHarness_RunConversation(t *testing.T) {
	h := NewHarness()
	h.Mailbox.AddAddress("user@example.com")
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	script := []ConversationStep{
		{Description: "Greeting", ExpectAny: true},
		{Description: "EHLO", Send: "EHLO client.example.com", Expect: icesmtp.Reply250OK},
		{Description: "MAIL FROM", Send: "MAIL FROM:<sender@example.com>", Expect: icesmtp.Reply250OK},
		{Description: "RCPT TO", Send: "RCPT TO:<user@example.com>", Expect: icesmtp.Reply250OK},
		{Description: "DATA", Send: "DATA", Expect: icesmtp.Reply354StartMailInput},
		{Description: "Message", Send: "Subject: Test\r\n\r\nBody.", Expect: icesmtp.ReplyCode(0)},
	}

	// Note: This test is simplified; full conversation scripts would be more complex
	_ = script

	// For now just verify the harness can be created and started
	h.Start(ctx)
	h.Expect(icesmtp.Reply220ServiceReady)
	h.Send("QUIT")
	h.Expect(icesmtp.Reply221ServiceClosing)
}
