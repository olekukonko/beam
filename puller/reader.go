package puller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"

	"gopkg.in/vmihailenco/msgpack.v2"
)

// Common errors
var (
	ErrInvalidPointer          = errors.New("must provide a non-nil pointer")
	ErrInvalidStringPointer    = errors.New("TXT requires a pointer to string")
	ErrInvalidByteSlicePointer = errors.New("BYTE/B64 requires a pointer to byte slice")
	ErrContextCanceled         = errors.New("operation canceled by context")
	ErrReadAllFailed           = errors.New("failed to read all data")
	ErrDecodingFailed          = errors.New("failed to decode data")
	ErrMsgPackUnsupported      = errors.New("MessagePack format not supported")
)

// Config holds package configuration.
type Config struct {
	DefaultBufferSize     int // Default chunk size for streaming operations.
	LargeContentThreshold int // Content size threshold to favor streaming.
	InitialBufferCapacity int // Initial capacity for pooled buffers.
}

// Global package configuration with sensible defaults.
var config = Config{
	DefaultBufferSize:     32 * 1024,   // 32KB
	LargeContentThreshold: 1024 * 1024, // 1MB
	InitialBufferCapacity: 4096,        // 4KB
}

// SetConfig updates the package configuration.
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

// byteBufferPool reuses buffers for reading operations.
var byteBufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, config.InitialBufferCapacity)
	},
}

// -----------------------------------------------------------------------------
// Reader: One-shot operations for complete data decoding.
// -----------------------------------------------------------------------------

// Reader wraps an io.Reader (and optionally an io.Closer) and supports context cancellation.
type Reader struct {
	r      io.Reader
	closer io.Closer
	ctx    context.Context
}

// NewReader creates a new Reader instance.
func NewReader(r io.Reader) *Reader {
	var c io.Closer
	if rc, ok := r.(io.Closer); ok {
		c = rc
	}
	return &Reader{r: r, closer: c}
}

// NewPullerWithContext creates a new Reader with cancellation support.
func NewPullerWithContext(ctx context.Context, r io.Reader) *Reader {
	rd := NewReader(r)
	rd.ctx = ctx
	return rd
}

// PULL reads all data from the underlying reader.
func (r *Reader) PULL() ([]byte, error) {
	if err := r.checkContext(); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(r.r)
	// Ensure we close the resource after reading.
	if r.closer != nil {
		r.closer.Close()
	}

	if err != nil {
		if r.ctx != nil && errors.Is(err, context.Canceled) {
			return nil, ErrContextCanceled
		}
		return nil, fmt.Errorf("%w: %v", ErrReadAllFailed, err)
	}
	return data, nil
}

// MsgPack decodes MessagePack data into the provided pointer.
func (r *Reader) MsgPack(v interface{}) error {
	if err := validatePointer(v); err != nil {
		return err
	}
	if err := r.checkContext(); err != nil {
		return err
	}

	decoder := msgpack.NewDecoder(r.r)
	err := decoder.Decode(v)
	if r.closer != nil {
		r.closer.Close()
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDecodingFailed, err)
	}
	return nil
}

// JSON decodes JSON data into the provided pointer.
func (r *Reader) JSON(v interface{}) error {
	if err := validatePointer(v); err != nil {
		return err
	}
	if err := r.checkContext(); err != nil {
		return err
	}

	decoder := json.NewDecoder(r.r)
	err := decoder.Decode(v)
	if r.closer != nil {
		r.closer.Close()
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDecodingFailed, err)
	}
	return nil
}

// XML decodes XML data into the provided pointer.
func (r *Reader) XML(v interface{}) error {
	if err := validatePointer(v); err != nil {
		return err
	}
	if err := r.checkContext(); err != nil {
		return err
	}

	decoder := xml.NewDecoder(r.r)
	err := decoder.Decode(v)
	if r.closer != nil {
		r.closer.Close()
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDecodingFailed, err)
	}
	return nil
}

// B64 decodes Base64 data into the provided byte slice pointer.
func (r *Reader) B64(v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Slice {
		return ErrInvalidByteSlicePointer
	}

	data, err := r.PULL()
	if err != nil {
		return err
	}

	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
	n, err := base64.StdEncoding.Decode(decoded, data)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDecodingFailed, err)
	}
	rv.Elem().SetBytes(decoded[:n])
	return nil
}

// Byte reads raw bytes into the provided byte slice pointer.
func (r *Reader) Byte(v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Slice {
		return ErrInvalidByteSlicePointer
	}

	data, err := r.PULL()
	if err != nil {
		return err
	}
	rv.Elem().SetBytes(data)
	return nil
}

// Text reads data as a string into the provided string pointer.
func (r *Reader) Text(v *string) error {
	if v == nil {
		return ErrInvalidStringPointer
	}

	data, err := r.PULL()
	if err != nil {
		return err
	}
	*v = string(data)
	return nil
}

// checkContext verifies whether the context has been canceled.
func (r *Reader) checkContext() error {
	if r.ctx != nil {
		select {
		case <-r.ctx.Done():
			return ErrContextCanceled
		default:
		}
	}
	return nil
}
