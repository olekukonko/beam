package beam

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HugoSmits86/nativewebp"
	"github.com/olekukonko/beam/hauler"
)

// Renderer is the core Beam renderer for constructing and sending responses.
// Manages response configuration, encoding, and output with support for multiple formats.
// Thread-safe through immutable cloning for concurrent modifications.
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
	contentType  string              // Current content type (e.g., "application/json")
	errorFilters []func(error) bool  // Renderer-level error filters
	logger       Logger              // Optional logger
	writer       Writer              // Default writer
	httpWriter   http.ResponseWriter // Concrete HTTP writer, if applicable
	finalizer    Finalizer           // Error finalizer
	generateID   bool                // Enable automatic ID generation
	system       System              // System metadata configuration
}

// NewRenderer creates a new Renderer with the provided settings and default content type.
// Initializes fields with default JSON content type and error filters.
// Returns a pointer to the initialized Renderer.
func NewRenderer(s Setting) *Renderer {
	if s.ContentType == Empty {
		s.ContentType = ContentTypeJSON // Fallback to JSON
	}
	if s.Name == Empty {
		s.Name = "beam" // Default name if not provided
	}
	r := &Renderer{
		s:           s,
		contentType: s.ContentType,
		code:        0, // Status code set by methods as needed
		meta:        make(map[string]interface{}),
		tags:        make([]string, 0),
		header:      make(http.Header),
		encoders:    NewEncoderRegistry(),
		protocol:    NewProtocolHandler(&HTTPProtocol{}),
		callbacks:   NewCallbackManager(),
		start:       time.Now(),
		errorFilters: []func(error) bool{
			func(err error) bool { return errors.Is(err, sql.ErrNoRows) },
			func(err error) bool { return errors.Is(err, ErrSkip) },
			func(err error) bool { return errors.Is(err, ErrHidden) },
		},
		finalizer: func(w Writer, err error) { // Default finalizer for HTTP
			if err != nil {
				if hw, ok := w.(http.ResponseWriter); ok {
					http.Error(hw, err.Error(), http.StatusInternalServerError)
				}
			}
		},
		system: System{
			show: SystemShowNone, // Off by default
		},
	}
	// Ensure EnableHeaders defaults to true if not set
	if !r.s.EnableHeaders {
		r.s.EnableHeaders = true
	}
	return r
}

// clone creates a shallow copy of the Renderer with deep copies of mutable fields.
// Ensures immutability for chained method calls by copying meta, tags, headers, and callbacks.
// Returns a new Renderer instance for thread-safe modifications.
func (r *Renderer) clone() *Renderer {
	newRenderer := *r
	newRenderer.meta = cloneMap(r.meta)
	newRenderer.tags = cloneSlice(r.tags)
	newRenderer.header = cloneHeader(r.header)
	newRenderer.callbacks = r.callbacks.Clone()
	newRenderer.errorFilters = append([]func(error) bool{}, r.errorFilters...)
	return &newRenderer
}

// -----------------------------------------------------------------------------
// Renderer Configuration Methods
// -----------------------------------------------------------------------------

// WithWriter sets the default writer for the Renderer.
// Assigns the provided Writer and sets httpWriter if applicable.
// Returns a new Renderer with updated writer fields.
func (r *Renderer) WithWriter(w Writer) *Renderer {
	nr := r.clone()
	if hw, ok := w.(http.ResponseWriter); ok {
		nr.httpWriter = hw
	}
	nr.writer = w
	return nr
}

// WithErrorFilters adds additional error filters to the Renderer.
// Appends the provided filter functions to the errorFilters slice.
// Returns a new Renderer with updated error filters.
func (r *Renderer) WithErrorFilters(filters ...func(error) bool) *Renderer {
	nr := r.clone()
	nr.errorFilters = append(nr.errorFilters, filters...)
	return nr
}

// SetLogger sets the Renderer's logger (deprecated; use WithLogger).
// Updates the logger field for error logging in a new Renderer copy.
// Returns a new Renderer with the updated logger.
func (r *Renderer) SetLogger(l Logger) *Renderer {
	nr := r.clone()
	nr.logger = l
	return nr
}

// WithLogger sets the Renderer's logger for error logging.
// Assigns the provided Logger interface in a new Renderer copy.
// Returns a new Renderer with the updated logger.
func (r *Renderer) WithLogger(l Logger) *Renderer {
	nr := r.clone()
	nr.logger = l
	return nr
}

// WithHeadersEnabled enables or disables header output.
// Toggles the EnableHeaders setting in a new Renderer copy.
// Returns a new Renderer with the updated header setting.
func (r *Renderer) WithHeadersEnabled(enabled bool) *Renderer {
	nr := r.clone()
	nr.s.EnableHeaders = enabled
	return nr
}

