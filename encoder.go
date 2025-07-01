package beam

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
)

// -----------------------------------------------------------------------------
// Buffer Pool
// -----------------------------------------------------------------------------

var bufferPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, 0, 1024)) // Initial capacity of 1KB
	},
}

// getBuffer retrieves a buffer from the pool.
// Returns a *bytes.Buffer with at least 1KB initial capacity.
// The caller must call putBuffer to return the buffer to the pool.
// Ensures efficient memory reuse for encoding operations.
func getBuffer() *bytes.Buffer {
	return bufferPool.Get().(*bytes.Buffer)
}

// putBuffer returns a buffer to the pool after resetting it.
// Takes a *bytes.Buffer to be reset and reused.
// Ensures no memory leaks by clearing the buffer's contents.
// Thread-safe for concurrent use via sync.Pool.
func putBuffer(buf *bytes.Buffer) {
	buf.Reset()
	bufferPool.Put(buf)
}

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
// Creates a new registry with thread-safe encoder mappings.
// Registers JSON, MsgPack, XML, Text, FormURLEncoded, and EventStream encoders.
// Returns a pointer to the initialized EncoderRegistry.
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
// Takes an Encoder implementation to register for its content type.
// Thread-safe using a mutex to protect concurrent access.
// Associates the encoder with its ContentType in the encoders map.
func (er *EncoderRegistry) Register(e Encoder) {
	er.mu.Lock()
	defer er.mu.Unlock()
	er.encoders[e.ContentType()] = e
}

// Get retrieves an encoder by content type.
// Takes a content type string (e.g., "application/json").
// Returns the associated Encoder and a boolean indicating if found.
// Thread-safe using a read lock for concurrent access.
func (er *EncoderRegistry) Get(contentType string) (Encoder, bool) {
	er.mu.RLock()
	defer er.mu.RUnlock()
	e, ok := er.encoders[contentType]
	return e, ok
}

// Encode marshals data using the encoder for the given content type.
// Takes a content type and data to encode.
// Returns the encoded bytes or an error if the encoder is not found.
// Delegates to the appropriate encoder's Marshal method.
func (er *EncoderRegistry) Encode(contentType string, v interface{}) ([]byte, error) {
	e, ok := er.Get(contentType)
	if !ok {
		return nil, fmt.Errorf("no encoder for content type %s", contentType)
	}
	return e.Marshal(v)
}

// EncodeWithFallback marshals data with fallback on error.
// Takes a content type and data to encode.
// Returns encoded bytes or fallback data with an EncoderError if encoding fails.
// Uses the encoder's Marshal method with fallback handling.
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

// Error returns a string representation of the encoding error.
// Combines the content type and original error message.
// Implements the error interface for EncoderError.
// Returns a formatted string for error reporting.
func (e *EncoderError) Error() string {
	return fmt.Sprintf("encoding failed for %s: %v", e.ContentType, e.OriginalError)
}

// Unwrap returns the original error causing the encoding failure.
// Provides access to the underlying error for unwrapping.
// Implements the Unwrap method for error handling.
// Returns the OriginalError field.
func (e *EncoderError) Unwrap() error {
	return e.OriginalError
}

// JSONErrorResponse generates a JSON-formatted error response.
// Creates a JSON object with "error" and "message" fields.
// Uses a pooled buffer for encoding to reduce allocations.
// Returns the encoded JSON bytes, falling back to direct marshal if needed.
func (e *EncoderError) JSONErrorResponse() []byte {
	resp := map[string]string{
		"error":   "encoding failed",
		"message": e.OriginalError.Error(),
	}
	buf := getBuffer()
	defer putBuffer(buf)
	enc := json.NewEncoder(buf)
	if err := enc.Encode(resp); err != nil {
		// Fallback to direct marshal as a last resort
		data, _ := json.Marshal(resp)
		return data
	}
	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())
	return data
}

