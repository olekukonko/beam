package beam

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"github.com/HugoSmits86/nativewebp"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Pre-defined errors to reduce fmt.Errorf allocations
var (
	errNoWriter          = errors.New("no writer set; use WithWriter to set a default writer")
	errEncodingFailed    = errors.New("encoding failed")
	errWriteFailed       = errors.New("write failed")
	errHeaderWriteFailed = errors.New("header write failed")
	errUnsupportedImage  = errors.New("unsupported image content type")
	errNilWriter         = errors.New("writer cannot be nil")
	errNilProtocol       = errors.New("protocol cannot be nil")
	errNoEncoder         = errors.New("no encoder for content type")
)

// responsePool reuses Response objects to reduce allocations
var responsePool = sync.Pool{
	New: func() interface{} {
		return &Response{
			Meta: make(map[string]interface{}),
		}
	},
}

// getResponse retrieves a Response from the pool.
// Returns a *Response with initialized Meta map.
// Caller must call putResponse to return it to the pool.
func getResponse() *Response {
	r := responsePool.Get().(*Response)
	return r
}

// putResponse returns a Response to the pool after resetting it.
// Takes a *Response and clears all fields to prevent data leakage.
// Ensures safe reuse by resetting Meta, Tags, Errors, and other fields.
func putResponse(r *Response) {
	r.Status = ""
	r.Title = ""
	r.Message = ""
	r.Info = nil
	r.Data = nil
	for k := range r.Meta {
		delete(r.Meta, k)
	}
	r.Tags = r.Tags[:0]
	r.Errors = r.Errors[:0]
	responsePool.Put(r)
}

// streamBufferPool reuses byte slices for streaming to reduce allocations
var streamBufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, 4096) // Initial capacity of 4KB
	},
}

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
// Takes a Setting struct to configure the renderer.
// Returns a *Renderer with initialized fields and default JSON content type.
// Sets up default error filters and finalizer for HTTP responses.
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
	// Ensure EnableHeaders defaults to true if not set
	if !r.s.EnableHeaders {
		r.s.EnableHeaders = true
	}
	return r
}

// clone creates a shallow copy of the renderer with deep copies of mutable fields.
// Returns a new *Renderer with copied meta, tags, headers, and callbacks.
// Preserves immutability for chained method calls.
// Ensures thread-safety for concurrent renderer modifications.
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

// WithWriter sets the default writer for the renderer.
// Takes a Writer interface to handle response output.
// Sets httpWriter if the Writer is an http.ResponseWriter.
// Returns a new *Renderer with the updated writer fields.
func (r *Renderer) WithWriter(w Writer) *Renderer {
	nr := r.clone()
	if hw, ok := w.(http.ResponseWriter); ok {
		nr.httpWriter = hw
	}
	nr.writer = w
	return nr
}

// WithErrorFilters adds additional error filters.
// Takes one or more functions that filter errors.
// Appends filters to the renderer's errorFilters slice.
// Returns a new *Renderer with updated filters.
func (r *Renderer) WithErrorFilters(filters ...func(error) bool) *Renderer {
	nr := r.clone()
	nr.errorFilters = append(nr.errorFilters, filters...)
	return nr
}

// SetLogger sets the renderer's logger.
// Takes a Logger interface for error logging.
// Updates the logger field in a new renderer copy.
// Returns a new *Renderer with the updated logger.
func (r *Renderer) SetLogger(l Logger) *Renderer {
	nr := r.clone()
	nr.logger = l
	return nr
}

// WithHeadersEnabled enables or disables header output.
// Takes a boolean to toggle header inclusion.
// Updates the EnableHeaders setting in a new renderer copy.
// Returns a new *Renderer with the updated setting.
func (r *Renderer) WithHeadersEnabled(enabled bool) *Renderer {
	nr := r.clone()
	nr.s.EnableHeaders = enabled
	return nr
}

// WithFinalizer sets the error finalizer.
// Takes a Finalizer function to handle errors during response writing.
// Updates the finalizer field in a new renderer copy.
// Returns a new *Renderer with the updated finalizer.
func (r *Renderer) WithFinalizer(f Finalizer) *Renderer {
	nr := r.clone()
	nr.finalizer = f
	return nr
}

// WithSystem configures system metadata display.
// Takes a SystemShow mode and System struct for metadata.
// Updates the system field in a new renderer copy.
// Returns a new *Renderer with the updated system settings.
func (r *Renderer) WithSystem(show SystemShow, sys System) *Renderer {
	nr := r.clone()
	nr.system = sys
	nr.system.Show = show
	return nr
}

