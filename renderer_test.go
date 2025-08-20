// renderer_test.go
package beam

import (
	"bytes"
	"context"
	"database/sql"
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
type LogEntry struct {
	Level  string
	Err    error
	Fields []interface{}
}

// TestLogger is a test implementation of the beam.Logger interface
// that captures structured, level-aware log entries.
type TestLogger struct {
	Entries []LogEntry
}

func (tl *TestLogger) Error(err error, fields ...interface{}) {
	tl.Entries = append(tl.Entries, LogEntry{
		Level:  "error",
		Err:    err,
		Fields: fields,
	})
}

func (tl *TestLogger) Fatal(err error, fields ...interface{}) {
	tl.Entries = append(tl.Entries, LogEntry{
		Level:  "fatal",
		Err:    err,
		Fields: fields,
	})
}

// LastEntry returns the most recent log entry, if any.
func (tl *TestLogger) LastEntry() *LogEntry {
	if len(tl.Entries) == 0 {
		return nil
	}
	return &tl.Entries[len(tl.Entries)-1]
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
		r := NewRenderer(settings)
		if r.contentType != ContentTypeJSON {
			t.Errorf("Expected default content type %s, got %s", ContentTypeJSON, r.contentType)
		}
		if !r.s.EnableHeaders {
			t.Error("Expected headers enabled by default")
		}
	})

	t.Run("CustomContentType", func(t *testing.T) {
		r := NewRenderer(settings).WithContentType(ContentTypeXML)
		if r.contentType != ContentTypeXML {
			t.Errorf("Expected content type %s, got %s", ContentTypeXML, r.contentType)
		}
	})
}

func TestRenderer_WithMethods(t *testing.T) {
	base := NewRenderer(settings)

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
		if r.system.App != "test-app" || r.showSystem != SystemShowHeaders {
			t.Error("WithSystem did not configure system settings")
		}
	})
}

func TestRenderer_Push(t *testing.T) {
	t.Run("SuccessfulJSON", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := NewRenderer(settings).WithWriter(tw)
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
		r := NewRenderer(settings).WithWriter(tw)
		resp := Response{Status: StatusSuccessful}

		err := r.Push(tw, resp)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Errorf("Expected write error, got %v", err)
		}
	})

	t.Run("WithSystemInfo", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		sys := System{App: "test-app"}
		r := NewRenderer(settings).WithWriter(tw).WithSystem(SystemShowBody, sys)
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
		r := NewRenderer(settings).WithWriter(tw)

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
		r := NewRenderer(settings) // No writer set

		err := r.Raw("test")
		if err == nil || !strings.Contains(err.Error(), "no writer set") {
			t.Errorf("Expected no writer error, got %v", err)
		}
	})
}

func TestRenderer_Stream(t *testing.T) {
	t.Run("SuccessfulStreamEventStream", func(t *testing.T) {
		tfw := &TestFlusherWriter{TestWriter: TestWriter{Headers: make(http.Header)}}
		r := NewRenderer(settings).WithContentType(ContentTypeEventStream).WithWriter(tfw)

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
		r := NewRenderer(settings).WithWriter(tfw)

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
		r := NewRenderer(settings).WithContentType(ContentTypeEventStream)
		err := r.Stream(func(r *Renderer) (interface{}, error) {
			return Event{Data: "test"}, nil
		})
		if err == nil || !strings.Contains(err.Error(), "no writer set") {
			t.Errorf("Expected no writer error, got %v", err)
		}
	})

	t.Run("StreamError", func(t *testing.T) {
		tfw := &TestFlusherWriter{TestWriter: TestWriter{Headers: make(http.Header)}}
		r := NewRenderer(settings).WithContentType(ContentTypeEventStream).WithWriter(tfw)

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
		r := NewRenderer(settings).WithWriter(tw)
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
		r := NewRenderer(settings).WithWriter(tw)

		// Create a simple 1x1 image
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.Set(0, 0, color.RGBA{R: 255, A: 255})

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
		r := NewRenderer(settings).WithWriter(tw)

		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		err := r.Image("unsupported/format", img)
		if err == nil || !strings.Contains(err.Error(), "unsupported image content type") {
			t.Errorf("Expected unsupported format error, got %v", err)
		}
	})
}

