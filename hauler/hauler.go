package hauler

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
)

// ContentType constants matching Beam's encoder types
const (
	ContentTypeJSON           = "application/json"
	ContentTypeMsgPack        = "application/msgpack"
	ContentTypeXML            = "application/xml"
	ContentTypeFormURLEncoded = "application/x-www-form-urlencoded"
	ContentTypeMultipartForm  = "multipart/form-data"
	ContentTypeText           = "text/plain"
	ContentTypeBinary         = "application/octet-stream"
)

var (
	ErrUnsupportedContentType = errors.New("unsupported content type")
	ErrNilRequest             = errors.New("request cannot be nil")
	ErrInvalidPointer         = errors.New("must provide a non-nil pointer")
)

// BodyParser defines the interface for content-type specific parsers.
// Provides methods to check if a content type can be parsed and to parse request bodies.
// Used by Hauler to delegate parsing to specific implementations.
type BodyParser interface {
	CanParse(contentType string) bool
	Parse(body io.Reader, v interface{}) error
}

// Hauler manages HTTP request body parsing.
// Stores a registry of parsers and handles content-type based parsing.
// Thread-safe using a read-write mutex for concurrent access.
type Hauler struct {
	parsers  []BodyParser
	registry map[string]BodyParser
	mu       sync.RWMutex
}

// New creates a new Hauler with default parsers.
// Initializes a Hauler with JSON, XML, MsgPack, form, and text parsers.
// Returns a pointer to the initialized Hauler.
func New() *Hauler {
	r := &Hauler{
		registry: make(map[string]BodyParser),
	}

	// Register default parsers
	r.Register(&jsonParser{})
	r.Register(&xmlParser{})
	r.Register(&msgpackParser{})
	r.Register(&formParser{})
	r.Register(&textParser{})

	return r
}

// Register adds a new BodyParser to the Hauler.
// Associates the parser with supported content types in the registry.
// Thread-safe using a mutex to protect concurrent registration.
func (r *Hauler) Register(p BodyParser) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ct := range []string{
		ContentTypeJSON,
		ContentTypeXML,
		ContentTypeMsgPack,
		ContentTypeFormURLEncoded,
		ContentTypeText,
	} {
		if p.CanParse(ct) {
			r.registry[ct] = p
		}
	}

	r.parsers = append(r.parsers, p)
}

// Read reads and parses the request body based on Content-Type.
// Takes an HTTP request and a target interface to parse the body into.
// Returns an error if the request is nil, content type is unsupported, or parsing fails.
func (r *Hauler) Read(req *http.Request, v interface{}) error {
	if req == nil || req.Body == nil {
		return ErrNilRequest
	}

	contentType := req.Header.Get("Content-Type")
	// Remove charset if present
	if idx := strings.Index(contentType, ";"); idx > 0 {
		contentType = contentType[:idx]
	}

	r.mu.RLock()
	parser, ok := r.registry[contentType]
	r.mu.RUnlock()

	if !ok {
		// Try to find a parser that can handle this content type
		for _, p := range r.parsers {
			if p.CanParse(contentType) {
				parser = p
				break
			}
		}
		if parser == nil {
			return fmt.Errorf("%w: %s", ErrUnsupportedContentType, contentType)
		}
	}

	// For idempotency, we'll read the body once and then re-create it
	// so subsequent reads will work
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	return parser.Parse(bytes.NewReader(bodyBytes), v)
}

// DefaultReader is the package-level default reader.
// Provides a pre-initialized Hauler for convenience.
// Used by the Read function for simplified parsing.
var DefaultReader = New()

// Read is a convenience function using the default reader.
// Parses an HTTP request body into the provided interface.
// Returns an error if parsing fails or the request is invalid.
func Read(req *http.Request, v interface{}) error {
	return DefaultReader.Read(req, v)
}

// Parser implementations

// jsonParser handles JSON content type parsing.
// Implements BodyParser for JSON request bodies.
// Supports content types containing "application/json".
type jsonParser struct{}

func (p *jsonParser) CanParse(contentType string) bool {
	return strings.Contains(contentType, ContentTypeJSON)
}

func (p *jsonParser) Parse(body io.Reader, v interface{}) error {
	if v == nil {
		return ErrInvalidPointer
	}
	return json.NewDecoder(body).Decode(v)
}

// xmlParser handles XML content type parsing.
// Implements BodyParser for XML request bodies.
// Supports content types containing "application/xml" or "text/xml".
type xmlParser struct{}

func (p *xmlParser) CanParse(contentType string) bool {
	return strings.Contains(contentType, ContentTypeXML) ||
		strings.Contains(contentType, "text/xml")
}

func (p *xmlParser) Parse(body io.Reader, v interface{}) error {
	if v == nil {
		return ErrInvalidPointer
	}
	return xml.NewDecoder(body).Decode(v)
}

// msgpackParser handles MsgPack content type parsing.
// Implements BodyParser for MsgPack request bodies.
// Supports content types containing "application/msgpack".
type msgpackParser struct{}

func (p *msgpackParser) CanParse(contentType string) bool {
	return strings.Contains(contentType, ContentTypeMsgPack)
}

func (p *msgpackParser) Parse(body io.Reader, v interface{}) error {
	if v == nil {
		return ErrInvalidPointer
	}
	return msgpack.NewDecoder(body).Decode(v)
}

// formParser handles form-urlencoded content type parsing.
// Implements BodyParser for form data request bodies.
// Supports "application/x-www-form-urlencoded" content type.
type formParser struct{}

func (p *formParser) CanParse(contentType string) bool {
	return contentType == ContentTypeFormURLEncoded
}

// Parse parses form-urlencoded data into a map or url.Values.
// Reads the body and decodes it into the provided interface.
// Returns an error if the data is invalid or the target type is unsupported.
func (p *formParser) Parse(body io.Reader, v interface{}) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("failed to read form data: %w", err)
	}

	values, err := url.ParseQuery(string(data))
	if err != nil {
		return fmt.Errorf("invalid form data: %w", err)
	}

	// Validate no empty keys exist
	for key := range values {
		if key == "" {
			return errors.New("form data contains empty key")
		}
	}

	switch dest := v.(type) {
	case *map[string]string:
		*dest = make(map[string]string)
		for k, v := range values {
			if len(v) > 0 {
				(*dest)[k] = v[0]
			}
		}
	case *map[string][]string:
		*dest = values
	case *url.Values:
		*dest = values
	default:
		return fmt.Errorf("form data can only be decoded into map[string]string, map[string][]string, or url.Values")
	}

	return nil
}

// textParser handles plain text content type parsing.
// Implements BodyParser for text request bodies.
// Supports content types containing "text/plain".
type textParser struct{}

func (p *textParser) CanParse(contentType string) bool {
	return strings.Contains(contentType, ContentTypeText)
}

func (p *textParser) Parse(body io.Reader, v interface{}) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	switch dest := v.(type) {
	case *string:
		*dest = string(data)
	case *[]byte:
		*dest = data
	default:
		return fmt.Errorf("text data can only be decoded into string or []byte")
	}

	return nil
}