// WithIDGeneration enables or disables automatic ID generation.
// Takes a boolean to toggle automatic ID generation.
// Updates the generateID field in a new renderer copy.
// Returns a new *Renderer with the updated setting.
func (r *Renderer) WithIDGeneration(enabled bool) *Renderer {
	nr := r.clone()
	nr.generateID = enabled
	return nr
}

// WithContext sets the context for the renderer.
// Takes a context.Context for cancellation and deadlines.
// Updates the ctx field in a new renderer copy.
// Returns a new *Renderer with the updated context.
func (r *Renderer) WithContext(ctx context.Context) *Renderer {
	nr := r.clone()
	nr.ctx = ctx
	return nr
}

// WithStatus sets the HTTP status code.
// Takes an HTTP status code (e.g., http.StatusOK).
// Updates the code field in a new renderer copy.
// Returns a new *Renderer with the updated status code.
func (r *Renderer) WithStatus(code int) *Renderer {
	nr := r.clone()
	nr.code = code
	return nr
}

// WithHeader adds a header to the renderer.
// Takes a key and value for the HTTP header.
// Adds the header to a new renderer copy's header map.
// Returns a new *Renderer with the updated headers.
func (r *Renderer) WithHeader(key, value string) *Renderer {
	nr := r.clone()
	nr.header.Add(key, value)
	return nr
}

// WithMeta adds metadata to the renderer.
// Takes a key and value for the metadata map.
// Updates the meta map in a new renderer copy.
// Returns a new *Renderer with the updated metadata.
func (r *Renderer) WithMeta(key string, value interface{}) *Renderer {
	nr := r.clone()
	if nr.meta == nil {
		nr.meta = make(map[string]interface{})
	}
	nr.meta[key] = value
	return nr
}

// WithTag adds tags to the renderer.
// Takes one or more tags to append to the tags slice.
// Updates the tags slice in a new renderer copy.
// Returns a new *Renderer with the updated tags.
func (r *Renderer) WithTag(tags ...string) *Renderer {
	nr := r.clone()
	nr.tags = append(nr.tags, tags...)
	return nr
}

// WithID sets the ID for the renderer.
// Takes a string ID for the response.
// Updates the id field in a new renderer copy.
// Returns a new *Renderer with the updated ID.
func (r *Renderer) WithID(id string) *Renderer {
	nr := r.clone()
	nr.id = id
	return nr
}

// WithTitle sets the title for the renderer.
// Takes a string title for the response.
// Updates the title field in a new renderer copy.
// Returns a new *Renderer with the updated title.
func (r *Renderer) WithTitle(t string) *Renderer {
	nr := r.clone()
	nr.title = t
	return nr
}

// WithCallback adds callbacks to the renderer.
// Takes one or more callback functions to handle response events.
// Adds callbacks to a new renderer copy's callback manager.
// Returns a new *Renderer with the updated callbacks.
func (r *Renderer) WithCallback(cb ...func(data CallbackData)) *Renderer {
	nr := r.clone()
	nr.callbacks.AddCallback(cb...)
	return nr
}

// FilterError adds error filters to the renderer.
// Takes one or more error filter functions.
// Appends filters to the errorFilters slice in a new renderer copy.
// Returns a new *Renderer with the updated filters.
func (r *Renderer) FilterError(filters ...func(error) bool) *Renderer {
	nr := r.clone()
	nr.errorFilters = append(nr.errorFilters, filters...)
	return nr
}

// UseEncoder registers a custom encoder.
// Takes an Encoder implementation to register.
// Registers the encoder in a new renderer copy's EncoderRegistry.
// Returns a new *Renderer with the updated encoders.
func (r *Renderer) UseEncoder(e Encoder) *Renderer {
	nr := r.clone()
	nr.encoders.Register(e)
	return nr
}

// WithContentType sets the output content type.
// Takes a content type string (e.g., "application/json").
// Updates the contentType field in a new renderer copy.
// Returns a new *Renderer with the updated content type.
func (r *Renderer) WithContentType(contentType string) *Renderer {
	nr := r.clone()
	nr.contentType = contentType
	return nr
}

// WithProtocol sets the protocol handler.
// Takes a Protocol interface for handling response output.
// Updates the protocol handler in a new renderer copy.
// Returns a new *Renderer with the updated protocol.
func (r *Renderer) WithProtocol(p Protocol) *Renderer {
	nr := r.clone()
	nr.protocol = NewProtocolHandler(p)
	return nr
}

// -----------------------------------------------------------------------------
// Renderer Core Methods
// -----------------------------------------------------------------------------

