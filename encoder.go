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

// EncoderRegistry's EncodeWithFallback
func (er *EncoderRegistry) EncodeWithFallback(contentType string, v interface{}) ([]byte, error) {
	e, ok := er.Get(contentType)
	if !ok {
		return nil, fmt.Errorf("no encoder for content type %s", contentType)
	}

	data, err := e.Marshal(v)
	if err == nil {
		return data, nil
	}

	encErr := &EncoderError{
		OriginalError: err,
		ContentType:   contentType,
	}
	encErr.FallbackData = encErr.GenerateFallback()
	return encErr.FallbackData, encErr
}

// EncoderError represents an encoding failure with fallback data
type EncoderError struct {
	OriginalError error
	ContentType   string
	FallbackData  []byte
}

func (e *EncoderError) Error() string {
	return fmt.Sprintf("encoding failed for %s: %v", e.ContentType, e.OriginalError)
}

func (e *EncoderError) Unwrap() error {
	return e.OriginalError
}

// JSONErrorResponse generates a JSON-formatted error response
func (e *EncoderError) JSONErrorResponse() []byte {
	resp := map[string]string{
		"error":   "encoding failed",
		"message": e.OriginalError.Error(),
	}
	data, _ := json.Marshal(resp) // Safe to ignore error here as fallback
	return data
}

// XMLErrorResponse generates an XML-formatted error response
func (e *EncoderError) XMLErrorResponse() []byte {
	type XMLError struct {
		XMLName xml.Name `xml:"error"`
		Message string   `xml:"message"`
	}
	resp := XMLError{Message: e.OriginalError.Error()}
	data, _ := xml.Marshal(resp) // Safe to ignore error here as fallback
	return data
}

// TextErrorResponse generates a text-formatted error response
func (e *EncoderError) TextErrorResponse() []byte {
	return []byte(fmt.Sprintf("encoding failed: %s", e.OriginalError.Error()))
}

// GenerateFallback generates the appropriate fallback based on content type
func (e *EncoderError) GenerateFallback() []byte {
	switch e.ContentType {
	case ContentTypeJSON:
		return e.JSONErrorResponse()
	case ContentTypeXML:
		return e.XMLErrorResponse()
	case ContentTypeText:
		return e.TextErrorResponse()
	case ContentTypeMsgPack:
		// Minimal MsgPack fallback
		resp := map[string]string{
			"error":   "encoding failed",
			"message": e.OriginalError.Error(),
		}
		data, _ := msgpack.Marshal(resp)
		return data
	default:
		return []byte(e.OriginalError.Error())
	}
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

func (e *XMLEncoder) Marshal(v interface{}) ([]byte, error) {
	// Handle Response type specially
	if resp, ok := v.(Response); ok {
		return e.marshalResponse(resp)
	}

	// Handle maps - convert to a more XML-friendly structure
	if m, ok := v.(map[string]interface{}); ok {
		return e.mapToXMLBytes(m)
	}

	return xml.Marshal(v)
}

func (e *XMLEncoder) mapToXMLBytes(m map[string]interface{}) ([]byte, error) {
	type Entry struct {
		XMLName xml.Name
		Value   interface{} `xml:",innerxml"`
	}

	var entries []Entry
	for k, v := range m {
		entries = append(entries, Entry{
			XMLName: xml.Name{Local: k},
			Value:   v,
		})
	}

	return xml.Marshal(entries)
}

func (e *XMLEncoder) marshalResponse(resp Response) ([]byte, error) {
	type xmlMeta struct {
		XMLName xml.Name
		Value   interface{} `xml:",innerxml"`
	}

	// MetaWrapper nests the system info separately under <meta>
	type MetaWrapper struct {
		System    interface{} `xml:"system,omitempty"`
		OtherMeta []xmlMeta   `xml:",any"`
	}

	type Alias struct {
		XMLName xml.Name      `xml:"response"` // root element
		Status  string        `xml:"status"`
		Title   string        `xml:"title,omitempty"`
		Message string        `xml:"message,omitempty"`
		Tags    []string      `xml:"tags,omitempty"`
		Info    interface{}   `xml:"info,omitempty"`
		Data    []interface{} `xml:"data,omitempty"`
		Meta    *MetaWrapper  `xml:"meta,omitempty"`
		Errors  ErrorList     `xml:"errors,omitempty"`
	}

	// Build the MetaWrapper if there is meta information
	var metaWrapper *MetaWrapper
	if resp.Meta != nil && len(resp.Meta) > 0 {
		mw := &MetaWrapper{}

		// Handle System struct specially in meta
		if sys, ok := resp.Meta["system"].(System); ok {
			type XMLSystem struct {
				App      string `xml:"App"`
				Server   string `xml:"Server,omitempty"`
				Version  string `xml:"Version,omitempty"`
				Build    string `xml:"Build,omitempty"`
				Play     bool   `xml:"Play,omitempty"`
				Duration string `xml:"Duration"`
			}
			mw.System = XMLSystem{
				App:      sys.App,
				Server:   sys.Server,
				Version:  sys.Version,
				Build:    sys.Build,
				Play:     sys.Play,
				Duration: sys.Duration.String(), // Explicit string conversion
			}
			delete(resp.Meta, "system")
		}

		// Process any additional meta fields
		for key, value := range resp.Meta {
			if nestedMap, ok := value.(map[string]interface{}); ok {
				nested := e.mapToXML(nestedMap)
				mw.OtherMeta = append(mw.OtherMeta, xmlMeta{
					XMLName: xml.Name{Local: key},
					Value:   nested,
				})
			} else {
				mw.OtherMeta = append(mw.OtherMeta, xmlMeta{
					XMLName: xml.Name{Local: key},
					Value:   value,
				})
			}
		}
		metaWrapper = mw
	}

	aux := Alias{
		Status:  resp.Status,
		Title:   resp.Title,
		Message: resp.Message,
		Tags:    resp.Tags,
		Info:    resp.Info,
		Data:    resp.Data,
		Meta:    metaWrapper,
		Errors:  resp.Errors,
	}

	// Add XML header and proper indentation
	data, err := xml.MarshalIndent(aux, "", "  ")
	if err != nil {
		return nil, err
	}

	// Proper XML document with header
	return []byte(xml.Header + string(data)), nil
}

// mapToXML converts a map[string]interface{} to an XML-compatible structure
func (e *XMLEncoder) mapToXML(m map[string]interface{}) interface{} {
	type xmlElement struct {
		XMLName xml.Name
		Value   interface{} `xml:",innerxml"`
	}

	elements := make([]xmlElement, 0, len(m))
	for key, value := range m {
		if nestedMap, ok := value.(map[string]interface{}); ok {
			elements = append(elements, xmlElement{XMLName: xml.Name{Local: key}, Value: e.mapToXML(nestedMap)})
		} else {
			elements = append(elements, xmlElement{XMLName: xml.Name{Local: key}, Value: value})
		}
	}
	return elements
}

func (e *XMLEncoder) Unmarshal(data []byte, v interface{}) error {
	return xml.Unmarshal(data, v)
}

func (e *XMLEncoder) ContentType() string {
	return ContentTypeXML
}

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
