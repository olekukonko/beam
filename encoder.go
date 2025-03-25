package beam

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"gopkg.in/vmihailenco/msgpack.v2"
)

// -----------------------------------------------------------------------------
// Content Type Constants
// -----------------------------------------------------------------------------

const (
	ContentTypeJSON           = "application/json"
	ContentTypeMsgPack        = "application/msgpack"
	ContentTypeXML            = "application/xml"
	ContentTypeText           = "text/plain"
	ContentTypeBinary         = "application/octet-stream"
	ContentTypeFormURLEncoded = "application/x-www-form-urlencoded"
	ContentTypeEventStream    = "text/event-stream"
	ContentTypePNG            = "image/png"
	ContentTypeJPEG           = "image/jpeg"
	ContentTypeGIF            = "image/gif"
	ContentTypeWebP           = "image/webp"
)

// -----------------------------------------------------------------------------
// Encoder Interface and Registry
// -----------------------------------------------------------------------------

// Encoder defines the interface for encoding data.
type Encoder interface {
	Marshal(v interface{}) ([]byte, error)
	Unmarshal(data []byte, v interface{}) error
	ContentType() string
}

// Streamer defines an optional interface for streaming data.
type Streamer interface {
	Stream(w Writer, callback func() (interface{}, error)) error
}

// EncoderRegistry manages content-type to encoder mappings.
type EncoderRegistry struct {
	mu       sync.RWMutex
	encoders map[string]Encoder
}

// NewEncoderRegistry initializes an EncoderRegistry with default encoders.
func NewEncoderRegistry() *EncoderRegistry {
	er := &EncoderRegistry{
		encoders: make(map[string]Encoder),
	}
	// Register default encoders
	er.Register(&JSONEncoder{})
	er.Register(&MsgPackEncoder{})
	er.Register(&XMLEncoder{})
	er.Register(&TextEncoder{})
	er.Register(&FormURLEncodedEncoder{})
	er.Register(&EventStreamEncoder{})
	return er
}

// Register adds an encoder to the registry.
func (er *EncoderRegistry) Register(e Encoder) {
	er.mu.Lock()
	defer er.mu.Unlock()
	er.encoders[e.ContentType()] = e
}

// Get retrieves an encoder by content type.
func (er *EncoderRegistry) Get(contentType string) (Encoder, bool) {
	er.mu.RLock()
	defer er.mu.RUnlock()
	e, ok := er.encoders[contentType]
	return e, ok
}

// Encode marshals data using the encoder for the given content type.
func (er *EncoderRegistry) Encode(contentType string, v interface{}) ([]byte, error) {
	e, ok := er.Get(contentType)
	if !ok {
		return nil, fmt.Errorf("no encoder for content type %s", contentType)
	}
	return e.Marshal(v)
}

// -----------------------------------------------------------------------------
// SSE Event Type
// -----------------------------------------------------------------------------

// Event represents a Server-Sent Events (SSE) event.
type Event struct {
	ID    string      `json:"id,omitempty"`
	Type  string      `json:"type,omitempty"`
	Data  interface{} `json:"data"`
	Retry int         `json:"retry,omitempty"`
}

// -----------------------------------------------------------------------------
// Default Encoder Implementations
// -----------------------------------------------------------------------------

type JSONEncoder struct{}

func (e *JSONEncoder) Marshal(v interface{}) ([]byte, error)      { return json.Marshal(v) }
func (e *JSONEncoder) Unmarshal(data []byte, v interface{}) error { return json.Unmarshal(data, v) }
func (e *JSONEncoder) ContentType() string                        { return ContentTypeJSON }

type MsgPackEncoder struct{}

func (e *MsgPackEncoder) Marshal(v interface{}) ([]byte, error) { return msgpack.Marshal(v) }
func (e *MsgPackEncoder) Unmarshal(data []byte, v interface{}) error {
	return msgpack.Unmarshal(data, v)
}
func (e *MsgPackEncoder) ContentType() string { return ContentTypeMsgPack }

type XMLEncoder struct{}

func (e *XMLEncoder) Marshal(v interface{}) ([]byte, error)      { return xml.Marshal(v) }
func (e *XMLEncoder) Unmarshal(data []byte, v interface{}) error { return xml.Unmarshal(data, v) }
func (e *XMLEncoder) ContentType() string                        { return ContentTypeXML }

type TextEncoder struct{}

func (e *TextEncoder) Marshal(v interface{}) ([]byte, error) {
	return []byte(fmt.Sprintf("%v", v)), nil
}
func (e *TextEncoder) Unmarshal(data []byte, v interface{}) error { return nil }
func (e *TextEncoder) ContentType() string                        { return ContentTypeText }

type FormURLEncodedEncoder struct{}

func (e *FormURLEncodedEncoder) Marshal(v interface{}) ([]byte, error) {
	if m, ok := v.(map[string]interface{}); ok {
		values := url.Values{}
		for k, val := range m {
			values.Set(k, fmt.Sprintf("%v", val))
		}
		return []byte(values.Encode()), nil
	}
	return nil, fmt.Errorf("requires map[string]interface{}")
}
func (e *FormURLEncodedEncoder) Unmarshal(data []byte, v interface{}) error { return nil }
func (e *FormURLEncodedEncoder) ContentType() string                        { return ContentTypeFormURLEncoded }

type EventStreamEncoder struct{}

func (e *EventStreamEncoder) Marshal(v interface{}) ([]byte, error) {
	if evt, ok := v.(Event); ok {
		var b strings.Builder
		if evt.ID != "" {
			b.WriteString(fmt.Sprintf("id: %s\n", evt.ID))
		}
		if evt.Type != "" {
			b.WriteString(fmt.Sprintf("event: %s\n", evt.Type))
		}
		data, err := json.Marshal(evt.Data)
		if err != nil {
			return nil, err
		}
		b.WriteString(fmt.Sprintf("data: %s\n", data))
		if evt.Retry > 0 {
			b.WriteString(fmt.Sprintf("retry: %d\n", evt.Retry))
		}
		b.WriteString("\n")
		return []byte(b.String()), nil
	}
	return nil, fmt.Errorf("requires Event type")
}
func (e *EventStreamEncoder) Unmarshal(data []byte, v interface{}) error { return nil }
func (e *EventStreamEncoder) ContentType() string                        { return ContentTypeEventStream }

// Stream implements the Streamer interface for EventStreamEncoder.
func (e *EventStreamEncoder) Stream(w Writer, callback func() (interface{}, error)) error {
	for {
		data, err := callback()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil // End of stream
			}
			return fmt.Errorf("stream callback failed: %w", err)
		}
		encoded, err := e.Marshal(data)
		if err != nil {
			return fmt.Errorf("encoding failed: %w", err)
		}
		if _, err := w.Write(encoded); err != nil {
			return fmt.Errorf("write failed: %w", err)
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}