// XMLErrorResponse generates an XML-formatted error response.
// Creates an XML structure with an "error" tag and message.
// Uses a pooled buffer for encoding to reduce allocations.
// Returns the encoded XML bytes, falling back to direct marshal if needed.
func (e *EncoderError) XMLErrorResponse() []byte {
	type XMLError struct {
		XMLName xml.Name `xml:"error"`
		Message string   `xml:"message"`
	}
	resp := XMLError{Message: e.OriginalError.Error()}
	buf := getBuffer()
	defer putBuffer(buf)
	enc := xml.NewEncoder(buf)
	if err := enc.Encode(resp); err != nil {
		// Fallback to direct marshal as a last resort
		data, _ := xml.Marshal(resp)
		return data
	}
	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())
	return data
}

// TextErrorResponse generates a text-formatted error response.
// Formats a plain text message with the encoding error.
// Uses a pooled buffer to minimize memory allocations.
// Returns the formatted text as bytes.
func (e *EncoderError) TextErrorResponse() []byte {
	buf := getBuffer()
	defer putBuffer(buf)
	fmt.Fprintf(buf, "encoding failed: %s", e.OriginalError.Error())
	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())
	return data
}

// GenerateFallback generates the appropriate fallback based on content type.
// Selects the appropriate error response based on content type.
// Supports JSON, XML, Text, and MsgPack formats.
// Returns the fallback data as bytes.
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
		buf := getBuffer()
		defer putBuffer(buf)
		enc := msgpack.NewEncoder(buf)
		if err := enc.Encode(resp); err != nil {
			// Fallback to direct marshal as a last resort
			data, _ := msgpack.Marshal(resp)
			return data
		}
		data := make([]byte, buf.Len())
		copy(data, buf.Bytes())
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
	Type  string      `type:"type,omitempty"`
	Data  interface{} `json:"data"`
	Retry int         `json:"retry,omitempty"`
}

// -----------------------------------------------------------------------------
// Default Encoder Implementations
// -----------------------------------------------------------------------------

type JSONEncoder struct{}

// Marshal encodes data to JSON format using a pooled buffer.
// Takes any JSON-serializable data as input.
// Returns the encoded JSON bytes without trailing newline or an error if encoding fails.
// Uses a pooled buffer to reduce memory allocations.
func (e *JSONEncoder) Marshal(v interface{}) ([]byte, error) {
	buf := getBuffer()
	defer putBuffer(buf)
	enc := json.NewEncoder(buf)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Trim trailing newline added by json.Encoder
	data := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	result := make([]byte, len(data))
	copy(result, data)
	return result, nil
}

// Unmarshal decodes JSON data into the provided pointer.
// Takes a byte slice and a pointer to the target variable.
// Returns an error if decoding fails.
// Uses standard json.Unmarshal without buffer pooling.
func (e *JSONEncoder) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// ContentType returns the JSON content type.
// Returns the constant "application/json".
// Used by EncoderRegistry to map this encoder.
// No side effects or parameters.
func (e *JSONEncoder) ContentType() string {
	return ContentTypeJSON
}

type MsgPackEncoder struct{}

// Marshal encodes data to MsgPack format using a pooled buffer.
// Takes any MsgPack-serializable data as input.
// Returns the encoded MsgPack bytes or an error if encoding fails.
// Uses a pooled buffer to reduce memory allocations.
func (e *MsgPackEncoder) Marshal(v interface{}) ([]byte, error) {
	buf := getBuffer()
	defer putBuffer(buf)
	enc := msgpack.NewEncoder(buf)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())
	return data, nil
}

// Unmarshal decodes MsgPack data into the provided pointer.
// Takes a byte slice and a pointer to the target variable.
// Returns an error if decoding fails.
// Uses standard msgpack.Unmarshal without buffer pooling.
func (e *MsgPackEncoder) Unmarshal(data []byte, v interface{}) error {
	return msgpack.Unmarshal(data, v)
}