func TestRenderer_ConvenienceMethods(t *testing.T) {
	tw := &TestWriter{Headers: make(http.Header)}
	r := NewRenderer(settings).WithWriter(tw)

	t.Run("Error", func(t *testing.T) {
		testErr := errors.New("test error")
		err := r.Errorf("error occurred: %v", testErr)
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
		r := r.WithLogger(testLogger)

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
		r := NewRenderer(settings)
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
		r := NewRenderer(settings).WithLogger(testLogger)
		handler := r.Handler(func(r *Renderer) error {
			return fmt.Errorf("handler error")
		})

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", w.Code)
		}

		if len(testLogger.Entries) < 1 {
			t.Error("Handler error was not logged")
		}
	})
}

func TestContextCancellation(t *testing.T) {
	t.Run("PushWithCancelledContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		tw := &TestWriter{Headers: make(http.Header)}
		r := NewRenderer(settings).
			WithWriter(tw).
			WithIDGeneration(Yes).
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
		r := NewRenderer(settings).
			WithWriter(tw).
			WithContentType(ContentTypeXML)

		data := struct {
			Channel chan int `xml:"channel"`
		}{
			Channel: make(chan int),
		}

		err := r.Push(tw, Response{Data: data})
		if err == nil {
			t.Fatal("Expected an encoding error")
		}

		var encErr *EncoderError
		if !errors.As(err, &encErr) {
			t.Errorf("Expected EncoderError, got %T", err)
		}

		output := tw.Buffer.String()
		if !strings.Contains(output, "<error>") {
			t.Errorf("Expected XML error response, got %q", output)
		}
	})

	t.Run("JSONEncodingError", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := NewRenderer(settings).
			WithWriter(tw).
			WithContentType(ContentTypeJSON)

		data := struct {
			Channel chan int `json:"channel"`
		}{
			Channel: make(chan int),
		}

		err := r.Push(tw, Response{Data: data})
		if err == nil {
			t.Fatal("Expected an encoding error")
		}

		var encErr *EncoderError
		if !errors.As(err, &encErr) {
			t.Errorf("Expected EncoderError, got %T", err)
		}

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
			App:     "test-app",
			Version: "1.0",
			Play:    true,
		}
		r := NewRenderer(settings).
			WithWriter(tw).
			WithContentType(ContentTypeJSON).
			WithSystem(SystemShowBody, sys)

		r.start = time.Now().Add(-2 * time.Second)

		err := r.Push(tw, Response{Status: StatusSuccessful, Message: "test"})
		if err != nil {
			t.Fatalf("Push failed: %v", err)
		}

		var resp Response
		if err := json.Unmarshal(tw.Buffer.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if resp.Meta == nil {
			t.Fatal("Expected meta field with system info")
		}

		system, ok := resp.Meta["system"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected system info as map, got %T", resp.Meta["system"])
		}

		if system["app"] != "test-app" || system["version"] != "1.0" || system["play"] != true {
			t.Errorf("System info mismatch: got %+v", system)
		}

		duration, ok := system["duration"].(string)
		if !ok {
			t.Errorf("Expected duration as string, got %T", system["duration"])
		} else if !strings.HasPrefix(duration, "2.") && duration != "2s" {
			t.Errorf("Expected duration around '2s', got %q", duration)
		}
	})

	t.Run("SystemStructWithBoth", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		sys := System{
			App:     "test-app",
			Server:  "localhost",
			Version: "2.0",
		}
		r := NewRenderer(settings).
			WithWriter(tw).
			WithContentType(ContentTypeXML).
			WithSystem(SystemShowBoth, sys)

		err := r.Push(tw, Response{Status: StatusSuccessful})
		if err != nil {
			t.Fatalf("Push failed: %v", err)
		}

		if tw.Headers.Get("X-test-App") != "test-app" || tw.Headers.Get("X-test-Server") != "localhost" {
			t.Errorf("Expected system info in headers, got %+v", tw.Headers)
		}

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

func TestEventStreamEncoderFormat(t *testing.T) {
	enc := &EventStreamEncoder{}
	event := Event{ID: "1", Data: "test", Type: "message"}
	b, err := enc.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	expected := "id: 1\nevent: message\ndata: \"test\"\n\n"
	if string(b) != expected {
		t.Errorf("Expected %q, got %q", expected, string(b))
	}
}

func TestResponsePoolReset(t *testing.T) {
	r := getResponse()
	r.Status = "test"
	r.Meta["key"] = "value"
	r.Tags = []string{"tag"}
	r.Errors = []error{errors.New("error")}
	putResponse(r)
	r2 := getResponse()
	if r2.Status != "" || len(r2.Meta) != 0 || len(r2.Tags) != 0 || len(r2.Errors) != 0 {
		t.Errorf("Response pool did not reset fields: %+v", r2)
	}
}

type testWriter struct {
	buffer  bytes.Buffer
	headers http.Header
}

func (tw *testWriter) Write(data []byte) (int, error) {
	return tw.buffer.Write(data)
}

func (tw *testWriter) Header() http.Header {
	return tw.headers
}

func (tw *testWriter) WriteHeader(statusCode int) {}

func TestSpecificCase(t *testing.T) {
	tw := &testWriter{headers: make(http.Header)}
	r := NewRenderer(Setting{Name: "test"}).
		WithWriter(tw).
		WithProtocol(&TCPProtocol{})

	_ = r.Errorf("problems: %v, %v",
		errors.New("first"), ErrSkip, errors.New("third"))

	var resp Response
	json.Unmarshal(tw.buffer.Bytes(), &resp)

	expectedMsg := "problems: first, %!v(MISSING)"
	if resp.Message != expectedMsg {
		t.Errorf("Expected message %q, got %q", expectedMsg, resp.Message)
	}
	if len(resp.Errors) != 2 || resp.Errors[0].Error() != "first" || resp.Errors[1].Error() != "third" {
		t.Errorf("Expected errors [first, third], got %v", resp.Errors)
	}
}

func TestErrorFormatting(t *testing.T) {
	tests := []struct {
		name           string
		format         string
		args           []interface{}
		expectedMsg    string
		expectedErrors []string
		shouldSkip     bool
	}{
		{
			name:           "No errors",
			format:         "simple message",
			args:           nil,
			expectedMsg:    "simple message",
			expectedErrors: nil,
			shouldSkip:     false,
		},
		{
			name:           "Single error with %v",
			format:         "error: %v",
			args:           []interface{}{errors.New("file not found")},
			expectedMsg:    "error: file not found",
			expectedErrors: []string{"file not found"},
			shouldSkip:     false,
		},
		{
			name:           "Single error with %w",
			format:         "wrapped: %w",
			args:           []interface{}{errors.New("permission denied")},
			expectedMsg:    "wrapped: permission denied",
			expectedErrors: []string{"permission denied"},
			shouldSkip:     false,
		},
		{
			name:           "Multiple errors with format",
			format:         "errors: %v, %v",
			args:           []interface{}{errors.New("network timeout"), errors.New("invalid input")},
			expectedMsg:    "errors: network timeout, invalid input",
			expectedErrors: []string{"network timeout", "invalid input"},
			shouldSkip:     false,
		},
		{
			name:           "More verbs than args",
			format:         "missing: %v, %v, %v",
			args:           []interface{}{errors.New("only one")},
			expectedMsg:    "missing: only one, %!v(MISSING), %!v(MISSING)",
			expectedErrors: []string{"only one"},
			shouldSkip:     false,
		},
		{
			name:           "Mixed arguments",
			format:         "User %s failed: %v",
			args:           []interface{}{"john", errors.New("validation error")},
			expectedMsg:    "User john failed: validation error",
			expectedErrors: []string{"validation error"},
			shouldSkip:     false,
		},
		{
			name:       "With ErrSkip",
			format:     "should be skipped",
			args:       []interface{}{ErrSkip},
			shouldSkip: true,
		},
		{
			name:           "Multiple errors including ErrSkip",
			format:         "problems: %v, %v",
			args:           []interface{}{errors.New("first"), ErrSkip, errors.New("third")},
			expectedMsg:    "problems: first, %!v(MISSING)",
			expectedErrors: []string{"first", "third"},
			shouldSkip:     false,
		},
		{
			name:           "Non-error arguments",
			format:         "Value: %s, Number: %d",
			args:           []interface{}{"test", 42},
			expectedMsg:    "Value: test, Number: 42",
			expectedErrors: nil,
			shouldSkip:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tw := &testWriter{headers: make(http.Header)}
			r := NewRenderer(Setting{Name: "test"}).
				WithWriter(tw).
				WithProtocol(&TCPProtocol{})

			err := r.Errorf(tt.format, tt.args...)

			if tt.shouldSkip {
				if err != nil {
					t.Errorf("Expected nil error for skipped case, got %v", err)
				}
				if tw.buffer.Len() != 0 {
					t.Error("Expected no output for skipped case")
				}
				return
			}

			if err != nil {
				t.Fatalf("Errorf returned unexpected error: %v", err)
			}

			var resp Response
			if err := json.Unmarshal(tw.buffer.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if resp.Message != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, resp.Message)
			}

			if len(tt.expectedErrors) != len(resp.Errors) {
				t.Fatalf("Expected %d errors in response, got %d: %v",
					len(tt.expectedErrors), len(resp.Errors), resp.Errors)
			}

			for i, expectedError := range tt.expectedErrors {
				actualError := resp.Errors[i].Error()
				if actualError != expectedError {
					t.Errorf("Error %d: expected %q, got %q", i, expectedError, actualError)
				}
			}
		})
	}
}