// WithFinalizer sets the error finalizer for the Renderer.
// Assigns a Finalizer function to handle errors during response writing.
// Returns a new Renderer with the updated finalizer.
func (r *Renderer) WithFinalizer(f Finalizer) *Renderer {
	nr := r.clone()
	nr.finalizer = f
	return nr
}

// WithSystem configures system metadata display for the Renderer.
// Sets the SystemShow mode and System struct for metadata inclusion.
// Returns a new Renderer with updated system settings.
func (r *Renderer) WithSystem(show SystemShow, sys System) *Renderer {
	nr := r.clone()
	nr.system = sys
	nr.system.show = show
	return nr
}

// WithIDGeneration enables or disables automatic ID generation.
// Toggles the generateID field in a new Renderer copy.
// Returns a new Renderer with the updated ID generation setting.
func (r *Renderer) WithIDGeneration(enabled bool) *Renderer {
	nr := r.clone()
	nr.generateID = enabled
	return nr
}

// WithContext sets the context for the Renderer.
// Assigns a context.Context for cancellation and deadlines.
// Returns a new Renderer with the updated context.
func (r *Renderer) WithContext(ctx context.Context) *Renderer {
	nr := r.clone()
	nr.ctx = ctx
	return nr
}

// WithStatus sets the HTTP status code for the Renderer.
// Assigns the provided HTTP status code (e.g., http.StatusOK).
// Returns a new Renderer with the updated status code.
func (r *Renderer) WithStatus(code int) *Renderer {
	nr := r.clone()
	nr.code = code
	return nr
}

// WithHeader adds a header to the Renderer.
// Adds the provided key-value pair to the HTTP header map.
// Returns a new Renderer with the updated headers.
func (r *Renderer) WithHeader(key, value string) *Renderer {
	nr := r.clone()
	nr.header.Add(key, value)
	return nr
}

// WithMeta adds metadata to the Renderer.
// Adds the provided key-value pair to the meta map.
// Returns a new Renderer with the updated metadata.
func (r *Renderer) WithMeta(key string, value interface{}) *Renderer {
	nr := r.clone()
	if nr.meta == nil {
		nr.meta = make(map[string]interface{})
	}
	nr.meta[key] = value
	return nr
}

// WithTag adds tags to the Renderer.
// Appends the provided tags to the tags slice.
// Returns a new Renderer with the updated tags.
func (r *Renderer) WithTag(tags ...string) *Renderer {
	nr := r.clone()
	nr.tags = append(nr.tags, tags...)
	return nr
}

// WithID sets the ID for the Renderer.
// Assigns the provided string ID for the response.
// Returns a new Renderer with the updated ID.
func (r *Renderer) WithID(id string) *Renderer {
	nr := r.clone()
	nr.id = id
	return nr
}

// WithTitle sets the title for the Renderer.
// Assigns the provided string title for the response.
// Returns a new Renderer with the updated title.
func (r *Renderer) WithTitle(t string) *Renderer {
	nr := r.clone()
	nr.title = t
	return nr
}

// WithCallback adds callbacks to the Renderer.
// Adds the provided callback functions to handle response events.
// Returns a new Renderer with updated callbacks.
func (r *Renderer) WithCallback(cb ...func(data CallbackData)) *Renderer {
	nr := r.clone()
	nr.callbacks.AddCallback(cb...)
	return nr
}

// SetAction adds an action to the Renderer's response.
// Appends a new Action with the provided name and description to meta.
// Returns a new Renderer with the updated actions.
func (r *Renderer) SetAction(name, description string) *Renderer {
	nr := r.clone()
	if nr.meta == nil {
		nr.meta = make(map[string]interface{})
	}
	if _, ok := nr.meta["actions"]; !ok {
		nr.meta["actions"] = []Action{}
	}
	actions := nr.meta["actions"].([]Action)
	nr.meta["actions"] = append(actions, Action{
		Name:        name,
		Description: description,
	})
	return nr
}

// WithAction adds fully specified actions to the Renderer.
// Appends the provided Action structs to the meta actions list.
// Returns a new Renderer with the updated actions.
func (r *Renderer) WithAction(actions ...Action) *Renderer {
	nr := r.clone()
	if nr.meta == nil {
		nr.meta = make(map[string]interface{})
	}
	if _, ok := nr.meta["actions"]; !ok {
		nr.meta["actions"] = []Action{}
	}
	currentActions := nr.meta["actions"].([]Action)
	nr.meta["actions"] = append(currentActions, actions...)
	return nr
}

// WithActions replaces all current actions in the Renderer.
// Sets the provided Action slice as the meta actions list.
// Returns a new Renderer with the updated actions.
func (r *Renderer) WithActions(actions []Action) *Renderer {
	nr := r.clone()
	if nr.meta == nil {
		nr.meta = make(map[string]interface{})
	}
	nr.meta["actions"] = actions
	return nr
}