// ContentType returns the MsgPack content type.
// Returns the constant "application/msgpack".
// Used by EncoderRegistry to map this encoder.
// No side effects or parameters.
func (e *MsgPackEncoder) ContentType() string {
	return ContentTypeMsgPack
}

type XMLEncoder struct{}

// Marshal encodes data to XML format, handling Response and map types specially.
// Takes any XML-serializable data, with special handling for Response and maps.
// Returns the encoded XML bytes or an error if encoding fails.
// Uses a pooled buffer for general encoding and specific methods for Response/maps.
func (e *XMLEncoder) Marshal(v interface{}) ([]byte, error) {
	// Handle Response type specially
	if resp, ok := v.(Response); ok {
		return e.marshalResponse(resp)
	}

	// Handle maps - convert to a more XML-friendly structure
	if m, ok := v.(map[string]interface{}); ok {
		return e.mapToXMLBytes(m)
	}

	buf := getBuffer()
	defer putBuffer(buf)
	enc := xml.NewEncoder(buf)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())
	return data, nil
}

// mapToXMLBytes converts a map to an XML-friendly byte slice.
// Takes a map[string]interface{} to encode as XML.
// Returns the encoded XML bytes or an error if encoding fails.
// Uses a pooled buffer to reduce memory allocations.
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

	buf := getBuffer()
	defer putBuffer(buf)
	enc := xml.NewEncoder(buf)
	if err := enc.Encode(entries); err != nil {
		return nil, err
	}
	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())
	return data, nil
}

// marshalResponse encodes a Response struct to XML with proper structure.
// Takes a Response struct to encode with system and meta handling.
// Returns the encoded XML bytes with XML header or an error if encoding fails.
// Uses a pooled buffer to reduce memory allocations.
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

	buf := getBuffer()
	defer putBuffer(buf)
	enc := xml.NewEncoder(buf)
	if err := enc.Encode(aux); err != nil {
		return nil, err
	}
	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())
	header := []byte(xml.Header)
	data = append(header, data...)
	return data, nil
}

// mapToXML converts a map to an XML-compatible structure.
// Takes a map[string]interface{} to transform into XML elements.
// Returns a structure suitable for XML encoding.
// Used internally by mapToXMLBytes and marshalResponse.
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

// Unmarshal decodes XML data into the provided pointer.
// Takes a byte slice and a pointer to the target variable.
// Returns an error if decoding fails.
// Uses standard xml.Unmarshal without buffer pooling.
func (e *XMLEncoder) Unmarshal(data []byte, v interface{}) error {
	return xml.Unmarshal(data, v)
}

// ContentType returns the XML content type.
// Returns the constant "application/xml".
// Used by EncoderRegistry to map this encoder.
// No side effects or parameters.
func (e *XMLEncoder) ContentType() string {
	return ContentTypeXML
}

type TextEncoder struct{}

// Marshal converts data to plain text using a pooled buffer.
// Takes any data and formats it as a string using fmt.Sprintf.
// Returns the text as bytes or an error if formatting fails.
// Uses a pooled buffer to reduce memory allocations.
func (e *TextEncoder) Marshal(v interface{}) ([]byte, error) {
	buf := getBuffer()
	defer putBuffer(buf)
	fmt.Fprintf(buf, "%v", v)
	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())
	return data, nil
}

// Unmarshal is a no-op for text encoding.
// Takes a byte slice and a target variable (ignored).
// Always returns nil, as text decoding is not supported.
// No side effects or buffer usage.
func (e *TextEncoder) Unmarshal(data []byte, v interface{}) error {
	return nil
}

// ContentType returns the text content type.
// Returns the constant "text/plain".
// Used by EncoderRegistry to map this encoder.
// No side effects or parameters.
func (e *TextEncoder) ContentType() string {
	return ContentTypeText
}

type FormURLEncodedEncoder struct{}