func TestErrorFilters(t *testing.T) {
	tests := []struct {
		name        string
		filter      func(error) bool
		err         error
		shouldWrite bool
	}{
		{
			name:        "Default filter with sql.ErrNoRows",
			filter:      nil, // Use default filter
			err:         sql.ErrNoRows,
			shouldWrite: false,
		},
		{
			name:        "Default filter with ErrSkip",
			filter:      nil,
			err:         ErrSkip,
			shouldWrite: false,
		},
		{
			name: "Custom filter matching error",
			filter: func(err error) bool {
				return err.Error() == "custom error"
			},
			err:         errors.New("custom error"),
			shouldWrite: false,
		},
		{
			name: "Custom filter not matching",
			filter: func(err error) bool {
				return false
			},
			err:         errors.New("some error"),
			shouldWrite: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tw := &testWriter{headers: make(http.Header)}
			r := NewRenderer(Setting{Name: "test"}).
				WithWriter(tw).
				WithProtocol(&TCPProtocol{})

			if tt.filter != nil {
				r = r.WithFilters(tt.filter)
			}

			err := r.Errorf("test error: %v", tt.err)
			if err != nil {
				t.Fatalf("Errorf returned unexpected error: %v", err)
			}

			if tt.shouldWrite && tw.buffer.Len() == 0 {
				t.Error("Expected error response to be written, but it wasn't")
			} else if !tt.shouldWrite && tw.buffer.Len() != 0 {
				t.Errorf("Expected no error response to be written, but it was. Got: %s", tw.buffer.String())
			}
		})
	}
}

