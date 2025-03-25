package beam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestLogger is a test implementation of the Logger interface
type TestLogger struct {
	LoggedErrors []error
}

func (tl *TestLogger) Log(err error) bool {
	tl.LoggedErrors = append(tl.LoggedErrors, err)
	return true
}

// TestWriter is a test implementation of the Writer interface
type TestWriter struct {
	Buffer      bytes.Buffer
	Headers     http.Header
	StatusCode  int
	WriteError  error
	HeaderCalls int
}

func (tw *TestWriter) Write(data []byte) (int, error) {
	if tw.WriteError != nil {
		return 0, tw.WriteError
	}
	return tw.Buffer.Write(data)
}

func (tw *TestWriter) Header() http.Header {
	tw.HeaderCalls++
	return tw.Headers
}

func (tw *TestWriter) WriteHeader(statusCode int) {
	tw.StatusCode = statusCode
}

// TestFlusherWriter extends TestWriter with Flusher for streaming tests
type TestFlusherWriter struct {
	TestWriter
	FlushCalled int
}

func (tfw *TestFlusherWriter) Flush() {
	tfw.FlushCalled++
}

var settings = Setting{Name: "test"}

func TestNewRenderer(t *testing.T) {
	t.Run("DefaultSettings", func(t *testing.T) {
		r := New(settings)
		if r.contentType != ContentTypeJSON {
			t.Errorf("Expected default content type %s, got %s", ContentTypeJSON, r.contentType)
		}
		if !r.s.EnableHeaders {
			t.Error("Expected headers enabled by default")
		}
	})

	t.Run("CustomContentType", func(t *testing.T) {
		r := New(settings).WithContentType(ContentTypeXML)
		if r.contentType != ContentTypeXML {
			t.Errorf("Expected content type %s, got %s", ContentTypeXML, r.contentType)
		}
	})
}

func TestRenderer_WithMethods(t *testing.T) {
	base := New(settings)

	t.Run("WithWriter", func(t *testing.T) {
		tw := &TestWriter{}
		r := base.WithWriter(tw)
		if r.writer != tw {
			t.Error("WithWriter did not set the writer")
		}
	})

	t.Run("WithStatus", func(t *testing.T) {
		r := base.WithStatus(http.StatusCreated)
		if r.code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", r.code)
		}
	})

	t.Run("WithHeader", func(t *testing.T) {
		r := base.WithHeader("X-Test", "value")
		if r.header.Get("X-Test") != "value" {
			t.Error("WithHeader did not set the header")
		}
	})

	t.Run("WithMeta", func(t *testing.T) {
		r := base.WithMeta("key", "value")
		if r.meta["key"] != "value" {
			t.Error("WithMeta did not set the metadata")
		}
	})

	t.Run("WithTag", func(t *testing.T) {
		r := base.WithTag("tag1", "tag2")
		if len(r.tags) != 2 || r.tags[0] != "tag1" || r.tags[1] != "tag2" {
			t.Error("WithTag did not set the tags")
		}
	})

	t.Run("WithID", func(t *testing.T) {
		r := base.WithID("test-id")
		if r.id != "test-id" {
			t.Error("WithID did not set the ID")
		}
	})

	t.Run("WithTitle", func(t *testing.T) {
		r := base.WithTitle("test-title")
		if r.title != "test-title" {
			t.Error("WithTitle did not set the title")
		}
	})

	t.Run("WithCallback", func(t *testing.T) {
		called := false
		cb := func(data CallbackData) { called = true }
		r := base.WithCallback(cb)
		r.callbacks.Trigger("test", StatusSuccessful, "", nil)
		if !called {
			t.Error("WithCallback did not register the callback")
		}
	})

	t.Run("WithContentType", func(t *testing.T) {
		r := base.WithContentType(ContentTypeMsgPack)
		if r.contentType != ContentTypeMsgPack {
			t.Errorf("Expected content type %s, got %s", ContentTypeMsgPack, r.contentType)
		}
	})

	t.Run("WithFinalizer", func(t *testing.T) {
		called := false
		f := func(w Writer, err error) { called = true }
		r := base.WithFinalizer(f)
		r.finalizer(nil, nil)
		if !called {
			t.Error("WithFinalizer did not set the finalizer")
		}
	})

	t.Run("WithSystem", func(t *testing.T) {
		sys := System{App: "test-app"}
		r := base.WithSystem(SystemShowHeaders, sys)
		if r.system.App != "test-app" || r.system.Show != SystemShowHeaders {
			t.Error("WithSystem did not configure system settings")
		}
	})
}

