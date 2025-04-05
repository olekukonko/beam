package puller

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/vmihailenco/msgpack/v5"
	"strings"
	"testing"
)

//// trackingEncoder wraps an Encoder to track calls
//type trackingEncoder struct {
//	beam.Encoder
//	called bool
//}
//
//func (te *trackingEncoder) Marshal(v interface{}) ([]byte, error) {
//	te.called = true
//	return te.Encoder.Marshal(v)
//}

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

func TestPullerPULL(t *testing.T) {
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
		r := NewPullerWithContext(ctx, strings.NewReader("data"))
		_, err := r.PULL()
		if !errors.Is(err, ErrContextCanceled) {
			t.Errorf("Expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestPullerMsgPack(t *testing.T) {
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

func TestPullerJSON(t *testing.T) {
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

func TestPullerB64(t *testing.T) {
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