// FilterError adds error filters to the Renderer.
// Appends the provided error filter functions to errorFilters.
// Returns a new Renderer with the updated filters.
func (r *Renderer) FilterError(filters ...func(error) bool) *Renderer {
	nr := r.clone()
	nr.errorFilters = append(nr.errorFilters, filters...)
	return nr
}

// UseEncoder registers a custom encoder with the Renderer.
// Adds the provided Encoder to the EncoderRegistry.
// Returns a new Renderer with the updated encoders.
func (r *Renderer) UseEncoder(e Encoder) *Renderer {
	nr := r.clone()
	nr.encoders.Register(e)
	return nr
}

// WithContentType sets the output content type for the Renderer.
// Assigns the provided content type string (e.g., "application/json").
// Returns a new Renderer with the updated content type.
func (r *Renderer) WithContentType(contentType string) *Renderer {
	nr := r.clone()
	nr.contentType = contentType
	return nr
}

// WithProtocol sets the protocol handler for the Renderer.
// Assigns the provided Protocol interface for response output.
// Returns a new Renderer with the updated protocol handler.
func (r *Renderer) WithProtocol(p Protocol) *Renderer {
	nr := r.clone()
	nr.protocol = NewProtocolHandler(p)
	return nr
}

// Show updates the system metadata display configuration.
// Sets the SystemShow mode for controlling metadata output.
// Returns nil as no error conditions are currently defined.
func (r *Renderer) Show(show SystemShow) error {
	r.system.show = show
	return nil
}

