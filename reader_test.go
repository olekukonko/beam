package beam

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"gopkg.in/vmihailenco/msgpack.v2"
	"strings"
	"testing"
	"time"
)

// trackingEncoder wraps an Encoder to track calls
type trackingEncoder struct {
	Encoder
	called bool
}

func (te *trackingEncoder) Marshal(v interface{}) ([]byte, error) {
	te.called = true
	return te.Encoder.Marshal(v)
}

func TestConfig(t *testing.T) {
	originalConfig := config

	t.Run("DefaultConfig", func(t *testing.T) {
		if config.DefaultBufferSize != 32*1024 {
			t.Errorf("Expected default buffer size 32KB, got %d", config.DefaultBufferSize)
		}
	})

	t.Run("CustomConfig", func(t *testing.T) {
		SetConfig(Config{
			DefaultBufferSize:     64 * 1024,
			LargeContentThreshold: 2 * 1024 * 1024,
			InitialBufferCapacity: 8192,
		})

		if config.DefaultBufferSize != 64*1024 {
			t.Error("Failed to set DefaultBufferSize")
		}
		if config.LargeContentThreshold != 2*1024*1024 {
			t.Error("Failed to set LargeContentThreshold")
		}
		if config.InitialBufferCapacity != 8192 {
			t.Error("Failed to set InitialBufferCapacity")
		}
	})

	// Restore original config
	config = originalConfig
}

func TestReaderPULL(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		data := []byte("test data")
		r := NewReader(bytes.NewReader(data))
		result, err := r.PULL()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !bytes.Equal(result, data) {
			t.Errorf("Expected %q, got %q", data, result)
		}
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := NewReaderWithContext(ctx, strings.NewReader("data"))
		_, err := r.PULL()
		if !errors.Is(err, ErrContextCanceled) {
			t.Errorf("Expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestReaderMsgPack(t *testing.T) {
	type testStruct struct {
		Name string
		Age  int
	}

	t.Run("Success", func(t *testing.T) {
		data := testStruct{Name: "Alice", Age: 30}
		var buf bytes.Buffer
		enc := msgpack.NewEncoder(&buf)
		_ = enc.Encode(data)

		var result testStruct
		r := NewReader(&buf)
		err := r.MsgPack(&result)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result != data {
			t.Errorf("Expected %+v, got %+v", data, result)
		}
	})

	t.Run("InvalidPointer", func(t *testing.T) {
		r := NewReader(strings.NewReader("data"))
		err := r.MsgPack("not a pointer")
		if !errors.Is(err, ErrInvalidPointer) {
			t.Errorf("Expected ErrInvalidPointer, got %v", err)
		}
	})
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

// infiniteReader is an io.Reader that never ends (for testing cancellation)
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestReaderJSON(t *testing.T) {
	type testStruct struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	t.Run("Success", func(t *testing.T) {
		data := testStruct{Name: "Bob", Age: 25}
		jsonData, _ := json.Marshal(data)

		var result testStruct
		r := NewReader(bytes.NewReader(jsonData))
		err := r.JSON(&result)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result != data {
			t.Errorf("Expected %+v, got %+v", data, result)
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		r := NewReader(strings.NewReader("invalid json"))
		var result struct{}
		err := r.JSON(&result)
		if err == nil {
			t.Error("Expected error for invalid JSON")
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

func TestReaderB64(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		data := []byte("original data")
		encoded := base64.StdEncoding.EncodeToString(data)

		var result []byte
		r := NewReader(strings.NewReader(encoded))
		err := r.B64(&result)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !bytes.Equal(result, data) {
			t.Errorf("Expected %q, got %q", data, result)
		}
	})

	t.Run("InvalidBase64", func(t *testing.T) {
		r := NewReader(strings.NewReader("invalid base64"))
		var result []byte
		err := r.B64(&result)
		if err == nil {
			t.Error("Expected error for invalid base64")
		}
	})
}
