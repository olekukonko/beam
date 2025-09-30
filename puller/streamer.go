package puller

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"

	"github.com/vmihailenco/msgpack/v5"
)

// Streamer provides efficient streaming operations on an io.Reader.
// Manages streaming of data formats like JSON, XML, or raw bytes.
// Closes the reader if it implements io.Closer after processing.
type Streamer struct {
	r      io.Reader
	closer io.Closer
	ctx    context.Context
}

// NewStreamer creates a new Streamer instance.
// Takes an io.Reader to stream data from.
// Returns a *Streamer, setting closer if the reader implements io.Closer.
func NewStreamer(r io.Reader) *Streamer {
	var c io.Closer
	if rc, ok := r.(io.Closer); ok {
		c = rc
	}
	return &Streamer{r: r, closer: c}
}

// NewStreamerWithContext creates a new Streamer with cancellation support.
// Takes a context.Context and an io.Reader for streaming.
// Returns a *Streamer with context and optional closer initialized.
func NewStreamerWithContext(ctx context.Context, r io.Reader) *Streamer {
	st := NewStreamer(r)
	st.ctx = ctx
	return st
}

// MsgPack streams MessagePack data using the provided callback.
// Takes a callback function to process MessagePack decoder output.
// Returns an error if streaming or callback fails, nil on io.EOF.
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
			return errors.Join(errMsgPackStreaming, err)
		}
	}
}

// JSON streams JSON data using the provided callback.
// Takes a callback function to process JSON decoder output.
// Returns an error if streaming or callback fails, nil on io.EOF.
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
			return errors.Join(errJSONStreaming, err)
		}
	}
}

// XML streams XML data using the provided callback.
// Takes a callback function to process XML decoder output.
// Returns an error if streaming or callback fails, nil on io.EOF.
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
			return errors.Join(errXMLStreaming, err)
		}
	}
}

// Bytes streams raw data in chunks and calls the provided callback for each chunk.
// Takes a callback to process byte chunks and an optional buffer size.
// Returns an error if reading or callback fails, nil on io.EOF.
func (s *Streamer) Bytes(callback func([]byte) error, bufSize int) error {
	if bufSize <= 0 {
		bufSize = config.DefaultBufferSize
	}
	buf := byteBufferPool.Get().([]byte)
	if cap(buf) < bufSize {
		buf = make([]byte, bufSize)
	} else {
		buf = buf[:bufSize]
	}
	defer byteBufferPool.Put(buf)
	defer s.close()

	for {
		if err := s.checkContext(); err != nil {
			return err
		}

		n, err := s.r.Read(buf)
		if n > 0 {
			if err := callback(buf[:n]); err != nil {
				return errors.Join(errCallbackStreaming, err)
			}
		}

		if err != nil {
			if err == io.EOF {
				return nil
			}
			return errors.Join(errReadStreaming, err)
		}
	}
}

// checkContext verifies whether the context has been canceled.
// Checks if the Streamer's context is non-nil and canceled.
// Returns ErrContextCanceled if canceled, nil otherwise.
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
// Closes the Streamer's closer field if non-nil.
// No return value; side effects only.
func (s *Streamer) close() {
	if s.closer != nil {
		_ = s.closer.Close()
	}
}