func TestErrorWithNil(t *testing.T) {
	tw := &testWriter{headers: make(http.Header)}
	r := NewRenderer(Setting{Name: "test"}).
		WithWriter(tw).
		WithProtocol(&TCPProtocol{})

	err := r.Errorf("test with nil")
	if err != nil {
		t.Fatalf("Errorf returned unexpected error: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(tw.buffer.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Message != "test with nil" {
		t.Errorf("Expected message %q, got %q", "test with nil", resp.Message)
	}
	if len(resp.Errors) != 0 {
		t.Errorf("Expected no errors in response, got %d", len(resp.Errors))
	}
}

func TestErrHiddenFunctionality(t *testing.T) {
	tests := []struct {
		name           string
		format         string
		args           []interface{}
		expectedMsg    string
		expectedErrors []string
		shouldSkip     bool
	}{
		{
			name:           "Direct ErrHidden",
			format:         "Error: %v",
			args:           []interface{}{ErrHidden},
			expectedMsg:    "Error: *hidden*",
			expectedErrors: nil,
		},
		{
			name:           "Wrapped ErrHidden with fmt.Errorf",
			format:         "Wrapped: %v",
			args:           []interface{}{fmt.Errorf("context: %w", ErrHidden)},
			expectedMsg:    "Wrapped: *hidden*",
			expectedErrors: nil,
		},
		{
			name:           "Mixed hidden and visible errors",
			format:         "Problems: %v, %v, %v",
			args:           []interface{}{errors.New("file not found"), ErrHidden, errors.New("timeout")},
			expectedMsg:    "Problems: file not found, *hidden*, timeout",
			expectedErrors: []string{"file not found", "timeout"},
		},
		{
			name:           "Mixed ErrHidden and ErrSkip",
			format:         "Mixed: %v, %v",
			args:           []interface{}{ErrHidden, ErrSkip},
			expectedMsg:    "Mixed: *hidden*, %!v(MISSING)",
			expectedErrors: nil,
		},
		{
			name:       "Only ErrSkip should skip response",
			format:     "This should not appear",
			args:       []interface{}{ErrSkip},
			shouldSkip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tw := &testWriter{headers: make(http.Header)}
			r := NewRenderer(Setting{Name: "test"}).
				WithWriter(tw).
				WithProtocol(&TCPProtocol{})

			err := r.Errorf(tt.format, tt.args...)

			if tt.shouldSkip {
				if err != nil {
					t.Errorf("Expected nil error for skipped case, got %v", err)
				}
				if tw.buffer.Len() != 0 {
					t.Error("Expected no output for skipped case")
				}
				return
			}

			if err != nil {
				t.Fatalf("Errorf returned unexpected error: %v", err)
			}

			var resp Response
			if err := json.Unmarshal(tw.buffer.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v. Body: %s", err, tw.buffer.String())
			}

			if resp.Message != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, resp.Message)
			}

			if len(tt.expectedErrors) != len(resp.Errors) {
				t.Fatalf("Expected %d errors in response, got %d: %v",
					len(tt.expectedErrors), len(resp.Errors), resp.Errors)
			}

			for i, expectedError := range tt.expectedErrors {
				actualError := resp.Errors[i].Error()
				if actualError != expectedError {
					t.Errorf("Error %d: expected %q, got %q", i, expectedError, actualError)
				}
			}
		})
	}
}

