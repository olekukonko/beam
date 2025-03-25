package puller

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"gopkg.in/vmihailenco/msgpack.v2"
	"io"
	"reflect"
)

// -----------------------------------------------------------------------------
// Streamer: Streaming operations for processing large or continuous data.
// -----------------------------------------------------------------------------

// Streamer provides efficient streaming operations on an io.Reader.
// If the underlying reader implements io.Closer, it will be closed after processing.
type Streamer struct {
	r      io.Reader
	closer io.Closer
	ctx    context.Context
}

// NewStreamer creates a new Streamer instance.
func NewStreamer(r io.Reader) *Streamer {
	var c io.Closer
	if rc, ok := r.(io.Closer); ok {
		c = rc
	}
	return &Streamer{r: r, closer: c}
}

// NewStreamerWithContext creates a new Streamer with cancellation support.
func NewStreamerWithContext(ctx context.Context, r io.Reader) *Streamer {
	st := NewStreamer(r)
	st.ctx = ctx
	return st
}

// MsgPack streams MessagePack data using the provided callback.
// The callback is invoked repeatedly until io.EOF or an error is returned.
func (s *Streamer) MsgPack(callback func(*msgpack.Decoder) error) error {
	decoder := msgpack.NewDecoder(s.r)
	defer s.close()

	for {
		if err := s.checkContext(); err != nil {
			return err
		}

		if err := callback(decoder); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("MessagePack streaming error: %w", err)
		}
	}
}

// JSON streams JSON data using the provided callback.
// The callback is invoked repeatedly until io.EOF or an error is returned.
func (s *Streamer) JSON(callback func(*json.Decoder) error) error {
	decoder := json.NewDecoder(s.r)
	defer s.close()

	for {
		if err := s.checkContext(); err != nil {
			return err
		}

		if err := callback(decoder); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("JSON streaming error: %w", err)
		}
	}
}

// XML streams XML data using the provided callback.
// The callback is invoked repeatedly until io.EOF or an error is returned.
func (s *Streamer) XML(callback func(*xml.Decoder) error) error {
	decoder := xml.NewDecoder(s.r)
	defer s.close()

	for {
		if err := s.checkContext(); err != nil {
			return err
		}

		if err := callback(decoder); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("XML streaming error: %w", err)
		}
	}
}

// Bytes streams raw data in chunks and calls the provided callback for each chunk.
// If bufSize is not positive, the default buffer size from config is used.
func (s *Streamer) Bytes(callback func([]byte) error, bufSize int) error {
	if bufSize <= 0 {
		bufSize = config.DefaultBufferSize
	}
	buf := make([]byte, bufSize)
	defer s.close()

	for {
		if err := s.checkContext(); err != nil {
			return err
		}

		n, err := s.r.Read(buf)
		if n > 0 {
			if err := callback(buf[:n]); err != nil {
				return fmt.Errorf("callback error during streaming: %w", err)
			}
		}

		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read error during streaming: %w", err)
		}
	}
}

// checkContext verifies whether the context has been canceled.
func (s *Streamer) checkContext() error {
	if s.ctx != nil {
		select {
		case <-s.ctx.Done():
			return ErrContextCanceled
		default:
		}
	}
	return nil
}

// close closes the underlying resource if it implements io.Closer.
func (s *Streamer) close() {
	if s.closer != nil {
		s.closer.Close()
	}
}

// validatePointer ensures that v is a non-nil pointer.
func validatePointer(v interface{}) error {
	if v == nil || reflect.ValueOf(v).Kind() != reflect.Ptr {
		return ErrInvalidPointer
	}
	return nil
}