// Marshal encodes a map to URL-encoded form data using a pooled buffer.
// Takes a map[string]interface{} to encode as form data.
// Returns the encoded bytes or an error if the input is not a map.
// Uses a pooled buffer to reduce memory allocations.
func (e *FormURLEncodedEncoder) Marshal(v interface{}) ([]byte, error) {
	if m, ok := v.(map[string]interface{}); ok {
		values := url.Values{}
		for k, val := range m {
			values.Set(k, fmt.Sprintf("%v", val))
		}
		buf := getBuffer()
		defer putBuffer(buf)
		buf.WriteString(values.Encode())
		data := make([]byte, buf.Len())
		copy(data, buf.Bytes())
		return data, nil
	}
	return nil, fmt.Errorf("requires map[string]interface{}")
}

// Unmarshal is a no-op for form-encoded data.
// Takes a byte slice and a target variable (ignored).
// Always returns nil, as decoding is not supported.
// No side effects or buffer usage.
func (e *FormURLEncodedEncoder) Unmarshal(data []byte, v interface{}) error {
	return nil
}

// ContentType returns the form-urlencoded content type.
// Returns the constant "application/x-www-form-urlencoded".
// Used by EncoderRegistry to map this encoder.
// No side effects or parameters.
func (e *FormURLEncodedEncoder) ContentType() string {
	return ContentTypeFormURLEncoded
}

// EventStreamEncoder encodes Server-Sent Events (SSE).
type EventStreamEncoder struct{}

// Marshal encodes an SSE event to its string representation.
// Takes an Event struct with ID, Type, Data, and Retry fields.
// Returns the encoded SSE bytes without extra newlines or an error if encoding fails.
// Uses pooled buffers for both the event and its JSON data field to minimize allocations.
func (e *EventStreamEncoder) Marshal(v interface{}) ([]byte, error) {
	if evt, ok := v.(Event); ok {
		buf := getBuffer()
		defer putBuffer(buf)
		if evt.ID != "" {
			buf.WriteString("id: ")
			buf.WriteString(evt.ID)
			buf.WriteByte('\n')
		}
		if evt.Type != "" {
			buf.WriteString("event: ")
			buf.WriteString(evt.Type)
			buf.WriteByte('\n')
		}
		dataBuf := getBuffer()
		enc := json.NewEncoder(dataBuf)
		if err := enc.Encode(evt.Data); err != nil {
			putBuffer(dataBuf)
			return nil, err
		}
		// Trim trailing newline from JSON data
		data := bytes.TrimSuffix(dataBuf.Bytes(), []byte("\n"))
		buf.WriteString("data: ")
		buf.Write(data)
		buf.WriteByte('\n')
		putBuffer(dataBuf)
		if evt.Retry > 0 {
			buf.WriteString("retry: ")
			// Convert int to string efficiently
			var numBuf [20]byte // Sufficient for int64
			n := len(strconv.AppendInt(numBuf[:0], int64(evt.Retry), 10))
			buf.Write(numBuf[:n])
			buf.WriteByte('\n')
		}
		buf.WriteString("\n")
		result := make([]byte, buf.Len())
		copy(result, buf.Bytes())
		return result, nil
	}
	return nil, errors.New("requires Event type")
}

// Unmarshal is a no-op for SSE events.
// Takes a byte slice and a target variable (ignored).
// Always returns nil, as decoding is not supported.
// No side effects or buffer usage.
func (e *EventStreamEncoder) Unmarshal(data []byte, v interface{}) error {
	return nil
}

// ContentType returns the SSE content type.
// Returns the constant "text/event-stream".
// Used by EncoderRegistry to map this encoder.
// No side effects or parameters.
func (e *EventStreamEncoder) ContentType() string {
	return ContentTypeEventStream
}

// Stream sends SSE events incrementally using a callback.
// Takes a Writer and a callback that produces Event data.
// Writes encoded events to the Writer, flushing if supported.
// Returns an error if encoding or writing fails.
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
