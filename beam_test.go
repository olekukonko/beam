package beam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestNewRenderer(t *testing.T) {
	t.Run("DefaultSettings", func(t *testing.T) {
		r := New(Setting{Name: "test"})
		if r.s.Name != "test" {
			t.Errorf("Expected name 'test', got '%s'", r.s.Name)
		}
		if r.format != FormatJSON {
			t.Errorf("Expected default format JSON, got %v", r.format)
		}
		if !r.s.EnableHeaders {
			t.Error("Expected headers enabled by default")
		}
	})

	t.Run("CustomFormat", func(t *testing.T) {
		r := New(Setting{Format: FormatXML})
		if r.format != FormatXML {
			t.Errorf("Expected format XML, got %v", r.format)
		}
	})
}

func TestRenderer_WithMethods(t *testing.T) {
	base := New(Setting{Name: "test"})

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

	t.Run("WithFormat", func(t *testing.T) {
		r := base.WithFormat(FormatMsgPack)
		if r.format != FormatMsgPack {
			t.Error("WithFormat did not set the format")
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
		r := New(Setting{Name: "test"}).WithWriter(tw)
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
		if contentType != "application/json" {
			t.Errorf("Expected content type application/json, got %s", contentType)
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
		r := New(Setting{Name: "test"}).WithWriter(tw)
		resp := Response{Status: StatusSuccessful}

		err := r.Push(tw, resp)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Errorf("Expected write error, got %v", err)
		}
	})

	t.Run("WithSystemInfo", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		sys := System{App: "test-app", Show: SystemShowBody}
		r := New(Setting{Name: "test"}).WithWriter(tw).WithSystem(SystemShowBody, sys)
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
		r := New(Setting{Name: "test"}).WithWriter(tw)

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
		r := New(Setting{Name: "test"}) // No writer set

		err := r.Raw("test")
		if err == nil || !strings.Contains(err.Error(), "no writer set") {
			t.Errorf("Expected no writer error, got %v", err)
		}
	})
}

func TestRenderer_Binary(t *testing.T) {
	t.Run("SuccessfulBinary", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := New(Setting{Name: "test"}).WithWriter(tw)
		data := []byte{1, 2, 3, 4}

		err := r.Binary("application/octet-stream", data)
		if err != nil {
			t.Fatalf("Binary failed: %v", err)
		}

		if !bytes.Equal(tw.Buffer.Bytes(), data) {
			t.Error("Binary data not written correctly")
		}

		contentType := tw.Headers.Get("Content-Type")
		if contentType != "application/octet-stream" {
			t.Errorf("Expected content type application/octet-stream, got %s", contentType)
		}
	})
}

func TestRenderer_Image(t *testing.T) {
	t.Run("SuccessfulPNG", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := New(Setting{Name: "test"}).WithWriter(tw)

		// Create a simple 1x1 image
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.Set(0, 0, color.RGBA{255, 0, 0, 255})

		err := r.Image(ImageTypePNG, img)
		if err != nil {
			t.Fatalf("Image failed: %v", err)
		}

		if tw.Buffer.Len() == 0 {
			t.Error("No image data written")
		}

		contentType := tw.Headers.Get("Content-Type")
		if contentType != ImageTypePNG {
			t.Errorf("Expected content type %s, got %s", ImageTypePNG, contentType)
		}
	})

	t.Run("UnsupportedFormat", func(t *testing.T) {
		tw := &TestWriter{Headers: make(http.Header)}
		r := New(Setting{Name: "test"}).WithWriter(tw)

		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		err := r.Image("unsupported/format", img)
		if err == nil || !strings.Contains(err.Error(), "unsupported image content type") {
			t.Errorf("Expected unsupported format error, got %v", err)
		}
	})
}

func TestRenderer_ConvenienceMethods(t *testing.T) {
	tw := &TestWriter{Headers: make(http.Header)}
	r := New(Setting{Name: "test"}).WithWriter(tw)

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
		r := New(Setting{Name: "test"})
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
		r := New(Setting{Name: "test"}).SetLogger(testLogger)
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
		r := New(Setting{Name: "test"}).WithWriter(tw)

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
		r := New(Setting{Name: "test"}).
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
		r := New(Setting{Name: "test"}).
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

func TestEncoderRegistry(t *testing.T) {
	t.Run("DefaultEncoders", func(t *testing.T) {
		er := NewEncoderRegistry()

		testCases := []struct {
			format Format
			data   interface{}
		}{
			{FormatJSON, map[string]string{"key": "value"}},
			{FormatMsgPack, map[string]string{"key": "value"}},
			{FormatXML, struct {
				XMLName struct{} `xml:"test"`
				Key     string   `xml:"key"`
			}{Key: "value"}},
			{FormatText, "test"},
		}

		for _, tc := range testCases {
			_, err := er.Encode(tc.format, tc.data)
			if err != nil {
				t.Errorf("%d encoding failed: %v", tc.format, err)
			}
		}
	})

	t.Run("CustomEncoder", func(t *testing.T) {
		er := NewEncoderRegistry()

		// Create a tracking encoder
		trackingEncoder := &trackingEncoder{
			Encoder: &JSONEncoder{},
			called:  false,
		}

		er.Register(FormatJSON, trackingEncoder)

		_, err := er.Encode(FormatJSON, map[string]string{"key": "value"})
		if err != nil {
			t.Errorf("Custom encoder failed: %v", err)
		}

		if !trackingEncoder.called {
			t.Error("Custom encoder was not called")
		}
	})

	t.Run("FallbackEncoder", func(t *testing.T) {
		er := NewEncoderRegistry().Fallback(&JSONEncoder{})
		_, err := er.Encode(FormatUnknown, map[string]string{"key": "value"})
		if err != nil {
			t.Errorf("Fallback encoding failed: %v", err)
		}
	})
}

func TestCallbackManager(t *testing.T) {
	t.Run("TriggerCallbacks", func(t *testing.T) {
		cm := NewCallbackManager()
		called := false
		cb := func(data CallbackData) {
			called = true
			if data.ID != "test-id" || data.Status != StatusError {
				t.Errorf("Unexpected callback data: %+v", data)
			}
		}

		cm.AddCallback(cb)
		cm.Trigger("test-id", StatusError, "test message", nil)

		if !called {
			t.Error("Callback was not called")
		}
	})

	t.Run("Clone", func(t *testing.T) {
		cm := NewCallbackManager()
		cm.AddCallback(func(data CallbackData) {})

		clone := cm.Clone()
		if len(clone.callbacks) != 1 {
			t.Error("Clone did not copy callbacks")
		}

		cm.AddCallback(func(data CallbackData) {})
		if len(clone.callbacks) != 1 {
			t.Error("Clone is not independent")
		}
	})
}
