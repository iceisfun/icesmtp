package icesmtp

import (
	"context"
	"io"
)

// Storage defines the interface for durable message storage.
// Implementations may persist to disk, database, message queue, or any backend.
type Storage interface {
	// Store persists a finalized envelope.
	// The context may be used for timeouts and cancellation.
	// Returns a receipt on success or an error on failure.
	Store(ctx context.Context, envelope Envelope) (StorageReceipt, error)

	// StoreStream persists an envelope with streaming data.
	// This is useful for large messages that should not be fully buffered.
	// The reader provides the message data; metadata comes from the envelope.
	StoreStream(ctx context.Context, envelope Envelope, data io.Reader) (StorageReceipt, error)
}

// StorageReceipt is returned on successful storage and contains
// information about the stored message.
type StorageReceipt struct {
	// MessageID is a unique identifier assigned by the storage backend.
	// This may differ from the EnvelopeID.
	MessageID StorageMessageID

	// EnvelopeID is the original envelope identifier.
	EnvelopeID EnvelopeID

	// StoredAt is the time the message was stored (if available).
	StoredAt Timestamp

	// BytesWritten is the number of bytes stored.
	BytesWritten ByteCount

	// Backend contains implementation-specific receipt data.
	Backend StorageBackendReceipt
}

// StorageMessageID is the identifier assigned by the storage backend.
type StorageMessageID = string

// Timestamp represents a Unix timestamp.
type Timestamp = int64

// ByteCount represents a count of bytes.
type ByteCount = int64

// StorageBackendReceipt contains implementation-specific storage receipt data.
// Implementations may type-assert this to their specific receipt type.
type StorageBackendReceipt interface{}

// StorageError represents an error from the storage backend.
type StorageError struct {
	// Operation is the storage operation that failed.
	Operation StorageOperation

	// EnvelopeID is the envelope that was being stored.
	EnvelopeID EnvelopeID

	// Cause is the underlying error.
	Cause error

	// Retryable indicates whether the operation may succeed if retried.
	Retryable bool

	// Message is a human-readable error message.
	Message string
}

// StorageOperation identifies a storage operation.
type StorageOperation = string

const (
	// StorageOpStore is the Store operation.
	StorageOpStore StorageOperation = "Store"

	// StorageOpStoreStream is the StoreStream operation.
	StorageOpStoreStream StorageOperation = "StoreStream"
)

func (e *StorageError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *StorageError) Unwrap() error {
	return e.Cause
}

// StorageHook provides optional callbacks for storage events.
// Implementations may use these for logging, metrics, or side effects.
type StorageHook interface {
	// BeforeStore is called before storing an envelope.
	// Returning an error aborts the store operation.
	BeforeStore(ctx context.Context, envelope Envelope) error

	// AfterStore is called after successfully storing an envelope.
	AfterStore(ctx context.Context, envelope Envelope, receipt StorageReceipt)

	// OnStoreError is called when a store operation fails.
	OnStoreError(ctx context.Context, envelope Envelope, err error)
}

// StorageMetrics provides storage statistics.
type StorageMetrics struct {
	// MessagesStored is the total number of messages stored.
	MessagesStored CounterValue

	// BytesStored is the total bytes stored.
	BytesStored CounterValue

	// StoreErrors is the count of failed store operations.
	StoreErrors CounterValue

	// StoreLatencyNs is the last store operation latency in nanoseconds.
	StoreLatencyNs DurationNs
}

// CounterValue is a monotonically increasing counter.
type CounterValue = uint64

// DurationNs is a duration in nanoseconds.
type DurationNs = int64

// StorageWithMetrics extends Storage with metrics access.
type StorageWithMetrics interface {
	Storage

	// Metrics returns current storage metrics.
	Metrics() StorageMetrics
}

// StorageWithHealth extends Storage with health checking.
type StorageWithHealth interface {
	Storage

	// Healthy returns nil if the storage backend is healthy.
	Healthy(ctx context.Context) error
}

// NullStorage is a Storage implementation that discards all messages.
// Useful for testing or when storage is not needed.
type NullStorage struct{}

// Store discards the envelope and returns a successful receipt.
func (NullStorage) Store(_ context.Context, envelope Envelope) (StorageReceipt, error) {
	return StorageReceipt{
		MessageID:    "null",
		EnvelopeID:   envelope.ID(),
		BytesWritten: envelope.DataSize(),
	}, nil
}

// StoreStream discards the data and returns a successful receipt.
func (NullStorage) StoreStream(_ context.Context, envelope Envelope, data io.Reader) (StorageReceipt, error) {
	n, _ := io.Copy(io.Discard, data)
	return StorageReceipt{
		MessageID:    "null",
		EnvelopeID:   envelope.ID(),
		BytesWritten: n,
	}, nil
}