func TestRenderer_Push(t *testing.T) {
	t.Run("SuccessfulJSON", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := New(settings).WithWriter(tw)
		resp := Response{
			Status:  StatusSuccessful,
			Message: "test message",
		}

		err := r.Push(tw, resp)
		if err != nil {
			t.Fatalf("Push failed: %v", err)
		}

		if tw.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", tw.StatusCode)
		}

		contentType := tw.Headers.Get("Content-Type")
		if contentType != ContentTypeJSON {
			t.Errorf("Expected content type %s, got %s", ContentTypeJSON, contentType)
		}

		var result Response
		if err := json.Unmarshal(tw.Buffer.Bytes(), &result); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if result.Status != StatusSuccessful || result.Message != "test message" {
			t.Errorf("Unexpected response content: %+v", result)
		}
	})

	t.Run("ErrorHandling", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header), WriteError: fmt.Errorf("write error")}
		r := New(settings).WithWriter(tw)
		resp := Response{Status: StatusSuccessful}

		err := r.Push(tw, resp)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Errorf("Expected write error, got %v", err)
		}
	})

	t.Run("WithSystemInfo", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		sys := System{App: "test-app", Show: SystemShowBody}
		r := New(settings).WithWriter(tw).WithSystem(SystemShowBody, sys)
		resp := Response{Status: StatusSuccessful}

		err := r.Push(tw, resp)
		if err != nil {
			t.Fatalf("Push failed: %v", err)
		}

		var result Response
		if err := json.Unmarshal(tw.Buffer.Bytes(), &result); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if result.Meta == nil || result.Meta["system"].(map[string]interface{})["app"] != "test-app" {
			t.Error("System info not included in response")
		}
	})
}

func TestRenderer_Raw(t *testing.T) {
	t.Run("SuccessfulRaw", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := New(settings).WithWriter(tw)

		err := r.Raw(map[string]string{"key": "value"})
		if err != nil {
			t.Fatalf("Raw failed: %v", err)
		}

		var result map[string]string
		if err := json.Unmarshal(tw.Buffer.Bytes(), &result); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if result["key"] != "value" {
			t.Errorf("Unexpected response content: %+v", result)
		}
	})

	t.Run("NoWriter", func(t *testing.T) {
		r := New(settings) // No writer set

		err := r.Raw("test")
		if err == nil || !strings.Contains(err.Error(), "no writer set") {
			t.Errorf("Expected no writer error, got %v", err)
		}
	})
}