func TestFilterErrorsWithErrHidden(t *testing.T) {
	r := NewRenderer(Setting{Name: "test"})

	tests := []struct {
		name     string
		input    []error
		expected int // expected number of errors after filtering
	}{
		{
			name:     "Only ErrHidden",
			input:    []error{ErrHidden},
			expected: 0,
		},
		{
			name:     "Mixed errors",
			input:    []error{errors.New("visible"), ErrHidden, errors.New("another")},
			expected: 2,
		},
		{
			name:     "Wrapped ErrHidden",
			input:    []error{fmt.Errorf("wrapped: %w", ErrHidden)},
			expected: 0,
		},
		{
			name:     "ErrHidden and ErrSkip",
			input:    []error{ErrHidden, ErrSkip},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.filterErrors(tt.input)
			if len(result) != tt.expected {
				t.Errorf("Expected %d errors, got %d: %v", tt.expected, len(result), result)
			}

			for _, err := range result {
				if errors.Is(err, ErrHidden) {
					t.Errorf("ErrHidden should be filtered out, but found: %v", err)
				}
			}
		})
	}
}

func TestErrorWithNilHandling(t *testing.T) {
	tests := []struct {
		name               string
		message            string
		errs               []error
		expectResponse     bool
		expectedErrorCount int
	}{
		{
			name:               "with nil error only",
			message:            "A message",
			errs:               []error{nil},
			expectResponse:     true,
			expectedErrorCount: 0,
		},
		{
			name:               "with no errors provided",
			message:            "Another message",
			errs:               []error{},
			expectResponse:     true,
			expectedErrorCount: 0,
		},
		{
			name:               "with a single real error",
			message:            "Real error occurred",
			errs:               []error{errors.New("something went wrong")},
			expectResponse:     true,
			expectedErrorCount: 1,
		},
		{
			name:               "with real and nil errors",
			message:            "Mixed errors",
			errs:               []error{errors.New("real error"), nil, errors.New("another real error")},
			expectResponse:     true,
			expectedErrorCount: 2,
		},
		{
			name:               "with only a skippable error",
			message:            "This should be skipped",
			errs:               []error{ErrSkip},
			expectResponse:     false,
			expectedErrorCount: 0,
		},
		{
			name:               "with skippable and real errors",
			message:            "Should show one error",
			errs:               []error{ErrSkip, errors.New("visible error")},
			expectResponse:     true,
			expectedErrorCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			renderer := NewRenderer(Setting{}).WithWriter(w)

			err := renderer.ErrorWith(tt.message, tt.errs...)
			if err != nil {
				t.Fatalf("ErrorWith returned an unexpected error: %v", err)
			}

			if !tt.expectResponse {
				if w.Body.Len() != 0 {
					t.Errorf("expected no response, but a response was sent with body: %s", w.Body.String())
				}
				return
			}

			if w.Body.Len() == 0 {
				t.Fatal("expected a response, but no response was sent")
			}

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected status %d, but got %d", http.StatusBadRequest, w.Code)
			}

			var resp Response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response body: %v", err)
			}

			if resp.Message != tt.message {
				t.Errorf("expected message %q, but got %q", tt.message, resp.Message)
			}

			if len(resp.Errors) != tt.expectedErrorCount {
				t.Errorf("expected %d errors in response, but got %d", tt.expectedErrorCount, len(resp.Errors))
			}
		})
	}
}

