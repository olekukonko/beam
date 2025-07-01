package beam

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/HugoSmits86/nativewebp"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"time"
)

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
	contentType  string             // Current content type (e.g., "application/json")
	errorFilters []func(error) bool // Renderer-level error filters
	logger       Logger             // Optional logger
	writer       Writer             // Default writer
	finalizer    Finalizer          // Error finalizer
	generateID   bool               // Enable automatic ID generation
	system       System             // System metadata configuration
}

// NewRenderer creates a new Renderer with the provided settings and default content type.
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

// WithContext sets the context for the renderer.
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
	if nr.meta == nil {
		nr.meta = make(map[string]interface{})
	}
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
func (r *Renderer) UseEncoder(e Encoder) *Renderer {
	nr := r.clone()
	nr.encoders.Register(e)
	return nr
}

// WithContentType sets the output content type.
func (r *Renderer) WithContentType(contentType string) *Renderer {
	nr := r.clone()
	nr.contentType = contentType
	return nr
}

// WithProtocol sets the protocol handler.
func (r *Renderer) WithProtocol(p Protocol) *Renderer {
	nr := r.clone()
	nr.protocol = NewProtocolHandler(p)
	return nr
}

// -----------------------------------------------------------------------------
// Renderer Core Methods
// -----------------------------------------------------------------------------

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
		prefix := HeaderPrefix
		if r.s.Name != Empty {
			prefix = fmt.Sprintf("X-%s", r.s.Name)
		}
		r.header.Set(fmt.Sprintf("%s-%s", prefix, key), value)
	}

	if r.s.EnableHeaders {
		r.header.Set(HeaderContentType, contentType)
		// Optionally include system metadata in headers.
		if r.system.Show == SystemShowHeaders || r.system.Show == SystemShowBoth {
			setHeader(HeaderNameDuration, time.Since(r.start).String())
			setHeader(HeaderNameTimestamp, fmt.Sprintf("%d", time.Now().Unix()))
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
			setHeader(HeaderNamePlay, fmt.Sprintf("%t", r.system.Play))
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
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}

	if nr.generateID && nr.id == Empty {
		nr.id = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}

	if d.Status == Empty {
		d.Status = StatusSuccessful
	}
	if d.Title == Empty && d.Status == StatusError {
		d.Title = "error"
	}

	// Set default status codes if not already defined.
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

	// If system display is enabled, include system info in meta.
	if nr.system.Show == SystemShowBody || nr.system.Show == SystemShowBoth {
		if d.Meta == nil {
			d.Meta = make(map[string]interface{})
		}
		sysCopy := nr.system
		sysCopy.Duration = time.Since(nr.start).Truncate(time.Second)
		d.Meta["system"] = sysCopy
	}

	// Use the fallback-capable encoder.
	encoded, err := nr.encoders.EncodeWithFallback(nr.contentType, d)
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
				wrapped := fmt.Errorf("write failed: %w", werr)
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
		wrapped := fmt.Errorf("unexpected encoding error: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	if err := nr.applyCommonHeaders(w, nr.contentType); err != nil {
		wrapped := fmt.Errorf("header write failed: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	if _, err := w.Write(encoded); err != nil {
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

// Raw sends raw data using the current content type.
func (r *Renderer) Raw(data interface{}) error {
	nr := r.clone()
	nr.start = time.Now()
	w := nr.writer
	if w == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	if nr.generateID && nr.id == Empty {
		nr.id = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	if nr.code == 0 {
		nr.code = http.StatusOK // Default for Raw
	}

	encoded, err := nr.encoders.Encode(nr.contentType, data)
	if err != nil {
		wrapped := fmt.Errorf("encoding failed: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	if err := nr.applyCommonHeaders(w, nr.contentType); err != nil {
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

// Stream sends data incrementally using a callback to produce chunks.
func (r *Renderer) Stream(callback func(*Renderer) (interface{}, error)) error {
	nr := r.clone()
	nr.start = time.Now()
	w := nr.writer
	if w == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	if nr.generateID && nr.id == Empty {
		nr.id = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	if nr.code == 0 {
		nr.code = http.StatusOK // Default for Stream
	}

	// Check if the encoder supports streaming
	encoder, ok := nr.encoders.Get(nr.contentType)
	if !ok {
		return fmt.Errorf("no encoder for content type %s", nr.contentType)
	}
	if streamer, supportsStreaming := encoder.(Streamer); supportsStreaming {
		// Delegate to the encoder's streaming implementation
		if err := nr.applyCommonHeaders(w, nr.contentType); err != nil {
			wrapped := fmt.Errorf("header write failed: %w", err)
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
		wrapped := fmt.Errorf("header write failed: %w", err)
		nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
		if nr.finalizer != nil {
			nr.finalizer(w, wrapped)
		}
		return wrapped
	}

	for {
		data, err := callback(nr)
		if err != nil {
			if errors.Is(err, io.EOF) { // End of stream
				nr.triggerCallbacks(nr.id, StatusSuccessful, "Stream completed", nil)
				return nil
			}
			wrapped := fmt.Errorf("stream callback failed: %w", err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}

		encoded, err := nr.encoders.Encode(nr.contentType, data)
		if err != nil {
			wrapped := fmt.Errorf("encoding failed: %w", err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}

		if _, err := w.Write(encoded); err != nil {
			wrapped := fmt.Errorf("write failed: %w", err)
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
func (r *Renderer) Binary(contentType string, data []byte) error {
	nr := r.clone()
	nr.start = time.Now()
	w := nr.writer
	if w == nil {
		return fmt.Errorf("no writer set; use WithWriter to set a default writer")
	}
	if nr.generateID && nr.id == Empty {
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
	if nr.generateID && nr.id == Empty {
		nr.id = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	if nr.code == 0 {
		nr.code = http.StatusOK // Default for Image
	}

	buf := new(bytes.Buffer)
	switch contentType {
	case ContentTypePNG:
		if err := png.Encode(buf, img); err != nil {
			wrapped := fmt.Errorf("PNG encoding failed: %w", err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}
	case ContentTypeJPEG:
		opts := &jpeg.Options{Quality: 80}
		if err := jpeg.Encode(buf, img, opts); err != nil {
			wrapped := fmt.Errorf("JPEG encoding failed: %w", err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}
	case ContentTypeGIF:
		if err := gif.Encode(buf, img, nil); err != nil {
			wrapped := fmt.Errorf("GIF encoding failed: %w", err)
			nr.triggerCallbacks(nr.id, StatusFatal, wrapped.Error(), wrapped)
			if nr.finalizer != nil {
				nr.finalizer(w, wrapped)
			}
			return wrapped
		}

	case ContentTypeWebP:
		if err := nativewebp.Encode(buf, img, nil); err != nil {
			wrapped := fmt.Errorf("WebP encoding failed: %w", err)
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