func TestRenderer_Stream(t *testing.T) {
	t.Run("SuccessfulStreamEventStream", func(t *testing.T) {
		tfw := &TestFlusherWriter{TestWriter: TestWriter{Headers: make(http.Header)}}
		r := New(settings).WithContentType(ContentTypeEventStream).WithWriter(tfw)

		count := 0
		err := r.Stream(func(r *Renderer) (interface{}, error) {
			if count >= 2 {
				return nil, io.EOF
			}
			count++
			return Event{ID: fmt.Sprintf("%d", count), Data: "test"}, nil
		})
		if err != nil {
			t.Fatalf("Stream failed: %v", err)
		}

		if tfw.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", tfw.StatusCode)
		}

		contentType := tfw.Headers.Get("Content-Type")
		if contentType != ContentTypeEventStream {
			t.Errorf("Expected content type %s, got %s", ContentTypeEventStream, contentType)
		}

		output := tfw.Buffer.String()
		expected := "id: 1\ndata: \"test\"\n\nid: 2\ndata: \"test\"\n\n"
		if output != expected {
			t.Errorf("Expected output %q, got %q", expected, output)
		}

		if tfw.FlushCalled < 2 {
			t.Errorf("Expected at least 2 flush calls, got %d", tfw.FlushCalled)
		}
	})

	t.Run("SuccessfulStreamJSON", func(t *testing.T) {
		tfw := &TestFlusherWriter{TestWriter: TestWriter{Headers: make(http.Header)}}
		r := New(settings).WithWriter(tfw)

		count := 0
		err := r.Stream(func(r *Renderer) (interface{}, error) {
			if count >= 2 {
				return nil, io.EOF
			}
			count++
			return map[string]int{"count": count}, nil
		})
		if err != nil {
			t.Fatalf("Stream failed: %v", err)
		}

		output := tfw.Buffer.String()
		expected := `{"count":1}{"count":2}`
		if output != expected {
			t.Errorf("Expected output %q, got %q", expected, output)
		}

		if tfw.FlushCalled < 2 {
			t.Errorf("Expected at least 2 flush calls, got %d", tfw.FlushCalled)
		}
	})

	t.Run("NoWriter", func(t *testing.T) {
		r := New(settings).WithContentType(ContentTypeEventStream)
		err := r.Stream(func(r *Renderer) (interface{}, error) {
			return Event{Data: "test"}, nil
		})
		if err == nil || !strings.Contains(err.Error(), "no writer set") {
			t.Errorf("Expected no writer error, got %v", err)
		}
	})

	t.Run("StreamError", func(t *testing.T) {
		tfw := &TestFlusherWriter{TestWriter: TestWriter{Headers: make(http.Header)}}
		r := New(settings).WithContentType(ContentTypeEventStream).WithWriter(tfw)

		testErr := errors.New("stream error")
		err := r.Stream(func(r *Renderer) (interface{}, error) {
			return nil, testErr
		})
		if err == nil || !strings.Contains(err.Error(), "stream callback failed") {
			t.Errorf("Expected stream error, got %v", err)
		}
	})
}

func TestRenderer_Binary(t *testing.T) {
	t.Run("SuccessfulBinary", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := New(settings).WithWriter(tw)
		data := []byte{1, 2, 3, 4}

		err := r.Binary(ContentTypeBinary, data)
		if err != nil {
			t.Fatalf("Binary failed: %v", err)
		}

		if !bytes.Equal(tw.Buffer.Bytes(), data) {
			t.Error("Binary data not written correctly")
		}

		contentType := tw.Headers.Get("Content-Type")
		if contentType != ContentTypeBinary {
			t.Errorf("Expected content type %s, got %s", ContentTypeBinary, contentType)
		}
	})
}

func TestRenderer_Image(t *testing.T) {
	t.Run("SuccessfulPNG", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := New(settings).WithWriter(tw)

		// Create a simple 1x1 image
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.Set(0, 0, color.RGBA{255, 0, 0, 255})

		err := r.Image(ContentTypePNG, img)
		if err != nil {
			t.Fatalf("Image failed: %v", err)
		}

		if tw.Buffer.Len() == 0 {
			t.Error("No image data written")
		}

		contentType := tw.Headers.Get("Content-Type")
		if contentType != ContentTypePNG {
			t.Errorf("Expected content type %s, got %s", ContentTypePNG, contentType)
		}
	})

	t.Run("UnsupportedFormat", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := New(settings).WithWriter(tw)

		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		err := r.Image("unsupported/format", img)
		if err == nil || !strings.Contains(err.Error(), "unsupported image content type") {
			t.Errorf("Expected unsupported format error, got %v", err)
		}
	})
}

