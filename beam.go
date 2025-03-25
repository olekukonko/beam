package beam

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/vmihailenco/msgpack.v2"
)

// -----------------------------------------------------------------------------
// Supported Formats and Status Constants
// -----------------------------------------------------------------------------

// Format defines supported content formats.
type Format int

const (
	FormatJSON Format = iota
	FormatMsgPack
	FormatXML
	FormatText
	FormatBinary
	FormatFormURLEncoded
	FormatEventStream
	FormatUnknown
)

// Status constants for response states.
const (
	StatusError      = "-error"
	StatusPending    = "?pending"
	StatusSuccessful = "+ok"
	StatusFatal      = "*fatal"
)

// Image type constants.
const (
	ImageTypePNG  = "image/png"
	ImageTypeJPEG = "image/jpeg"
	ImageTypeGIF  = "image/gif"
	ImageTypeWebp = "image/webp"
)

// DefaultName is the default application name.
const DefaultName = "beam"

// ErrSkip is a predefined error for skipping.
var ErrSkip = errors.New("skip")

// -----------------------------------------------------------------------------
// Logger, Finalizer, and Preset
// -----------------------------------------------------------------------------

// Logger is an interface for logging errors.
type Logger interface {
	Log(err error) bool // Returns true if logged successfully
}

// Finalizer defines a function to handle errors after rendering.
type Finalizer func(w Writer, err error)

// Preset defines a preset for custom content types.
type Preset struct {
	ContentType string
	Headers     http.Header
}

// -----------------------------------------------------------------------------
// System Metadata and Renderer Settings
// -----------------------------------------------------------------------------

// SystemShow controls where system info is displayed.
type SystemShow int

const (
	SystemShowNone SystemShow = iota
	SystemShowHeaders
	SystemShowBody
	SystemShowBoth
)

// System holds system metadata and display preferences.
type System struct {
	App      string
	Server   string
	Version  string
	Build    string
	Play     bool
	Show     SystemShow // Where to show system info (default: None)
	Duration time.Duration
}

// Setting configures the renderer.
type Setting struct {
	Name          string
	Format        Format            // Default output format
	EnableHeaders bool              // Enable sending headers (default true)
	Presets       map[string]Preset // Custom presets for content types
}

// -----------------------------------------------------------------------------
// SSE Event and Callback Types
// -----------------------------------------------------------------------------

// Event represents a Server-Sent Events (SSE) event.
type Event struct {
	ID    string      `json:"id,omitempty"`
	Type  string      `json:"type,omitempty"`
	Data  interface{} `json:"data"`
	Retry int         `json:"retry,omitempty"`
}

