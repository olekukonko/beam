package puller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"github.com/vmihailenco/msgpack/v5"
	"io"
	"reflect"
)

// Reader wraps an io.Reader (and optionally an io.Closer) and supports context cancellation.
// Provides one-shot decoding for JSON, XML, MessagePack, and raw data.
// Closes the reader if it implements io.Closer after processing.
type Reader struct {
	r      io.Reader
	closer io.Closer
	ctx    context.Context
}

// NewReader creates a new Reader instance.
// Takes an io.Reader to read data from.
// Returns a *Reader, setting closer if the reader implements io.Closer.
func NewReader(r io.Reader) *Reader {
	var c io.Closer
	if rc, ok := r.(io.Closer); ok {
		c = rc
	}
	return &Reader{r: r, closer: c}
}

// NewPullerWithContext creates a new Reader with cancellation support.
// Takes a context.Context and an io.Reader for reading.
// Returns a *Reader with context and optional closer initialized.
func NewPullerWithContext(ctx context.Context, r io.Reader) *Reader {
	rd := NewReader(r)
	rd.ctx = ctx
	return rd
}

// Pull reads all data from the underlying reader.
// Reads all available data into a byte slice.
// Returns the data or an error if reading or context fails.
func (r *Reader) Pull() ([]byte, error) {
	if err := r.checkContext(); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(r.r)
	// Ensure we close the resource after reading.
	if r.closer != nil {
		_ = r.closer.Close()
	}

	if err != nil {
		if r.ctx != nil && errors.Is(err, context.Canceled) {
			return nil, ErrContextCanceled
		}
		return nil, errors.Join(ErrReadAllFailed, err)
	}
	return data, nil
}

// PULL reads all data from the underlying reader.
// Deprecated: use Pull instead for reading all data.
// Returns the data or an error if reading or context fails.
func (r *Reader) PULL() ([]byte, error) {
	return r.Pull()
}

// MsgPack decodes MessagePack data into the provided pointer.
// Takes a pointer to a value for MessagePack decoding.
// Returns an error if decoding, context, or pointer validation fails.
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
		_ = r.closer.Close()
	}
	if err != nil {
		return errors.Join(errMsgPackDecoding, err)
	}
	return nil
}

// JSON decodes JSON data into the provided pointer.
// Takes a pointer to a value for JSON decoding.
// Returns an error if decoding, context, or pointer validation fails.
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
		_ = r.closer.Close()
	}
	if err != nil {
		return errors.Join(errJSONDecoding, err)
	}
	return nil
}

// XML decodes XML data into the provided pointer.
// Takes a pointer to a value for XML decoding.
// Returns an error if decoding, context, or pointer validation fails.
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
		_ = r.closer.Close()
	}
	if err != nil {
		return errors.Join(errXMLDecoding, err)
	}
	return nil
}

// B64 decodes Base64 data into the provided byte slice pointer.
// Takes a pointer to a byte slice for Base64 decoding.
// Returns an error if decoding, context, or pointer validation fails.
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
		return errors.Join(errB64Decoding, err)
	}
	rv.Elem().SetBytes(decoded[:n])
	return nil
}

// Byte reads raw bytes into the provided byte slice pointer.
// Takes a pointer to a byte slice for raw data.
// Returns an error if reading, context, or pointer validation fails.
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
// Takes a pointer to a string for text data.
// Returns an error if reading, context, or pointer validation fails.
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
// Checks if the Reader's context is non-nil and canceled.
// Returns ErrContextCanceled if canceled, nil otherwise.
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

// validatePointer ensures that v is a non-nil pointer.
// Takes an interface{} to validate as a pointer.
// Returns ErrInvalidPointer if v is nil or not a pointer, nil otherwise.
func validatePointer(v interface{}) error {
	if v == nil || reflect.ValueOf(v).Kind() != reflect.Ptr {
		return ErrInvalidPointer
	}
	return nil
}