func TestRenderer_ConvenienceMethods(t *testing.T) {
	tw := &TestWriter{Headers: make(http.Header)}
	r := New(settings).WithWriter(tw)

	t.Run("Error", func(t *testing.T) {
		testErr := errors.New("test error")
		err := r.Error("error occurred: %v", testErr)
		if err != nil {
			t.Fatalf("Error failed: %v", err)
		}

		var result Response
		if err := json.Unmarshal(tw.Buffer.Bytes(), &result); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if result.Status != StatusError || len(result.Errors) != 1 {
			t.Errorf("Unexpected error response: %+v", result)
		}
		tw.Buffer.Reset()
	})

	t.Run("Fatal", func(t *testing.T) {
		testLogger := &TestLogger{}
		testErr := errors.New("fatal error")
		r := r.SetLogger(testLogger)

		err := r.Fatal(testErr)
		if err != nil {
			t.Fatalf("Fatal failed: %v", err)
		}

		var result Response
		if err := json.Unmarshal(tw.Buffer.Bytes(), &result); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if result.Status != StatusFatal {
			t.Errorf("Expected status %s, got %s", StatusFatal, result.Status)
		}
		tw.Buffer.Reset()
	})
}

func TestRenderer_Handler(t *testing.T) {
	t.Run("SuccessfulHandler", func(t *testing.T) {
		r := New(settings)
		handler := r.Handler(func(r *Renderer) error {
			return r.Info("handler test", nil)
		})

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var result Response
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if result.Status != StatusSuccessful || result.Message != "handler test" {
			t.Errorf("Unexpected handler response: %+v", result)
		}
	})

	t.Run("HandlerError", func(t *testing.T) {
		testLogger := &TestLogger{}
		r := New(settings).SetLogger(testLogger)
		handler := r.Handler(func(r *Renderer) error {
			return fmt.Errorf("handler error")
		})

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", w.Code)
		}

		if len(testLogger.LoggedErrors) != 1 || testLogger.LoggedErrors[0].Error() != "handler error" {
			t.Error("Handler error was not logged")
		}
	})
}

func TestErrorFilters(t *testing.T) {
	t.Run("SkipError", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := New(settings).WithWriter(tw)

		err := r.Error("test", ErrSkip)
		if err != nil {
			t.Fatalf("Error returned unexpected error: %v", err)
		}

		if tw.Buffer.Len() != 0 {
			t.Error("Error response was written despite skip error")
		}
	})

	t.Run("CustomFilter", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		customErr := errors.New("custom error")
		r := New(settings).
			WithWriter(tw).
			WithErrorFilters(func(err error) bool {
				return errors.Is(err, customErr)
			})

		err := r.Error("test", customErr)
		if err != nil {
			t.Fatalf("Error returned unexpected error: %v", err)
		}

		if tw.Buffer.Len() != 0 {
			t.Error("Error response was written despite filtered error")
		}
	})
}

func TestContextCancellation(t *testing.T) {
	t.Run("PushWithCancelledContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		tw := &TestWriter{Headers: make(http.Header)}
		r := New(settings).
			WithWriter(tw).
			WithIDGeneration(true).
			WithContext(ctx)

		err := r.Push(tw, Response{Status: StatusSuccessful})
		if !errors.Is(err, ErrContextCanceled) {
			t.Errorf("Expected context canceled error, got %v", err)
		}

		if tw.Buffer.Len() != 0 {
			t.Error("Data was written despite cancelled context")
		}
	})
}