func TestFatalMethods(t *testing.T) {
	tests := []struct {
		name                       string
		testFunc                   func(r *Renderer) error
		expectResponse             bool
		expectLog                  bool
		expectedResponseMessage    string
		expectedResponseErrorCount int
		expectedLogFieldsCount     int
	}{
		{
			name:                       "Fatal with a real error",
			testFunc:                   func(r *Renderer) error { return r.Fatal(errors.New("db connection failed")) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    defaultFatalMessage,
			expectedResponseErrorCount: 1,
			expectedLogFieldsCount:     0,
		},
		{
			name:                       "FatalWith with a real error",
			testFunc:                   func(r *Renderer) error { return r.FatalWith("failed to load user", errors.New("user not found")) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "failed to load user",
			expectedResponseErrorCount: 1,
			expectedLogFieldsCount:     0,
		},
		{
			name:                       "Fatalf with a real error",
			testFunc:                   func(r *Renderer) error { return r.Fatalf("hello : %v", errors.New("test error")) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "hello : test error",
			expectedResponseErrorCount: 1,
			expectedLogFieldsCount:     0,
		},
		{
			name:                       "Fatal with multiple real errors",
			testFunc:                   func(r *Renderer) error { return r.Fatal(errors.New("error1"), errors.New("error2")) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    defaultFatalMessage,
			expectedResponseErrorCount: 2,
			expectedLogFieldsCount:     1,
		},
		{
			name:                       "FatalWith with only nil error MUST log and respond",
			testFunc:                   func(r *Renderer) error { return r.FatalWith("failed to load", nil) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "failed to load",
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     0,
		},
		{
			name:                       "Fatal with no errors MUST log and respond",
			testFunc:                   func(r *Renderer) error { return r.Fatal() },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    defaultFatalMessage,
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     0,
		},
		{
			name:                       "FatalWith with no errors MUST log and respond",
			testFunc:                   func(r *Renderer) error { return r.FatalWith("failed to load") },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "failed to load",
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     0,
		},
		{
			name:                       "Fatalf with no error arguments MUST log and respond",
			testFunc:                   func(r *Renderer) error { return r.Fatalf("hello") },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "hello",
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     0,
		},
		{
			name:                       "Fatalf with nil error MUST log and respond",
			testFunc:                   func(r *Renderer) error { return r.Fatalf("hello : %v", nil) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "hello : <nil>",
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     0,
		},
		{
			name:                   "Fatal with only ErrSkip should not log or respond",
			testFunc:               func(r *Renderer) error { return r.Fatal(ErrSkip) },
			expectResponse:         false,
			expectLog:              false,
			expectedLogFieldsCount: 0,
		},
		{
			name:                   "FatalWith with only ErrSkip should not log or respond",
			testFunc:               func(r *Renderer) error { return r.FatalWith("this should be skipped", ErrSkip) },
			expectResponse:         false,
			expectLog:              false,
			expectedLogFieldsCount: 0,
		},
		{
			name:                       "Fatal with only ErrHidden MUST log and respond",
			testFunc:                   func(r *Renderer) error { return r.Fatal(ErrHidden) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    defaultFatalMessage,
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     0,
		},
		{
			name:                       "FatalWith with only ErrHidden MUST log and respond",
			testFunc:                   func(r *Renderer) error { return r.FatalWith("a hidden error occurred", ErrHidden) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "a hidden error occurred",
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     0,
		},
		{
			name:                       "Fatalf with only ErrHidden MUST log and respond",
			testFunc:                   func(r *Renderer) error { return r.Fatalf("hidden : %v", ErrHidden) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "hidden : *hidden*",
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     0,
		},
		{
			name:                   "Fatalf with ErrSkip should not log or respond",
			testFunc:               func(r *Renderer) error { return r.Fatalf("skipped : %v", ErrSkip) },
			expectResponse:         false,
			expectLog:              false,
			expectedLogFieldsCount: 0,
		},
		{
			name: "Fatalf with mixed errors including ErrSkip",
			testFunc: func(r *Renderer) error {
				return r.Fatalf("mixed: %v %v %v", errors.New("real"), ErrSkip, errors.New("another"))
			},
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "mixed: real %!v(MISSING) another",
			expectedResponseErrorCount: 2,
			expectedLogFieldsCount:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testLogger := &TestLogger{}
			renderer := NewRenderer(Setting{}).WithWriter(w).WithLogger(testLogger)

			err := tt.testFunc(renderer)
			if err != nil {
				t.Fatalf("Function returned an unexpected error: %v", err)
			}

			if !tt.expectResponse {
				if w.Body.Len() != 0 {
					t.Errorf("expected NO response, but got body: %s", w.Body.String())
				}
			} else {
				if w.Body.Len() == 0 {
					t.Fatal("expected a response, but NO response was sent")
				}
				if w.Code != http.StatusInternalServerError {
					t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
				}
				var resp Response
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if resp.Message != tt.expectedResponseMessage {
					t.Errorf("expected message %q, got %q", tt.expectedResponseMessage, resp.Message)
				}
				if len(resp.Errors) != tt.expectedResponseErrorCount {
					t.Errorf("expected %d errors in response, got %d", tt.expectedResponseErrorCount, len(resp.Errors))
				}
			}

			if !tt.expectLog {
				if len(testLogger.Entries) != 0 {
					t.Errorf("expected NO log entry, but %d were found", len(testLogger.Entries))
				}
			} else {
				if len(testLogger.Entries) == 0 {
					t.Fatal("expected a log entry, but NO log was created")
				}
				lastLog := testLogger.LastEntry()
				if lastLog.Level != "fatal" {
					t.Errorf("expected log level 'fatal', got %q", lastLog.Level)
				}
				if lastLog.Err == nil {
					t.Error("expected a non-nil error in the log entry")
				}
				if len(lastLog.Fields) != tt.expectedLogFieldsCount {
					t.Errorf("expected %d log fields, got %d", tt.expectedLogFieldsCount, len(lastLog.Fields))
				}
			}
		})
	}
}

// TestFatalMethods_WithFilterableErrors specifically tests that fatal methods
// ALWAYS produce a response and that the response body contains the original
// filterable error for client context.
func TestFatalMethods_WithFilterableErrors(t *testing.T) {
	// sql.ErrNoRows is a perfect stand-in for any custom, filterable error.
	filterableErr := sql.ErrNoRows

	tests := []struct {
		name     string
		testFunc func(r *Renderer) error
	}{
		{
			name:     "Fatal with a filterable error",
			testFunc: func(r *Renderer) error { return r.Fatal(filterableErr) },
		},
		{
			name:     "FatalWith with a filterable error",
			testFunc: func(r *Renderer) error { return r.FatalWith("failed to query", filterableErr) },
		},
		{
			name:     "Fatalf with a filterable error",
			testFunc: func(r *Renderer) error { return r.Fatalf("query failed: %v", filterableErr) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			w := httptest.NewRecorder()
			testLogger := &TestLogger{}
			renderer := NewRenderer(Setting{}).WithWriter(w).WithLogger(testLogger)

			// Act
			err := tt.testFunc(renderer)
			if err != nil {
				t.Fatalf("Function returned an unexpected error: %v", err)
			}

			// Assert Response: A response MUST be sent.
			if w.Body.Len() == 0 {
				t.Fatal("expected a fatal response, but NO response was sent")
			}
			if w.Code != http.StatusInternalServerError {
				t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
			}
			var resp Response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			// The original error MUST be in the response body.
			if len(resp.Errors) != 1 {
				t.Fatalf("expected 1 error in response body, got %d", len(resp.Errors))
			}
			if resp.Errors[0].Error() != filterableErr.Error() {
				t.Errorf("expected error %q in response, got %q", filterableErr.Error(), resp.Errors[0].Error())
			}

			// Assert Logging: The logger MUST be called.
			if len(testLogger.Entries) == 0 {
				t.Fatal("expected a log entry, but NO log was created")
			}
			lastLog := testLogger.LastEntry()
			if lastLog.Level != "fatal" {
				t.Errorf("expected log level 'fatal', got %q", lastLog.Level)
			}
			// The log entry's primary error should be a generic message since the real error was filtered
			// from the logging perspective.
			if !strings.Contains(lastLog.Err.Error(), "fatal error occurred") && !strings.Contains(lastLog.Err.Error(), "failed to query") && !strings.Contains(lastLog.Err.Error(), "query failed") {
				t.Errorf("expected log message to contain the fatal message, got %q", lastLog.Err.Error())
			}
		})
	}
}