// CallbackData carries information to callback functions.
type CallbackData struct {
	ID      string   `json:"id"`
	Status  string   `json:"status"` // Uses Status* constants
	Title   string   `json:"title,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Message string   `json:"message,omitempty"`
	Output  string   `json:"output,omitempty"`
	Err     error    `json:"-"` // Not marshaled, for internal use
}

func (c CallbackData) IsError() bool {
	return c.Status == StatusError || c.Status == StatusFatal
}

func (c CallbackData) Error() error {
	return c.Err
}

// ErrorList is a custom type for a list of errors that implements JSON marshalling.
type ErrorList []error

// MarshalJSON implements custom JSON marshaling for ErrorList
func (el ErrorList) MarshalJSON() ([]byte, error) {
	errStrings := make([]string, len(el))
	for i, err := range el {
		if err != nil {
			errStrings[i] = err.Error()
		}
	}
	return json.Marshal(errStrings)
}

// UnmarshalJSON implements custom JSON unmarshaling for ErrorList
func (el *ErrorList) UnmarshalJSON(data []byte) error {
	var errStrings []string
	if err := json.Unmarshal(data, &errStrings); err != nil {
		return err
	}

	*el = make(ErrorList, len(errStrings))
	for i, s := range errStrings {
		(*el)[i] = errors.New(s)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Response Structure and Encoder Interfaces
// -----------------------------------------------------------------------------

// Response is the standard response structure.
type Response struct {
	Status  string                 `json:"status" xml:"status" msgpack:"status"`
	Title   string                 `json:"title,omitempty" xml:"title,omitempty" msgpack:"title"`
	Message string                 `json:"message,omitempty" xml:"message,omitempty" msgpack:"message"`
	Tags    []string               `json:"tags,omitempty" xml:"tags,omitempty" msgpack:"tags"`
	Info    interface{}            `json:"info,omitempty" xml:"info,omitempty" msgpack:"info"`
	Data    []interface{}          `json:"data,omitempty" xml:"data,omitempty" msgpack:"data"`
	Meta    map[string]interface{} `json:"meta,omitempty" xml:"meta,omitempty" msgpack:"meta"`
	Errors  ErrorList              `json:"errors,omitempty" xml:"errors,omitempty" msgpack:"errors"` // Changed from 'error' to 'errors'
}

// Encoder defines a generic encoding interface.
type Encoder interface {
	Marshal(v interface{}) ([]byte, error)
	Unmarshal(data []byte, v interface{}) error
}

// EncoderRegistry manages mappings from Format to Encoder.
type EncoderRegistry struct {
	defaults  map[Format]Encoder
	custom    map[Format]Encoder
	fallbacks []Encoder
}

// NewEncoderRegistry initializes an EncoderRegistry with default encoders.
func NewEncoderRegistry() *EncoderRegistry {
	return &EncoderRegistry{
		defaults: map[Format]Encoder{
			FormatJSON:           &JSONEncoder{},
			FormatMsgPack:        &MsgPackEncoder{},
			FormatXML:            &XMLEncoder{},
			FormatText:           &TextEncoder{},
			FormatFormURLEncoded: &FormURLEncodedEncoder{},
			FormatEventStream:    &EventStreamEncoder{},
		},
		custom: make(map[Format]Encoder),
	}
}

// Register allows registering a custom encoder for a format.
func (er *EncoderRegistry) Register(f Format, e Encoder) *EncoderRegistry {
	er.custom[f] = e
	return er
}

// Fallback adds fallback encoders.
func (er *EncoderRegistry) Fallback(e ...Encoder) *EncoderRegistry {
	er.fallbacks = append(er.fallbacks, e...)
	return er
}

// Encode marshals the value v using the encoder for format f.
func (er *EncoderRegistry) Encode(f Format, v interface{}) ([]byte, error) {
	if e, ok := er.custom[f]; ok {
		return e.Marshal(v)
	}
	if e, ok := er.defaults[f]; ok {
		return e.Marshal(v)
	}
	if len(er.fallbacks) > 0 {
		return er.fallbacks[0].Marshal(v)
	}
	return nil, fmt.Errorf("no encoder for format %d", f)
}

// Default Encoders

type JSONEncoder struct{}

func (e *JSONEncoder) Marshal(v interface{}) ([]byte, error) { return json.Marshal(v) }
func (e *JSONEncoder) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

type MsgPackEncoder struct{}

func (e *MsgPackEncoder) Marshal(v interface{}) ([]byte, error) { return msgpack.Marshal(v) }
func (e *MsgPackEncoder) Unmarshal(data []byte, v interface{}) error {
	return msgpack.Unmarshal(data, v)
}

type XMLEncoder struct{}

func (e *XMLEncoder) Marshal(v interface{}) ([]byte, error) { return xml.Marshal(v) }
func (e *XMLEncoder) Unmarshal(data []byte, v interface{}) error {
	return xml.Unmarshal(data, v)
}

type TextEncoder struct{}

func (e *TextEncoder) Marshal(v interface{}) ([]byte, error) {
	return []byte(fmt.Sprintf("%v", v)), nil
}
func (e *TextEncoder) Unmarshal(data []byte, v interface{}) error { return nil }

type FormURLEncodedEncoder struct{}

func (e *FormURLEncodedEncoder) Marshal(v interface{}) ([]byte, error) {
	if m, ok := v.(map[string]interface{}); ok {
		values := url.Values{}
		for k, val := range m {
			values.Set(k, fmt.Sprintf("%v", val))
		}
		return []byte(values.Encode()), nil
	}
	return nil, fmt.Errorf("FormatFormURLEncoded requires map[string]interface{}")
}
func (e *FormURLEncodedEncoder) Unmarshal(data []byte, v interface{}) error { return nil }

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
	return nil, fmt.Errorf("FormatEventStream requires Event")
}
func (e *EventStreamEncoder) Unmarshal(data []byte, v interface{}) error { return nil }

// -----------------------------------------------------------------------------
// Protocol and Writer Interfaces
// -----------------------------------------------------------------------------

// ProtocolHandler manages protocol-specific behavior.
type ProtocolHandler struct {
	protocol Protocol
}

// NewProtocolHandler creates a new ProtocolHandler.
func NewProtocolHandler(p Protocol) *ProtocolHandler {
	return &ProtocolHandler{protocol: p}
}

// ApplyHeaders applies protocol-specific headers.
func (ph *ProtocolHandler) ApplyHeaders(w Writer, code int) error {
	return ph.protocol.ApplyHeaders(w, code)
}

// Protocol defines protocol-specific behavior.
type Protocol interface {
	ApplyHeaders(w Writer, code int) error
}

// HTTPProtocol implements the HTTP protocol.
type HTTPProtocol struct{}

func (p *HTTPProtocol) ApplyHeaders(w Writer, code int) error {
	if hw, ok := w.(http.ResponseWriter); ok {
		hw.WriteHeader(code)
	}
	return nil
}

// TCPProtocol implements a basic TCP protocol.
type TCPProtocol struct{}

func (p *TCPProtocol) ApplyHeaders(w Writer, code int) error {
	return nil
}

// Writer defines the interface for output destinations.
type Writer interface {
	Write(data []byte) (int, error)
}

// -----------------------------------------------------------------------------
// Callback Management
// -----------------------------------------------------------------------------

// CallbackManager handles callback registration and triggering.
type CallbackManager struct {
	callbacks []func(data CallbackData)
}

// NewCallbackManager creates a new CallbackManager.
func NewCallbackManager() *CallbackManager {
	return &CallbackManager{}
}

// Clone creates a copy of the CallbackManager.
func (cm *CallbackManager) Clone() *CallbackManager {
	newCM := &CallbackManager{
		callbacks: append([]func(data CallbackData){}, cm.callbacks...),
	}
	return newCM
}

// AddCallback registers one or more callbacks.
func (cm *CallbackManager) AddCallback(cb ...func(data CallbackData)) *CallbackManager {
	cm.callbacks = append(cm.callbacks, cb...)
	return cm
}

// Trigger calls all registered callbacks with the provided data.
func (cm *CallbackManager) Trigger(id, status, msg string, err error) {
	if len(cm.callbacks) == 0 {
		return
	}
	data := CallbackData{
		ID:      id,
		Status:  status,
		Message: msg,
		Err:     err,
	}
	if err != nil {
		data.Output = err.Error()
	}
	for _, cb := range cm.callbacks {
		cb(data)
	}
}

// -----------------------------------------------------------------------------
// Renderer: Core Beam Renderer
// -----------------------------------------------------------------------------

// Renderer is the core Beam renderer responsible for constructing and sending responses.
type Renderer struct {
	s            Setting
	name         string
	code         int
	meta         map[string]interface{}
	tags         []string
	id           string
	title        string
	start        time.Time
	header       http.Header
	ctx          context.Context
	encoders     *EncoderRegistry
	protocol     *ProtocolHandler
	callbacks    *CallbackManager
	format       Format
	errorFilters []func(error) bool // Renderer-level error filters
	logger       Logger             // Optional logger
	writer       Writer             // Default writer
	finalizer    Finalizer          // Error finalizer
	generateID   bool               // Enable automatic ID generation
	system       System             // System metadata configuration
}

// New creates a new Renderer with the provided settings.
func New(s Setting) *Renderer {
	if s.Format == FormatUnknown {
		s.Format = FormatJSON
	}
	if s.Name == "" {
		s.Name = DefaultName
	}
	s.EnableHeaders = true // Enable headers by default
	return &Renderer{
		s:         s,
		code:      0, // Status code will be set by methods as needed
		meta:      make(map[string]interface{}),
		tags:      make([]string, 0),
		header:    make(http.Header),
		encoders:  NewEncoderRegistry(),
		protocol:  NewProtocolHandler(&HTTPProtocol{}),
		callbacks: NewCallbackManager(),
		format:    s.Format,
		start:     time.Now(),
		errorFilters: []func(error) bool{
			func(err error) bool { return errors.Is(err, sql.ErrNoRows) },
			func(err error) bool { return errors.Is(err, ErrSkip) },
		},
		finalizer: func(w Writer, err error) { // Default finalizer for HTTP
			if err != nil {
				if hw, ok := w.(http.ResponseWriter); ok {
					http.Error(hw, err.Error(), http.StatusInternalServerError)
				}
			}
		},
		system: System{
			Show: SystemShowNone, // Off by default
		},
	}
}

// clone creates a shallow copy of the renderer with deep copies of mutable fields.
func (r *Renderer) clone() *Renderer {
	newRenderer := *r
	newRenderer.meta = cloneMap(r.meta)
	newRenderer.tags = cloneSlice(r.tags)
	newRenderer.header = cloneHeader(r.header)
	newRenderer.callbacks = r.callbacks.Clone()
	newRenderer.errorFilters = append([]func(error) bool{}, r.errorFilters...)
	return &newRenderer
}

// WithWriter sets the default writer for the renderer.
func (r *Renderer) WithWriter(w Writer) *Renderer {
	nr := r.clone()
	nr.writer = w
	return nr
}

// WithErrorFilters adds additional error filters.
func (r *Renderer) WithErrorFilters(filters ...func(error) bool) *Renderer {
	nr := r.clone()
	nr.errorFilters = append(nr.errorFilters, filters...)
	return nr
}

// SetLogger sets the renderer's logger.
func (r *Renderer) SetLogger(l Logger) *Renderer {
	nr := r.clone()
	nr.logger = l
	return nr
}

// WithHeadersEnabled enables or disables header output.
func (r *Renderer) WithHeadersEnabled(enabled bool) *Renderer {
	nr := r.clone()
	nr.s.EnableHeaders = enabled
	return nr
}

// WithFinalizer sets the error finalizer.
func (r *Renderer) WithFinalizer(f Finalizer) *Renderer {
	nr := r.clone()
	nr.finalizer = f
	return nr
}

// WithSystem configures system metadata display.
func (r *Renderer) WithSystem(show SystemShow, sys System) *Renderer {
	nr := r.clone()
	nr.system = sys
	nr.system.Show = show
	return nr
}

// WithIDGeneration enables or disables automatic ID generation.
func (r *Renderer) WithIDGeneration(enabled bool) *Renderer {
	nr := r.clone()
	nr.generateID = enabled
	return nr
}

// applyCommonHeaders builds and applies common headers to the writer.
func (r *Renderer) applyCommonHeaders(w Writer, contentType string) error {
	if w == nil {
		return fmt.Errorf("writer cannot be nil")
	}
	if r.protocol == nil {
		return fmt.Errorf("protocol cannot be nil")
	}

	// Build common headers with a prefix based on the application name.
	setHeader := func(key, value string) {
		r.header.Set(fmt.Sprintf("X-%s-%s", r.s.Name, key), value)
	}

	if r.s.EnableHeaders {
		r.header.Set("Content-Type", contentType)
		// Optionally include system metadata in headers.
		if r.system.Show == SystemShowHeaders || r.system.Show == SystemShowBoth {
			setHeader("Duration", time.Since(r.start).String())
			setHeader("Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
			if r.system.App != "" {
				setHeader("App", r.system.App)
			}
			if r.system.Server != "" {
				setHeader("Server", r.system.Server)
			}
			if r.system.Version != "" {
				setHeader("Version", r.system.Version)
			}
			if r.system.Build != "" {
				setHeader("Build", r.system.Build)
			}
			setHeader("Play", fmt.Sprintf("%t", r.system.Play))
		}
		// Apply preset headers if available.
		if r.s.Presets != nil {
			if preset, ok := r.s.Presets[contentType]; ok && preset.Headers != nil {
				for key, values := range preset.Headers {
					for _, value := range values {
						r.header.Add(key, value)
					}
				}
			}
		}
		// If the writer is an HTTP ResponseWriter, update its header.
		if hw, ok := w.(http.ResponseWriter); ok {
			for key, values := range r.header {
				for _, value := range values {
					hw.Header().Add(key, value)
				}
			}
		}
	}
	return r.protocol.ApplyHeaders(w, r.code)
}

// Add these helper methods to the Renderer struct in beam.go
func (r *Renderer) WithContext(ctx context.Context) *Renderer {
	nr := r.clone()
	nr.ctx = ctx
	return nr
}

// WithStatus sets the HTTP status code.
func (r *Renderer) WithStatus(code int) *Renderer {
	nr := r.clone()
	nr.code = code
	return nr
}

// WithHeader adds a header to the renderer.
func (r *Renderer) WithHeader(key, value string) *Renderer {
	nr := r.clone()
	nr.header.Add(key, value)
	return nr
}

// WithMeta adds metadata to the renderer.
func (r *Renderer) WithMeta(key string, value interface{}) *Renderer {
	nr := r.clone()
	nr.meta[key] = value
	return nr
}

// WithTag adds tags to the renderer.
func (r *Renderer) WithTag(tags ...string) *Renderer {
	nr := r.clone()
	nr.tags = append(nr.tags, tags...)
	return nr
}

// WithID sets the ID for the renderer.
func (r *Renderer) WithID(id string) *Renderer {
	nr := r.clone()
	nr.id = id
	return nr
}

// WithTitle sets the title for the renderer.
func (r *Renderer) WithTitle(t string) *Renderer {
	nr := r.clone()
	nr.title = t
	return nr
}

// WithCallback adds callbacks to the renderer.
func (r *Renderer) WithCallback(cb ...func(data CallbackData)) *Renderer {
	nr := r.clone()
	nr.callbacks.AddCallback(cb...)
	return nr
}

// FilterError adds error filters to the renderer.
func (r *Renderer) FilterError(filters ...func(error) bool) *Renderer {
	nr := r.clone()
	nr.errorFilters = append(nr.errorFilters, filters...)
	return nr
}

// UseEncoder registers a custom encoder.
func (r *Renderer) UseEncoder(f Format, e Encoder) *Renderer {
	nr := r.clone()
	nr.encoders.Register(f, e)
	return nr
}

// WithFormat sets the output format.
func (r *Renderer) WithFormat(f Format) *Renderer {
	nr := r.clone()
	nr.format = f
	return nr
}

// WithProtocol sets the protocol handler.
func (r *Renderer) WithProtocol(p Protocol) *Renderer {
	nr := r.clone()
	nr.protocol = NewProtocolHandler(p)
	return nr
}

// contentType returns the MIME type for the current format.
func (r *Renderer) contentType() string {
	switch r.format {
	case FormatMsgPack:
		return "application/msgpack"
	case FormatXML:
		return "application/xml"
	case FormatText:
		return "text/plain"
	case FormatBinary:
		return "application/octet-stream"
	case FormatFormURLEncoded:
		return "application/x-www-form-urlencoded"
	case FormatEventStream:
		return "text/event-stream"
	case FormatJSON:
		fallthrough
	default:
		return "application/json"
	}
}

// triggerCallbacks is a helper to invoke callbacks and optionally log errors.
func (r *Renderer) triggerCallbacks(id, status, msg string, err error) {
	r.callbacks.Trigger(id, status, msg, err)
	if err != nil && r.logger != nil {
		r.logger.Log(err)
	}
}

// Push sends a structured Response using the current renderer configuration.
func (r *Renderer) Push(w Writer, d Response) error {
	nr := r.clone()
	nr.start = time.Now()

	// Check context cancellation first
	if nr.ctx != nil {
		select {
		case <-nr.ctx.Done():
			nr.triggerCallbacks(nr.id, StatusError, "operation canceled", ErrContextCanceled)
			return ErrContextCanceled
		default:
		}
	}

	if w == nil && nr.writer != nil {
		w = nr.writer
	}

	if nr.generateID && nr.id == "" {
		nr.id = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}

	if d.Status == "" {
		d.Status = StatusSuccessful
	}

	if d.Title == "" && d.Status == StatusError {
		d.Title = "error"
	}

	// Set default status codes if not already defined
	if nr.code == 0 {
		switch d.Status {
		case StatusSuccessful:
			nr.code = http.StatusOK
		case StatusPending:
			nr.code = http.StatusAccepted
		case StatusError:
			nr.code = http.StatusBadRequest
		case StatusFatal:
			nr.code = http.StatusInternalServerError
		}
	}

	d.Tags = cloneSlice(nr.tags)

	// If system display is enabled, include system info in meta
	if nr.system.Show == SystemShowBody || nr.system.Show == SystemShowBoth {
		if d.Meta == nil {
			d.Meta = make(map[string]interface{})
		}
		d.Meta["system"] = map[string]interface{}{
			"app":       nr.system.App,
			"server":    nr.system.Server,
			"version":   nr.system.Version,
			"build":     nr.system.Build,
			"play":      nr.system.Play,
			"timestamp": time.Now().Unix(),
			"duration":  time.Since(nr.start).String(),
		}
	}

	encoded, err := nr.encoders.Encode(nr.format, d)
	if err != nil {
		wrapped := fmt.Errorf("encoding failed: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	if err := nr.applyCommonHeaders(w, nr.contentType()); err != nil {
		wrapped := fmt.Errorf("header write failed: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	_, err = w.Write(encoded)
	if err != nil {
		wrapped := fmt.Errorf("write failed: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	nr.triggerCallbacks(nr.id, d.Status, d.Message, nil)
	return nil
}

// Raw sends raw data using the current format.
func (r *Renderer) Raw(data interface{}) error {
	nr := r.clone()
	nr.start = time.Now()
	w := nr.writer
	if w == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	if nr.generateID && nr.id == "" {
		nr.id = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	if nr.code == 0 {
		nr.code = http.StatusOK // Default for Raw
	}
	encoded, err := nr.encoders.Encode(nr.format, data)
	if err != nil {
		wrapped := fmt.Errorf("encoding failed: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}
	if err := nr.applyCommonHeaders(w, nr.contentType()); err != nil {
		wrapped := fmt.Errorf("header write failed: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}
	_, err = w.Write(encoded)
	if err != nil {
		wrapped := fmt.Errorf("write failed: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}
	nr.triggerCallbacks(nr.id, StatusSuccessful, "Raw data sent", nil)
	return nil
}

// Binary sends binary data with proper headers.
func (r *Renderer) Binary(contentType string, data []byte) error {
	nr := r.clone()
	nr.start = time.Now()
	w := nr.writer
	if w == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	if nr.generateID && nr.id == "" {
		nr.id = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	if nr.code == 0 {
		nr.code = http.StatusOK // Default for Binary
	}
	if err := nr.applyCommonHeaders(w, contentType); err != nil {
		wrapped := fmt.Errorf("header write failed: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}
	_, err := w.Write(data)
	if err != nil {
		wrapped := fmt.Errorf("binary write failed: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}
	nr.triggerCallbacks(nr.id, StatusSuccessful, "Binary data sent", nil)
	return nil
}

// Image encodes and sends an image using the specified content type.
func (r *Renderer) Image(contentType string, img image.Image) error {
	nr := r.clone()
	nr.start = time.Now()
	w := nr.writer
	if w == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	if nr.generateID && nr.id == "" {
		nr.id = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	if nr.code == 0 {
		nr.code = http.StatusOK // Default for Image
	}
	buf := new(bytes.Buffer)
	switch contentType {
	case ImageTypePNG:
		if err := png.Encode(buf, img); err != nil {
			wrapped := fmt.Errorf("PNG encoding failed: %w", err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}
	case ImageTypeJPEG:
		opts := &jpeg.Options{Quality: 80}
		if err := jpeg.Encode(buf, img, opts); err != nil {
			wrapped := fmt.Errorf("JPEG encoding failed: %w", err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}
	case ImageTypeGIF:
		if err := gif.Encode(buf, img, nil); err != nil {
			wrapped := fmt.Errorf("GIF encoding failed: %w", err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}
	default:
		err := fmt.Errorf("unsupported image content type: %s", contentType)
		nr.triggerCallbacks(nr.id, StatusError, err.Error(), err)
		if nr.finalizer != nil {
			nr.finalizer(w, err)
		}
		return err
	}
	return nr.Binary(contentType, buf.Bytes())
}

// -----------------------------------------------------------------------------
// Convenience Methods (Writer-less versions)
// -----------------------------------------------------------------------------

func (r *Renderer) Info(msg string, info interface{}) error {
	if r.writer == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
		Info:    info,
	})
}

func (r *Renderer) Data(msg string, data []interface{}) error {
	if r.writer == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
		Data:    data,
	})
}

func (r *Renderer) Response(msg string, info interface{}, data []interface{}) error {
	if r.writer == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
		Info:    info,
		Data:    data,
	})
}

func (r *Renderer) Pending(msg string, info interface{}) error {
	if r.writer == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	return r.WithStatus(http.StatusAccepted).Push(r.writer, Response{
		Status:  StatusPending,
		Message: msg,
		Info:    info,
	})
}

func (r *Renderer) Titled(title, msg string, info interface{}) error {
	if r.writer == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Title:   title,
		Message: msg,
		Info:    info,
	})
}

func (r *Renderer) Error(format string, errs ...error) error {
	if r.writer == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	// Check error filters.
	for _, filter := range r.errorFilters {
		for _, err := range errs {
			if filter(err) {
				return nil
			}
		}
	}
	joined := errors.Join(errs...)
	msg := fmt.Sprintf(format, joined)
	return r.WithStatus(http.StatusBadRequest).Push(r.writer, Response{
		Status:  StatusError,
		Message: msg,
		Errors:  errs,
	})
}

func (r *Renderer) Warning(errs ...error) error {
	if r.writer == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	for _, filter := range r.errorFilters {
		for _, err := range errs {
			if filter(err) {
				return nil
			}
		}
	}
	joined := errors.Join(errs...)
	return r.WithStatus(http.StatusBadRequest).Push(r.writer, Response{
		Status:  StatusError,
		Message: joined.Error(),
		Errors:  errs,
	})
}

func (r *Renderer) Fatal(errs ...error) error {
	if r.writer == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	for _, filter := range r.errorFilters {
		for _, err := range errs {
			if filter(err) {
				return nil
			}
		}
	}
	joined := errors.Join(errs...)
	resp := Response{
		Status:  StatusFatal,
		Message: joined.Error(),
		Errors:  errs,
	}
	err := r.WithStatus(http.StatusInternalServerError).WithTag(StatusFatal).Push(r.writer, resp)
	if err == nil && r.logger != nil {
		r.logger.Log(joined)
	}
	return err
}

func (r *Renderer) Log(err error) {
	if err == nil {
		return
	}
	for _, filter := range r.errorFilters {
		if filter(err) {
			return
		}
	}
	if r.logger != nil {
		r.logger.Log(err)
	}
}

// Handler returns an http.HandlerFunc that uses the renderer to handle requests.
func (r *Renderer) Handler(fn func(r *Renderer) error) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		renderer := r.WithWriter(w)
		if err := fn(renderer); err != nil {
			renderer.Fatal(err)
		}
	}
}

// -----------------------------------------------------------------------------
// Helper Functions for Deep Copying Mutable Fields
// -----------------------------------------------------------------------------

// cloneHeader creates a deep copy of the given http.Header.
func cloneHeader(h http.Header) http.Header {
	newHeader := make(http.Header)
	for k, v := range h {
		newVals := make([]string, len(v))
		copy(newVals, v)
		newHeader[k] = newVals
	}
	return newHeader
}

// cloneMap creates a shallow copy of a map.
func cloneMap(m map[string]interface{}) map[string]interface{} {
	newMap := make(map[string]interface{})
	for k, v := range m {
		newMap[k] = v
	}
	return newMap
}

// cloneSlice creates a deep copy of a string slice.
func cloneSlice(s []string) []string {
	newSlice := make([]string, len(s))
	copy(newSlice, s)
	return newSlice
}
