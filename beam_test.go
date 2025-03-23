// beam_test.go
package beam

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestResponse is a helper struct used for unmarshalling JSON/XML responses in tests.
// It mirrors the Response structure but uses []string for Errors.
type TestResponse struct {
	Status  string                 `json:"status" xml:"status"`
	Title   string                 `json:"title,omitempty" xml:"title,omitempty"`
	Message string                 `json:"message,omitempty" xml:"message,omitempty"`
	Tags    []string               `json:"tags,omitempty" xml:"tags,omitempty"`
	Info    interface{}            `json:"info,omitempty" xml:"info,omitempty"`
	Data    []interface{}          `json:"data,omitempty" xml:"data,omitempty"`
	Meta    map[string]interface{} `json:"meta,omitempty" xml:"meta,omitempty"`
	Errors  []string               `json:"error,omitempty" xml:"error,omitempty"`
}

// simpleLogger implements the Logger interface.
type simpleLogger struct{}

func (l *simpleLogger) Log(err error) bool {
	println("LOG:", err.Error())
	return true
}

// setupRouter creates a test router with our endpoints.
func setupRouter() http.Handler {
	renderer := New(Setting{
		Name:          "beam",
		Format:        FormatJSON,
		EnableHeaders: true,
	}).
		SetLogger(&simpleLogger{}).
		WithCallback(func(data CallbackData) {
			// Callback is not used during tests.
		}).
		WithSystem(SystemShowBoth, System{
			App:     "MyApp",
			Server:  "localhost",
			Version: "1.0.0",
			Build:   "20250323",
			Play:    true,
		})

	r := chi.NewRouter()

	// /hello endpoint returns a JSON success message.
	r.Get("/hello", renderer.Handler(func(r *Renderer) error {
		return r.Info("Hello, world!", map[string]string{"greeting": "Hello from Beam with Chi!"})
	}))

	// /error endpoint returns a JSON error response.
	r.Get("/error", renderer.Handler(func(r *Renderer) error {
		return r.Error("error %s", errors.New("something happened"))
	}))

	// /xml endpoint returns an XML error response.
	// We disable system meta info (which is a map and not XML-marshallable) for this route.
	r.Get("/xml", renderer.WithSystem(SystemShowNone, System{}).Handler(func(r *Renderer) error {
		return r.WithFormat(FormatXML).Error("error %s", errors.New("something happened"))
	}))

	// /fatal endpoint returns a fatal error response in JSON.
	r.Get("/fatal", renderer.Handler(func(r *Renderer) error {
		return r.Fatal(errors.New("danger danger"))
	}))

	// /image endpoint returns a PNG image.
	r.Get("/image", renderer.Handler(func(r *Renderer) error {
		return r.WithHeader("name", "sample image").Image(ImageTypePNG, dummyImage(300, 300))
	}))

	return r
}

func TestHelloEndpoint(t *testing.T) {
	router := setupRouter()
	req, _ := http.NewRequest("GET", "/hello", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Check HTTP status.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, status)
	}

	// Check Content-Type header.
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Expected Content-Type to contain 'application/json', got %s", ct)
	}

	// Decode JSON response into TestResponse.
	var resp TestResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("Error unmarshalling JSON: %v", err)
	}

	// Validate response message.
	if resp.Message != "Hello, world!" {
		t.Errorf("Expected message 'Hello, world!', got '%s'", resp.Message)
	}

	// Check info field.
	infoMap, ok := resp.Info.(map[string]interface{})
	if !ok {
		t.Errorf("Expected info to be a map, got %T", resp.Info)
	} else {
		if greeting, exists := infoMap["greeting"]; !exists || greeting != "Hello from Beam with Chi!" {
			t.Errorf("Expected greeting 'Hello from Beam with Chi!', got %v", infoMap["greeting"])
		}
	}

	// Check that system meta info was added.
	if resp.Meta == nil || resp.Meta["system"] == nil {
		t.Errorf("Expected meta to contain system info, got %v", resp.Meta)
	}
}

func TestErrorEndpoint(t *testing.T) {
	router := setupRouter()
	req, _ := http.NewRequest("GET", "/error", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, status)
	}

	var resp TestResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("Error unmarshalling JSON: %v", err)
	}

	if resp.Status != StatusError {
		t.Errorf("Expected status %s, got %s", StatusError, resp.Status)
	}
	if !strings.Contains(resp.Message, "something happened") {
		t.Errorf("Expected error message to contain 'something happened', got '%s'", resp.Message)
	}
}

func TestXMLEndpoint(t *testing.T) {
	router := setupRouter()
	req, _ := http.NewRequest("GET", "/xml", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, status)
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/xml") {
		t.Errorf("Expected Content-Type to contain 'application/xml', got %s", ct)
	}

	var resp TestResponse
	if err := xml.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("Error unmarshalling XML: %v", err)
	}

	if resp.Status != StatusError {
		t.Errorf("Expected status %s, got %s", StatusError, resp.Status)
	}
	if !strings.Contains(resp.Message, "something happened") {
		t.Errorf("Expected error message to contain 'something happened', got '%s'", resp.Message)
	}
}

func TestFatalEndpoint(t *testing.T) {
	router := setupRouter()
	req, _ := http.NewRequest("GET", "/fatal", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("Expected status code %d, got %d", http.StatusInternalServerError, status)
	}

	var resp TestResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("Error unmarshalling JSON: %v", err)
	}

	if resp.Status != StatusFatal {
		t.Errorf("Expected status %s, got %s", StatusFatal, resp.Status)
	}
	if !strings.Contains(resp.Message, "danger danger") {
		t.Errorf("Expected fatal message to contain 'danger danger', got '%s'", resp.Message)
	}
}

func TestImageEndpoint(t *testing.T) {
	router := setupRouter()
	req, _ := http.NewRequest("GET", "/image", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, status)
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "image/png") {
		t.Errorf("Expected Content-Type to contain 'image/png', got %s", ct)
	}

	// Validate that the response body contains a decodable PNG image.
	_, err := png.Decode(bytes.NewReader(rr.Body.Bytes()))
	if err != nil {
		t.Errorf("Error decoding PNG image: %v", err)
	}
}

// dummyImage creates a simple solid blue image for testing.
func dummyImage(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	blue := color.RGBA{0, 0, 255, 255}
	// Fill image by setting each pixel.
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, blue)
		}
	}
	return img
}
