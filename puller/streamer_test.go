package puller

import (
	"bytes"
	"context"
	"errors"
	"github.com/vmihailenco/msgpack/v5"
	"strings"
	"testing"
	"time"
)

// infiniteReader is an io.Reader that never ends (for testing cancellation)
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestStreamerBytes(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		data := []byte("large data stream")
		s := NewStreamer(bytes.NewReader(data))

		var received []byte
		err := s.Bytes(func(chunk []byte) error {
			received = append(received, chunk...)
			return nil
		}, 4) // Small buffer size for testing

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !bytes.Equal(received, data) {
			t.Errorf("Expected %q, got %q", data, received)
		}
	})

	t.Run("CustomBufferSize", func(t *testing.T) {
		SetConfig(Config{DefaultBufferSize: 2})
		defer SetConfig(Config{DefaultBufferSize: 32 * 1024})

		data := []byte("abcd")
		s := NewStreamer(bytes.NewReader(data))

		var calls int
		err := s.Bytes(func(chunk []byte) error {
			calls++
			if len(chunk) > 2 {
				t.Errorf("Expected chunk size <= 2, got %d", len(chunk))
			}
			return nil
		}, 0) // 0 means use default

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if calls < 2 { // Should take at least 2 calls to read 4 bytes with 2-byte buffer
			t.Errorf("Expected at least 2 calls, got %d", calls)
		}
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		s := NewStreamerWithContext(ctx, infiniteReader{})

		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		err := s.Bytes(func(chunk []byte) error {
			return nil
		}, 1024)

		if !errors.Is(err, ErrContextCanceled) {
			t.Errorf("Expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestStreamerMsgPack(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		var buf bytes.Buffer
		enc := msgpack.NewEncoder(&buf)
		_ = enc.Encode(map[string]interface{}{"a": 1})
		_ = enc.Encode(map[string]interface{}{"b": 2})

		s := NewStreamer(&buf)
		var count int
		err := s.MsgPack(func(dec *msgpack.Decoder) error {
			var m map[string]interface{}
			if err := dec.Decode(&m); err != nil {
				return err
			}
			count++
			return nil
		})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if count != 2 {
			t.Errorf("Expected 2 items, got %d", count)
		}
	})
}

func TestReaderText(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		data := "hello world"
		r := NewReader(strings.NewReader(data))

		var result string
		err := r.Text(&result)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result != data {
			t.Errorf("Expected %q, got %q", data, result)
		}
	})

	t.Run("InvalidPointer", func(t *testing.T) {
		r := NewReader(strings.NewReader("data"))
		err := r.Text(nil)
		if !errors.Is(err, ErrInvalidStringPointer) {
			t.Errorf("Expected ErrInvalidStringPointer, got %v", err)
		}
	})
}