func TestEncoderErrorHandling(t *testing.T) {

	t.Run("XMLEncodingError", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := New(settings).
			WithWriter(tw).
			WithContentType(ContentTypeXML)

		// Create a value that XML can't encode natively
		data := struct {
			Channel chan int `xml:"channel"`
		}{
			Channel: make(chan int),
		}

		err := r.Push(tw, Response{Data: []interface{}{data}})
		if err == nil {
			t.Fatal("Expected an encoding error")
		}

		// Check if we got an EncoderError
		var encErr *EncoderError
		if !errors.As(err, &encErr) {
			t.Errorf("Expected EncoderError, got %T", err)
		}

		// Verify we got an XML error response
		output := tw.Buffer.String()
		if !strings.Contains(output, "<error>") {
			t.Errorf("Expected XML error response, got %q", output)
		}
	})

	t.Run("JSONEncodingError", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := New(settings).
			WithWriter(tw).
			WithContentType(ContentTypeJSON)

		// Create a value that JSON can't encode
		data := struct {
			Channel chan int `json:"channel"`
		}{
			Channel: make(chan int),
		}

		err := r.Push(tw, Response{Data: []interface{}{data}})
		if err == nil {
			t.Fatal("Expected an encoding error")
		}

		// Check if we got an EncoderError
		var encErr *EncoderError
		if !errors.As(err, &encErr) {
			t.Errorf("Expected EncoderError, got %T", err)
		}

		// Verify we got a JSON error response
		var resp map[string]interface{}
		if err := json.Unmarshal(tw.Buffer.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to unmarshal error response: %v", err)
		}
		if resp["error"] == nil {
			t.Errorf("Expected error in response, got %+v", resp)
		}
	})

	t.Run("SystemStructWithBody", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		sys := System{
			App:      "test-app",
			Version:  "1.0",
			Play:     true,
			Duration: 0, // Initialize with zero value
		}
		r := New(settings).
			WithWriter(tw).
			WithContentType(ContentTypeJSON).
			WithSystem(SystemShowBody, sys)

		// Override the start time to control duration
		r.start = time.Now().Add(-2 * time.Second) // Fixed 2 second duration

		err := r.Push(tw, Response{Status: StatusSuccessful, Message: "test"})
		if err != nil {
			t.Fatalf("Push failed: %v", err)
		}

		// Parse the response
		var resp Response
		if err := json.Unmarshal(tw.Buffer.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		// Verify system metadata
		if resp.Meta == nil {
			t.Fatal("Expected meta field with system info")
		}

		system, ok := resp.Meta["system"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected system info as map, got %T", resp.Meta["system"])
		}

		// Verify fixed fields
		if system["app"] != "test-app" || system["version"] != "1.0" || system["play"] != true {
			t.Errorf("System info mismatch: got %+v", system)
		}

		// Verify duration is present and formatted
		duration, ok := system["duration"].(string)
		if !ok {
			t.Errorf("Expected duration as string, got %T", system["duration"])
		} else if duration != "2s" { // Should match our 2 second fixed duration
			t.Errorf("Expected duration '2s', got %q", duration)
		}
	})

	t.Run("SystemStructWithBoth", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		sys := System{
			App:     "test-app",
			Server:  "localhost",
			Version: "2.0",
		}
		r := New(settings).
			WithWriter(tw).
			WithContentType(ContentTypeXML).
			WithSystem(SystemShowBoth, sys)

		err := r.Push(tw, Response{Status: StatusSuccessful})
		if err != nil {
			t.Fatalf("Push failed: %v", err)
		}

		// Verify headers
		if tw.Headers.Get("X-test-App") != "test-app" || tw.Headers.Get("X-test-Server") != "localhost" {
			t.Errorf("Expected system info in headers, got %+v", tw.Headers)
		}

		// Verify body contains expected system info
		output := tw.Buffer.String()
		if !strings.Contains(output, "<App>test-app</App>") ||
			!strings.Contains(output, "<Server>localhost</Server>") ||
			!strings.Contains(output, "<Version>2.0</Version>") {
			t.Errorf("Expected system info in XML body, got %q", output)
		}
		if !strings.Contains(output, "<Duration>") {
			t.Errorf("Expected duration in XML body, got %q", output)
		}
	})
}