// applyCommonHeaders builds and applies common headers to the writer.
// Sets headers including content type, system metadata, and presets.
// Returns an error if the writer or protocol is nil or header application fails.
func (r *Renderer) applyCommonHeaders(w Writer, contentType string) error {
	if w == nil {
		return errNilWriter
	}
	if r.protocol == nil {
		return errNilProtocol
	}

	// Build common headers with a prefix based on the application name.
	setHeader := func(key, value string) {
		prefix := HeaderPrefix
		if r.s.Name != Empty {
			prefix = "X-" + r.s.Name
		}
		r.header.Set(prefix+"-"+key, value)
	}

	if r.s.EnableHeaders {
		r.header.Set(HeaderContentType, contentType)
		// Optionally include system metadata in headers.
		if r.system.show == SystemShowHeaders || r.system.show == SystemShowBoth {
			setHeader(HeaderNameDuration, time.Since(r.start).String())
			setHeader(HeaderNameTimestamp, strconv.FormatInt(time.Now().Unix(), 10))
			if r.system.App != Empty {
				setHeader(HeaderNameApp, r.system.App)
			}
			if r.system.Server != Empty {
				setHeader(HeaderNameServer, r.system.Server)
			}
			if r.system.Version != Empty {
				setHeader(HeaderNameVersion, r.system.Version)
			}
			if r.system.Build != Empty {
				setHeader(HeaderNameBuild, r.system.Build)
			}
			setHeader(HeaderNamePlay, strconv.FormatBool(r.system.Play))
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
		// If httpWriter is set, use it directly to avoid type assertion.
		if r.httpWriter != nil {
			for key, values := range r.header {
				for _, value := range values {
					r.httpWriter.Header().Add(key, value)
				}
			}
		} else if hw, ok := w.(http.ResponseWriter); ok {
			for key, values := range r.header {
				for _, value := range values {
					hw.Header().Add(key, value)
				}
			}
		}
	}
	return r.protocol.ApplyHeaders(w, r.code)
}

// triggerCallbacks invokes registered callbacks and logs errors if needed.
// Triggers callbacks with the provided ID, status, message, and error.
// Logs errors via the Renderer’s logger if present; no return value.
func (r *Renderer) triggerCallbacks(id, status, msg string, err error) {
	r.callbacks.Trigger(id, status, msg, err)
	if err != nil && r.logger != nil {
		r.logger.Log(err)
	}
}

// Push sends a structured Response using the Renderer’s configuration.
// Encodes and writes the Response with headers, handling errors with fallbacks.
// Returns an error if encoding, header application, or writing fails.
func (r *Renderer) Push(w Writer, d Response) error {
	nr := r.clone()
	// Only set start time if not already set (allows tests to preset it)
	if nr.start.IsZero() {
		nr.start = time.Now()
	}

	// Check context cancellation first.
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
	if w == nil {
		return errNoWriter
	}

	if nr.generateID && nr.id == Empty {
		var buf [20]byte
		n := len(strconv.AppendInt(buf[:0], time.Now().UnixNano(), 10))
		nr.id = "req-" + string(buf[:n])
	}

	resp := getResponse()
	defer putResponse(resp)
	resp.Status = d.Status
	resp.Title = d.Title
	resp.Message = d.Message
	resp.Info = d.Info
	resp.Data = d.Data
	resp.Tags = cloneSlice(nr.tags)
	resp.Errors = d.Errors

	if resp.Status == Empty {
		resp.Status = StatusSuccessful
	}
	if resp.Title == Empty && resp.Status == StatusError {
		resp.Title = "error"
	}

	// Set default status codes if not already defined.
	if nr.code == 0 {
		switch resp.Status {
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

	// If system display is enabled, include system info in meta.
	if nr.system.show == SystemShowBody || nr.system.show == SystemShowBoth {
		if resp.Meta == nil {
			resp.Meta = make(map[string]interface{})
		}
		sysCopy := nr.system
		sysCopy.Duration = time.Since(nr.start).Truncate(time.Second)
		resp.Meta["system"] = sysCopy
	}

	// Use the fallback-capable encoder.
	encoded, err := nr.encoders.EncodeWithFallback(nr.contentType, *resp)
	if err != nil {
		// We expect an EncoderError if encoding failed.
		var encErr *EncoderError
		if errors.As(err, &encErr) {
			encoded = encErr.FallbackData
			nr.triggerCallbacks(nr.id, StatusError, encErr.Error(), encErr)
			// Adjust the status code.
			if nr.code == http.StatusOK || nr.code == http.StatusAccepted {
				nr.code = http.StatusInternalServerError
			}
			// Write fallback error response.
			if hdrErr := nr.applyCommonHeaders(w, nr.contentType); hdrErr != nil {
				nr.triggerCallbacks(nr.id, StatusFatal, hdrErr.Error(), hdrErr)
				if nr.finalizer != nil {
					nr.finalizer(w, hdrErr)
				}
				return hdrErr
			}
			if _, wErr := w.Write(encoded); wErr != nil {
				wrapped := errors.Join(errWriteFailed, wErr)
				nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
				if nr.finalizer != nil {
					nr.finalizer(w, wrapped)
				}
				return wrapped
			}
			// Return the encoding error so callers (and tests) see it.
			return encErr
		}
		// Unexpected error.
		wrapped := errors.Join(errEncodingFailed, err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	if err := nr.applyCommonHeaders(w, nr.contentType); err != nil {
		wrapped := errors.Join(errHeaderWriteFailed, err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	if _, err := w.Write(encoded); err != nil {
		wrapped := errors.Join(errWriteFailed, err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	nr.triggerCallbacks(nr.id, resp.Status, resp.Message, nil)
	return nil
}

// Raw sends raw data using the Renderer’s current content type.
// Encodes and writes the provided data with headers, handling errors.
// Returns an error if encoding, header application, or writing fails.
func (r *Renderer) Raw(data interface{}) error {
	nr := r.clone()
	nr.start = time.Now()
	w := nr.writer
	if w == nil {
		return errNoWriter
	}
	if nr.generateID && nr.id == Empty {
		var buf [20]byte
		n := len(strconv.AppendInt(buf[:0], time.Now().UnixNano(), 10))
		nr.id = "req-" + string(buf[:n])
	}
	if nr.code == 0 {
		nr.code = http.StatusOK // Default for Raw
	}

	encoded, err := nr.encoders.Encode(nr.contentType, data)
	if err != nil {
		wrapped := errors.Join(errEncodingFailed, err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	if err := nr.applyCommonHeaders(w, nr.contentType); err != nil {
		wrapped := errors.Join(errHeaderWriteFailed, err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	_, err = w.Write(encoded)
	if err != nil {
		wrapped := errors.Join(errWriteFailed, err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	nr.triggerCallbacks(nr.id, StatusSuccessful, "Raw data sent", nil)
	return nil
}

// Stream sends data incrementally using a callback to produce chunks.
// Writes encoded chunks with headers, flushing if supported by the writer.
// Returns an error if encoding, header application, or writing fails.
func (r *Renderer) Stream(callback func(*Renderer) (interface{}, error)) error {
	nr := r.clone()
	nr.start = time.Now()
	w := nr.writer
	if w == nil {
		return errNoWriter
	}
	if nr.generateID && nr.id == Empty {
		var buf [20]byte
		n := len(strconv.AppendInt(buf[:0], time.Now().UnixNano(), 10))
		nr.id = "req-" + string(buf[:n])
	}
	if nr.code == 0 {
		nr.code = http.StatusOK // Default for Stream
	}

	// Check if the encoder supports streaming
	encoder, ok := nr.encoders.Get(nr.contentType)
	if !ok {
		err := errors.Join(errNoEncoder, errors.New(nr.contentType))
		nr.triggerCallbacks(nr.id, StatusFatal, err.Error(), err)
		if nr.finalizer != nil {
			nr.finalizer(w, err)
		}
		return err
	}
	if streamer, supportsStreaming := encoder.(Streamer); supportsStreaming {
		// Delegate to the encoder's streaming implementation
		if err := nr.applyCommonHeaders(w, nr.contentType); err != nil {
			wrapped := errors.Join(errHeaderWriteFailed, err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}
		return streamer.Stream(w, func() (interface{}, error) { return callback(nr) })
	}

	// Fallback to generic streaming if no Streamer implementation
	if err := nr.applyCommonHeaders(w, nr.contentType); err != nil {
		wrapped := errors.Join(errHeaderWriteFailed, err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	buf := streamBufferPool.Get().([]byte)
	defer streamBufferPool.Put(buf[:0])

	for {
		data, err := callback(nr)
		if err != nil {
			if errors.Is(err, io.EOF) { // End of stream
				nr.triggerCallbacks(nr.id, StatusSuccessful, "Stream completed", nil)
				return nil
			}
			wrapped := errors.Join(errors.New("stream callback failed"), err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}

		encoded, err := nr.encoders.Encode(nr.contentType, data)
		if err != nil {
			wrapped := errors.Join(errEncodingFailed, err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}

		if _, err := w.Write(encoded); err != nil {
			wrapped := errors.Join(errWriteFailed, err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}

		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

// Binary sends binary data with the specified content type and headers.
// Writes the provided byte slice with appropriate headers.
// Returns an error if header application or writing fails.
func (r *Renderer) Binary(contentType string, data []byte) error {
	nr := r.clone()
	nr.start = time.Now()
	w := nr.writer
	if w == nil {
		return errNoWriter
	}
	if nr.generateID && nr.id == Empty {
		var buf [20]byte
		n := len(strconv.AppendInt(buf[:0], time.Now().UnixNano(), 10))
		nr.id = "req-" + string(buf[:n])
	}
	if nr.code == 0 {
		nr.code = http.StatusOK // Default for Binary
	}

	if err := nr.applyCommonHeaders(w, contentType); err != nil {
		wrapped := errors.Join(errHeaderWriteFailed, err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	_, err := w.Write(data)
	if err != nil {
		wrapped := errors.Join(errWriteFailed, err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	nr.triggerCallbacks(nr.id, StatusSuccessful, "Binary data sent", nil)
	return nil
}

// Image encodes and sends an image with the specified content type.
// Encodes the provided image.Image (PNG, JPEG, GIF, WebP) and sends as binary data.
// Returns an error if encoding, header application, or writing fails.
func (r *Renderer) Image(contentType string, img image.Image) error {
	nr := r.clone()
	nr.start = time.Now()
	w := nr.writer
	if w == nil {
		return errNoWriter
	}
	if nr.generateID && nr.id == Empty {
		var buf [20]byte
		n := len(strconv.AppendInt(buf[:0], time.Now().UnixNano(), 10))
		nr.id = "req-" + string(buf[:n])
	}
	if nr.code == 0 {
		nr.code = http.StatusOK // Default for Image
	}

	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	switch contentType {
	case ContentTypePNG:
		if err := png.Encode(buf, img); err != nil {
			wrapped := errors.Join(errors.New("PNG encoding failed"), err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}
	case ContentTypeJPEG:
		opts := &jpeg.Options{Quality: 80}
		if err := jpeg.Encode(buf, img, opts); err != nil {
			wrapped := errors.Join(errors.New("JPEG encoding failed"), err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}
	case ContentTypeGIF:
		if err := gif.Encode(buf, img, nil); err != nil {
			wrapped := errors.Join(errors.New("GIF encoding failed"), err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}
	case ContentTypeWebP:
		if err := nativewebp.Encode(buf, img, nil); err != nil {
			wrapped := errors.Join(errors.New("WebP encoding failed"), err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}
	default:
		err := errors.Join(errUnsupportedImage, errors.New(contentType))
		nr.triggerCallbacks(nr.id, StatusError, err.Error(), err)
		if nr.finalizer != nil {
			nr.finalizer(w, err)
		}
		return err
	}

	return nr.Binary(contentType, buf.Bytes())
}

// Msg sends a successful response with a message.
// Sends a Response with StatusSuccessful and the provided message.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Msg(msg string) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
	})
}

// Info sends a successful response with a message and info data.
// Sends a Response with StatusSuccessful, message, and optional info.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Info(msg string, info interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
		Info:    info,
	})
}

// Data sends a successful response with a message and data items.
// Sends a Response with StatusSuccessful, message, and data slice.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Data(msg string, data interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
		Data:    data,
	})
}

// Response sends a successful response with message, info, and data.
// Sends a Response with StatusSuccessful, message, info, and data.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Response(msg string, info interface{}, data interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
		Info:    info,
		Data:    data,
	})
}

// Pending sends a pending response with a message and info data.
// Sends a Response with StatusPending and the provided message/info.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Pending(msg string, info interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusAccepted).Push(r.writer, Response{
		Status:  StatusPending,
		Message: msg,
		Info:    info,
	})
}

// Titled sends a successful response with a title, message, and info.
// Sends a Response with StatusSuccessful, title, message, and info.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Titled(title, msg string, info interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Title:   title,
		Message: msg,
		Info:    info,
	})
}

// Error sends an error response with a default summary message.
// Sends a Response with StatusError and filtered errors, if any.
// Returns an error if the writer is unset or sending fails; skips if all errors filtered.
func (r *Renderer) Error(errs ...error) error {
	if r.writer == nil {
		return errNoWriter
	}

	// First, check if any of the provided errors are ErrHidden.
	containsHidden := false
	if len(errs) > 0 {
		for _, err := range errs {
			if errors.Is(err, ErrHidden) {
				containsHidden = true
				break
			}
		}
	}

	filteredErrs := r.filterErrors(errs)

	// If all errors were filtered out AND none of them were hidden, we can skip the response.
	if len(errs) > 0 && len(filteredErrs) == 0 && !containsHidden {
		return nil
	}

	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusError
	resp.Errors = filteredErrs // Use the filtered list (will be empty for ErrHidden)
	resp.Message = defaultErrorMessage

	return r.WithStatus(http.StatusBadRequest).Push(r.writer, *resp)
}

// ErrorWith sends an error response with a custom message and errors.
// Sends a Response with StatusError, custom message, and filtered errors.
// Returns an error if the writer is unset or sending fails; skips if all errors filtered.
func (r *Renderer) ErrorWith(message string, errs ...error) error {
	if r.writer == nil {
		return errNoWriter
	}

	containsHidden := false
	if len(errs) > 0 {
		for _, err := range errs {
			if errors.Is(err, ErrHidden) {
				containsHidden = true
				break
			}
		}
	}

	filteredErrs := r.filterErrors(errs)

	// If all errors were filtered out AND none of them were hidden, we can skip the response.
	if len(errs) > 0 && len(filteredErrs) == 0 && !containsHidden {
		return nil
	}

	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusError
	resp.Errors = filteredErrs
	resp.Message = message

	return r.WithStatus(http.StatusBadRequest).Push(r.writer, *resp)
}

// Errorf sends an error response with a formatted message and errors.
// Formats the message with provided args, filtering errors for the response.
// Returns an error if the writer is unset or sending fails; skips if all errors filtered.
func (r *Renderer) Errorf(format string, args ...interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}

	// 1. Collect all arguments that are errors into a list.
	allErrorsFromArgs := Any2Error(args...)

	// 2. Create the final, filtered list of errors for the JSON 'errors' field.
	jsonErrorList := r.filterErrors(allErrorsFromArgs)

	// 3. Decide if we should skip sending a response entirely.
	containsHidden := false
	for _, err := range allErrorsFromArgs {
		if errors.Is(err, ErrHidden) {
			containsHidden = true
			break
		}
	}
	if len(allErrorsFromArgs) > 0 && len(jsonErrorList) == 0 && !containsHidden {
		return nil // All errors were skippable (like ErrSkip), so send nothing.
	}

	// 4. Build the separate argument list for the formatted "message" string,
	// respecting the number of verbs in the format string.
	verbCount := strings.Count(format, "%") - (strings.Count(format, "%%") * 2)
	messageFormatArgs := []interface{}{}
	argsConsumed := 0

	for i := 0; i < verbCount && argsConsumed < len(args); {
		arg := args[argsConsumed]
		argsConsumed++

		err, isErr := arg.(error)
		if !isErr {
			messageFormatArgs = append(messageFormatArgs, arg)
			i++ // This non-error argument corresponds to a verb.
			continue
		}

		// The argument is an error. Check if it's skippable.
		isSkippable := false
		for _, filter := range r.errorFilters {
			// The ErrHidden filter doesn't make an error "skippable" from the format string.
			if errors.Is(err, ErrHidden) {
				continue
			}
			if filter(err) {
				isSkippable = true
				break
			}
		}

		if isSkippable {
			// It's a skippable error like ErrSkip. We consume the verb (i++) but provide
			// no argument, forcing fmt.Sprintf to print "%!v(MISSING)".
			i++
			continue
		}

		// It's not skippable. Check if it's hidden or visible.
		if errors.Is(err, ErrHidden) {
			messageFormatArgs = append(messageFormatArgs, "*hidden*")
		} else {
			messageFormatArgs = append(messageFormatArgs, err)
		}
		i++ // This error argument corresponds to a verb.
	}

	// 5. Build and send the response.
	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusError
	resp.Errors = jsonErrorList

	format = strings.ReplaceAll(format, "%w", "%v")
	resp.Message = fmt.Sprintf(format, messageFormatArgs...)

	return r.WithStatus(http.StatusBadRequest).Push(r.writer, *resp)
}

// Warning sends a warning response with a default message and errors.
// Sends a Response with StatusWarning and filtered errors, if any.
// Returns an error if the writer is unset or sending fails; skips if all errors filtered.
func (r *Renderer) Warning(errs ...error) error {
	if r.writer == nil {
		return errNoWriter
	}

	filteredErrs := r.filterErrors(errs)
	if len(filteredErrs) == 0 && len(errs) > 0 {
		return nil
	}

	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusWarning
	resp.Errors = filteredErrs
	resp.Message = "A warning occurred" // Default message

	return r.WithStatus(http.StatusBadRequest).Push(r.writer, *resp)
}

// Warningf sends a warning response with a formatted message and errors.
// Formats the message with provided args, sending StatusWarning with filtered errors.
// Returns an error if the writer is unset or sending fails; skips if all errors filtered.
func (r *Renderer) Warningf(format string, args ...interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}

	errorList := Any2Error(args...)
	filteredErrs := r.filterErrors(errorList)
	if len(errorList) > 0 && len(filteredErrs) == 0 {
		return nil
	}

	// Prepare format args (replace %w with %v)
	formatArgs := make([]interface{}, len(args))
	for i, arg := range args {
		formatArgs[i] = arg
	}

	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusWarning
	resp.Errors = filteredErrs

	format = strings.ReplaceAll(format, "%w", "%v")
	if len(formatArgs) > 0 {
		resp.Message = fmt.Sprintf(format, formatArgs...)
	} else {
		resp.Message = format
	}

	return r.WithStatus(http.StatusBadRequest).Push(r.writer, *resp)
}

// Fatal sends a fatal error response and logs the errors.
// Sends a Response with StatusFatal, filtered errors, and logs if applicable.
// Returns an error if the writer is unset or sending fails; skips if all errors filtered.
func (r *Renderer) Fatal(errs ...error) error {
	if r.writer == nil {
		return errNoWriter
	}

	filteredErrs := r.filterErrors(errs)
	if len(filteredErrs) == 0 && len(errs) > 0 {
		return nil
	}

	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusFatal
	resp.Errors = filteredErrs
	resp.Message = "A fatal error occurred"

	err := r.WithStatus(http.StatusInternalServerError).WithTag(StatusFatal).Push(r.writer, *resp)

	if err == nil && r.logger != nil && len(filteredErrs) > 0 {
		r.logger.Log(errors.Join(filteredErrs...))
	}
	return err
}

// Fatalf sends a fatal error response with a formatted message.
// Formats the message with args, sends StatusFatal, and logs filtered errors.
// Returns an error if the writer is unset or sending fails; skips if all errors filtered.
func (r *Renderer) Fatalf(format string, args ...interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}

	errorList := Any2Error(args...)
	filteredErrs := r.filterErrors(errorList)
	if len(errorList) > 0 && len(filteredErrs) == 0 {
		return nil
	}

	formatArgs := make([]interface{}, len(args))
	for i, arg := range args {
		formatArgs[i] = arg
	}

	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusFatal
	resp.Errors = filteredErrs

	format = strings.ReplaceAll(format, "%w", "%v")
	if len(formatArgs) > 0 {
		resp.Message = fmt.Sprintf(format, formatArgs...)
	} else {
		resp.Message = format
	}

	err := r.WithStatus(http.StatusInternalServerError).WithTag(StatusFatal).Push(r.writer, *resp)

	if err == nil && r.logger != nil && len(filteredErrs) > 0 {
		r.logger.Log(errors.Join(filteredErrs...))
	}
	return err
}

// Log logs an error if not filtered and a logger is present.
// Applies error filters and logs the error via the Renderer’s logger.
// No return value; performs logging as a side effect.
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

// Logf logs a formatted message if a logger is present.
// Formats the message with filtered args and logs via the Renderer’s logger.
// No return value; performs logging as a side effect.
func (r *Renderer) Logf(format string, args ...interface{}) {
	if r.logger == nil {
		return
	}

	// Filter arguments
	var filteredArgs []interface{}
	for _, arg := range args {
		if err, ok := arg.(error); ok {
			include := true
			for _, filter := range r.errorFilters {
				if filter(err) {
					include = false
					break
				}
			}
			if include {
				filteredArgs = append(filteredArgs, err)
			}
		} else {
			filteredArgs = append(filteredArgs, arg)
		}
	}

	// Log the formatted message
	if len(filteredArgs) > 0 {
		msg := fmt.Sprintf(format, filteredArgs...)
		r.logger.Log(errors.New(msg)) // Assuming logger accepts strings
	} else {
		r.logger.Log(errors.New(format))
	}
}

// Handler wraps a function into an HTTP handler, handling errors with Fatal.
// Takes a function that processes the Renderer and returns an error.
// Returns an http.HandlerFunc for use in HTTP servers.
func (r *Renderer) Handler(fn func(r *Renderer) error) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		renderer := r.WithWriter(w)
		if err := fn(renderer); err != nil {
			_ = renderer.Fatal(err)
		}
	}
}

// Reader returns a new request reader instance for parsing HTTP bodies.
// Creates a new Hauler instance for parsing request data.
// Returns a pointer to the initialized Hauler.
func (r *Renderer) Reader() *hauler.Hauler {
	return hauler.New()
}

// Request reads and parses an HTTP request body into the provided value.
// Uses the Hauler to parse the request body based on content type.
// Returns an error if the request is nil or parsing fails; logs errors if applicable.
func (r *Renderer) Request(req *http.Request, v interface{}) error {
	if req == nil {
		return hauler.ErrNilRequest
	}

	// Use the default reader
	err := hauler.Read(req, v)
	if err != nil {
		// Log the error if we have a logger
		r.Log(err)
		return err
	}
	return nil
}

// JSON reads and parses a JSON request body into the provided value.
// Verifies the Content-Type is JSON and delegates to Request.
// Returns an error if the request is nil, content type is invalid, or parsing fails.
func (r *Renderer) JSON(req *http.Request, v interface{}) error {
	if req == nil {
		return hauler.ErrNilRequest
	}

	// Ensure content type is JSON
	ct := req.Header.Get("Content-Type")
	if !strings.Contains(ct, hauler.ContentTypeJSON) {
		return fmt.Errorf("%w: expected JSON content type", hauler.ErrUnsupportedContentType)
	}

	return r.Request(req, v)
}

// XML reads and parses an XML request body into the provided value.
// Verifies the Content-Type is XML and delegates to Request.
// Returns an error if the request is nil, content type is invalid, or parsing fails.
func (r *Renderer) XML(req *http.Request, v interface{}) error {
	if req == nil {
		return hauler.ErrNilRequest
	}

	// Ensure content type is XML
	ct := req.Header.Get("Content-Type")
	if !strings.Contains(ct, hauler.ContentTypeXML) &&
		!strings.Contains(ct, "text/xml") {
		return fmt.Errorf("%w: expected XML content type", hauler.ErrUnsupportedContentType)
	}

	return r.Request(req, v)
}

// MsgPack reads and parses a MsgPack request body into the provided value.
// Verifies the Content-Type is MsgPack and delegates to Request.
// Returns an error if the request is nil, content type is invalid, or parsing fails.
func (r *Renderer) MsgPack(req *http.Request, v interface{}) error {
	if req == nil {
		return hauler.ErrNilRequest
	}

	// Ensure content type is MsgPack
	ct := req.Header.Get("Content-Type")
	if !strings.Contains(ct, hauler.ContentTypeMsgPack) &&
		!strings.Contains(ct, "application/msgpack") {
		return fmt.Errorf("%w: expected MsgPack content type", hauler.ErrUnsupportedContentType)
	}

	return r.Request(req, v)
}

// Form reads and parses a form-urlencoded request body into the provided value.
// Verifies the Content-Type is form-urlencoded and delegates to Request.
// Returns an error if the request is nil, content type is invalid, or parsing fails.
func (r *Renderer) Form(req *http.Request, v interface{}) error {
	if req == nil {
		return hauler.ErrNilRequest
	}

	// Ensure content type is form data
	ct := req.Header.Get("Content-Type")
	if ct != hauler.ContentTypeFormURLEncoded {
		return fmt.Errorf("%w: expected form-urlencoded content type", hauler.ErrUnsupportedContentType)
	}

	return r.Request(req, v)
}

// filterErrors applies the Renderer’s filters to a slice of errors.
// Filters out errors matching ErrSkip, ErrHidden, or other filters.
// Returns a slice of errors that pass the filters; nil if empty or all filtered.
func (r *Renderer) filterErrors(errs []error) []error {
	if len(errs) == 0 {
		return nil
	}
	filtered := make([]error, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		include := true
		for _, filter := range r.errorFilters {
			if filter(err) {
				include = false
				break
			}
			// Also check for wrapped ErrHidden
			if errors.Is(err, ErrHidden) {
				include = false
				break
			}
		}
		if include {
			filtered = append(filtered, err)
		}
	}
	return filtered
}
