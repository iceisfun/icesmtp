package icesmtp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"icesmtp"
	"icesmtp/harness"
)

func TestStreamingLargeMessage(t *testing.T) {
	// Setup with default in-memory storage (fine for 10MB test)
	h := harness.NewHarness()
	h.Mailbox.AddAddress("user@example.com")
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	h.Start(ctx)

	// Conversation
	h.Expect(icesmtp.Reply220ServiceReady)
	h.Send("EHLO localhost")
	h.Expect(icesmtp.Reply250OK)

	h.Send("MAIL FROM:<sender@example.com>")
	h.Expect(icesmtp.Reply250OK)

	h.Send("RCPT TO:<user@example.com>")
	h.Expect(icesmtp.Reply250OK)

	h.Send("DATA")
	h.Expect(icesmtp.Reply354StartMailInput)

	// Generate 10MB of data
	chunkSize := 64
	chunk := strings.Repeat("A", chunkSize)
	// 10MB / 66 bytes (64 + \r\n) ~= 158900 lines
	totalBytes := 10 * 1024 * 1024
	numLines := totalBytes / (chunkSize + 2)

	// Write data directly to harness input
	for i := 0; i < numLines; i++ {
		h.Input.Write([]byte(chunk + "\r\n"))
	}
	h.Input.Write([]byte(".\r\n"))

	if _, err := h.Expect(icesmtp.Reply250OK); err != nil {
		t.Fatalf("DATA expected 250: %v", err)
	}

	h.Send("QUIT")
	if _, err := h.Expect(icesmtp.Reply221ServiceClosing); err != nil {
		t.Fatalf("QUIT expected 221: %v", err)
	}

	// Verify we have 1 message
	if h.MessageCount() != 1 {
		t.Errorf("expected 1 message, got %d", h.MessageCount())
		t.Logf("Process Errors: %v", h.Errors)
		t.Logf("Transcript:\n%s", h.Transcript.String())
	}
}
