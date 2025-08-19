package puller

import (
	"errors"
	"sync"
)

// Common errors for the puller package.
// Defines reusable error values for decoding and context operations.
// Used across Reader and Streamer methods for consistent error handling.
var (
	ErrInvalidPointer          = errors.New("must provide a non-nil pointer")
	ErrInvalidStringPointer    = errors.New("TXT requires a pointer to string")
	ErrInvalidByteSlicePointer = errors.New("BYTE/B64 requires a pointer to byte slice")
	ErrContextCanceled         = errors.New("operation canceled by context")
	ErrReadAllFailed           = errors.New("failed to read all data")
	ErrDecodingFailed          = errors.New("failed to decode data")
	// Streaming-specific errors
	errMsgPackStreaming  = errors.New("MessagePack streaming error")
	errJSONStreaming     = errors.New("JSON streaming error")
	errXMLStreaming      = errors.New("XML streaming error")
	errCallbackStreaming = errors.New("callback error during streaming")
	errReadStreaming     = errors.New("read error during streaming")
	// Decoding-specific errors
	errMsgPackDecoding = errors.New("MessagePack decoding error")
	errJSONDecoding    = errors.New("JSON decoding error")
	errXMLDecoding     = errors.New("XML decoding error")
	errB64Decoding     = errors.New("base64 decoding error")
)

// Config holds package configuration.
// Stores settings for buffer sizes and streaming thresholds.
// Used to customize Reader and Streamer behavior.
type Config struct {
	DefaultBufferSize     int // Default chunk size for streaming operations.
	LargeContentThreshold int // Content size threshold to favor streaming.
	InitialBufferCapacity int // Initial capacity for pooled buffers.
}

// Global package configuration with sensible defaults.
// Provides default values for buffer sizes and thresholds.
// Modified via SetConfig to adjust package behavior.
var config = Config{
	DefaultBufferSize:     32 * 1024,   // 32KB
	LargeContentThreshold: 1024 * 1024, // 1MB
	InitialBufferCapacity: 4096,        // 4KB
}

// byteBufferPool reuses buffers for reading operations.
// Provides a sync.Pool for byte slices with initial capacity.
// Used in streaming methods to reduce memory allocations.
var byteBufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, config.InitialBufferCapacity)
	},
}

// SetConfig updates the package configuration.
// Takes a Config struct with desired settings.
// Updates non-zero fields in the global config.
func SetConfig(cfg Config) {
	if cfg.DefaultBufferSize > 0 {
		config.DefaultBufferSize = cfg.DefaultBufferSize
	}
	if cfg.LargeContentThreshold > 0 {
		config.LargeContentThreshold = cfg.LargeContentThreshold
	}
	if cfg.InitialBufferCapacity > 0 {
		config.InitialBufferCapacity = cfg.InitialBufferCapacity
	}
}
