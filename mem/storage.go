// Package mem provides in-memory implementations of icesmtp interfaces.
// These are suitable for testing and development but not production use.
package mem

import (
	"context"
	"io"
	"sync"
	"time"

	"icesmtp"
)

// Storage is an in-memory Storage implementation.
// Messages are stored in a map and can be retrieved for inspection.
// This is useful for testing but should not be used in production.
type Storage struct {
	mu       sync.RWMutex
	messages map[icesmtp.EnvelopeID]*StoredMessage
	metrics  icesmtp.StorageMetrics
}

// StoredMessage represents a message stored in memory.
type StoredMessage struct {
	// Envelope contains the envelope metadata.
	Envelope icesmtp.Envelope

	// StoredAt is when the message was stored.
	StoredAt time.Time

	// Data is the raw message data.
	Data []byte
}

// NewStorage creates a new in-memory storage.
func NewStorage() *Storage {
	return &Storage{
		messages: make(map[icesmtp.EnvelopeID]*StoredMessage),
	}
}

// Store stores an envelope in memory.
func (s *Storage) Store(ctx context.Context, envelope icesmtp.Envelope) (icesmtp.StorageReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := time.Now()

	msg := &StoredMessage{
		Envelope: envelope,
		StoredAt: time.Now(),
		Data:     envelope.Data(),
	}

	s.messages[envelope.ID()] = msg

	// Update metrics
	s.metrics.MessagesStored++
	s.metrics.BytesStored += uint64(len(msg.Data))
	s.metrics.StoreLatencyNs = int64(time.Since(start))

	return icesmtp.StorageReceipt{
		MessageID:    icesmtp.StorageMessageID(envelope.ID()),
		EnvelopeID:   envelope.ID(),
		StoredAt:     time.Now().Unix(),
		BytesWritten: int64(len(msg.Data)),
	}, nil
}

// StoreStream stores an envelope with streaming data.
func (s *Storage) StoreStream(ctx context.Context, envelope icesmtp.Envelope, data io.Reader) (icesmtp.StorageReceipt, error) {
	// Read all data into memory
	buf, err := io.ReadAll(data)
	if err != nil {
		s.mu.Lock()
		s.metrics.StoreErrors++
		s.mu.Unlock()
		return icesmtp.StorageReceipt{}, &icesmtp.StorageError{
			Operation:  icesmtp.StorageOpStoreStream,
			EnvelopeID: envelope.ID(),
			Cause:      err,
			Message:    "failed to read message data",
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	start := time.Now()

	msg := &StoredMessage{
		Envelope: envelope,
		StoredAt: time.Now(),
		Data:     buf,
	}

	s.messages[envelope.ID()] = msg

	// Update metrics
	s.metrics.MessagesStored++
	s.metrics.BytesStored += uint64(len(buf))
	s.metrics.StoreLatencyNs = int64(time.Since(start))

	return icesmtp.StorageReceipt{
		MessageID:    icesmtp.StorageMessageID(envelope.ID()),
		EnvelopeID:   envelope.ID(),
		StoredAt:     time.Now().Unix(),
		BytesWritten: int64(len(buf)),
	}, nil
}

// Get retrieves a stored message by envelope ID.
func (s *Storage) Get(id icesmtp.EnvelopeID) (*StoredMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msg, ok := s.messages[id]
	return msg, ok
}

// List returns all stored messages.
func (s *Storage) List() []*StoredMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*StoredMessage, 0, len(s.messages))
	for _, msg := range s.messages {
		result = append(result, msg)
	}
	return result
}

// Count returns the number of stored messages.
func (s *Storage) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages)
}

// Delete removes a message by envelope ID.
func (s *Storage) Delete(id icesmtp.EnvelopeID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.messages[id]; ok {
		delete(s.messages, id)
		return true
	}
	return false
}

// Clear removes all stored messages.
func (s *Storage) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = make(map[icesmtp.EnvelopeID]*StoredMessage)
}

// Metrics returns storage metrics.
func (s *Storage) Metrics() icesmtp.StorageMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metrics
}

// Healthy always returns nil for in-memory storage.
func (s *Storage) Healthy(ctx context.Context) error {
	return nil
}

// Ensure Storage implements the interfaces.
var (
	_ icesmtp.Storage            = (*Storage)(nil)
	_ icesmtp.StorageWithMetrics = (*Storage)(nil)
	_ icesmtp.StorageWithHealth  = (*Storage)(nil)
)