// applyCommonHeaders builds and applies common headers to the writer.
// Takes a Writer and content type to set headers.
// Applies headers including content type, system metadata, and presets.
// Returns an error if header application fails.
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
		if r.system.Show == SystemShowHeaders || r.system.Show == SystemShowBoth {
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

// triggerCallbacks invokes callbacks and optionally logs errors.
// Takes an ID, status, message, and optional error for callbacks.
// Triggers callbacks and logs errors if a logger is set.
// No return value; side effects only.
func (r *Renderer) triggerCallbacks(id, status, msg string, err error) {
	r.callbacks.Trigger(id, status, msg, err)
	if err != nil && r.logger != nil {
		r.logger.Log(err)
	}
}

// Push sends a structured Response using the current renderer configuration.
// Takes a Writer and Response struct to encode and send.
// Writes the encoded response with headers, handling errors with fallbacks.
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
	if nr.system.Show == SystemShowBody || nr.system.Show == SystemShowBoth {
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
		if encErr, ok := err.(*EncoderError); ok {
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
			if _, werr := w.Write(encoded); werr != nil {
				wrapped := errors.Join(errWriteFailed, werr)
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

// Raw sends raw data using the current content type.
// Takes an interface{} to encode and send.
// Writes the encoded data with headers, handling errors appropriately.
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
// Takes a callback function that produces data chunks.
// Writes encoded chunks with headers, flushing if supported.
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

// Binary sends binary data with proper headers.
// Takes a content type and byte slice to send.
// Writes the data with headers, handling errors appropriately.
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

// Image encodes and sends an image using the specified content type.
// Takes a content type and image.Image to encode and send.
// Encodes the image and sends it as binary data with headers.
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

// -----------------------------------------------------------------------------
// Convenience Methods
// -----------------------------------------------------------------------------

// Info sends a successful response with an info message.
// Takes a message string and optional info data.
// Sends a Response with StatusSuccessful via Push.
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

// Data sends a successful response with data items.
// Takes a message string and slice of data items.
// Sends a Response with StatusSuccessful via Push.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Data(msg string, data []interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
		Data:    data,
	})
}

// Response sends a successful response with info and data.
// Takes a message, info, and slice of data items.
// Sends a Response with StatusSuccessful via Push.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Response(msg string, info interface{}, data []interface{}) error {
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

// Pending sends a pending response with an info message.
// Takes a message string and optional info data.
// Sends a Response with StatusPending via Push.
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

// Titled sends a successful response with a title and info.
// Takes a title, message, and optional info data.
// Sends a Response with StatusSuccessful via Push.
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

// Error sends an error response with formatted message and errors.
// Takes a format string and one or more errors.
// Sends a Response with StatusError via Push, respecting filters.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Error(format string, errs ...error) error {
	if r.writer == nil {
		return errNoWriter
	}
	for _, filter := range r.errorFilters {
		for _, err := range errs {
			if filter(err) {
				return nil
			}
		}
	}
	joined := errors.Join(errs...)
	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusError
	resp.Message = format + ": " + joined.Error()
	resp.Errors = errs
	return r.WithStatus(http.StatusBadRequest).Push(r.writer, *resp)
}

// Warning sends a warning response with errors.
// Takes one or more errors to include in the response.
// Sends a Response with StatusError via Push, respecting filters.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Warning(errs ...error) error {
	if r.writer == nil {
		return errNoWriter
	}
	for _, filter := range r.errorFilters {
		for _, err := range errs {
			if filter(err) {
				return nil
			}
		}
	}
	joined := errors.Join(errs...)
	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusError
	resp.Message = joined.Error()
	resp.Errors = errs
	return r.WithStatus(http.StatusBadRequest).Push(r.writer, *resp)
}

// Fatal sends a fatal error response and logs the error.
// Takes one or more errors to include in the response.
// Sends a Response with StatusFatal via Push, respecting filters.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Fatal(errs ...error) error {
	if r.writer == nil {
		return errNoWriter
	}
	for _, filter := range r.errorFilters {
		for _, err := range errs {
			if filter(err) {
				return nil
			}
		}
	}
	joined := errors.Join(errs...)
	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusFatal
	resp.Message = joined.Error()
	resp.Errors = errs
	err := r.WithStatus(http.StatusInternalServerError).WithTag(StatusFatal).Push(r.writer, *resp)
	if err == nil && r.logger != nil {
		r.logger.Log(joined)
	}
	return err
}

// Log logs an error if not filtered and a logger is set.
// Takes an error to log.
// Applies error filters and logs via the renderer's logger.
// No return value; side effects only.
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
// Takes a function that processes the renderer and returns an error.
// Wraps the function in an HTTP handler, calling Fatal on errors.
// Returns an http.HandlerFunc for use in HTTP servers.
func (r *Renderer) Handler(fn func(r *Renderer) error) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		renderer := r.WithWriter(w)
		if err := fn(renderer); err != nil {
			renderer.Fatal(err)
		}
	}
}
