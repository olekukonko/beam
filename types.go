package beam

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"time"
)

// -----------------------------------------------------------------------------
// Core Interfaces
// -----------------------------------------------------------------------------

// Writer defines the interface for output destinations.
// Provides a method to write byte data to an output.
// Used by Renderer to send responses to clients.
type Writer interface {
	Write(data []byte) (int, error)
}

// Logger is an interface for logging errors.
// Defines a method to log an error and indicate success.
// Used by Renderer to log errors during response handling.
type Logger interface {
	Log(err error) bool // Returns true if logged successfully
}

// Finalizer defines a function to handle errors after rendering.
// Takes a Writer and an error to process.
// Used by Renderer to finalize error responses.
type Finalizer func(w Writer, err error)

// ErrContextCanceled is a predefined error for context cancellation.
// Signals that a context was canceled during operation.
// Used by Renderer to handle canceled requests.
var ErrContextCanceled = errors.New("context canceled")

// System holds system metadata and display preferences.
// Stores metadata like app name, version, and duration.
// Used to include system information in responses.
type System struct {
	App      string        `json:"app" xml:"App"`
	Server   string        `json:"server,omitempty" xml:"Server,omitempty"`
	Version  string        `json:"version,omitempty" xml:"Version,omitempty"`
	Build    string        `json:"build,omitempty" xml:"Build,omitempty"`
	Play     bool          `json:"play,omitempty" xml:"Play,omitempty"`
	Duration time.Duration `json:"duration" xml:"Duration"`

	show SystemShow `json:"-" xml:"-"`
}

// MarshalJSON provides a custom JSON encoding for System.
// Encodes the System struct with duration as a string.
// Returns the JSON-encoded bytes or an error if encoding fails.
func (s System) MarshalJSON() ([]byte, error) {
	type Alias System // Prevent recursion
	return json.Marshal(&struct {
		Duration string `json:"duration"`
		*Alias
	}{
		Duration: s.Duration.String(),
		Alias:    (*Alias)(&s),
	})
}

// MarshalXML provides a custom XML encoding for System.
// Encodes the System struct with duration as a string.
// Returns an error if XML encoding fails.
func (s System) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	type Alias System
	aux := &struct {
		Duration string `xml:"Duration"`
		*Alias
	}{
		Duration: s.Duration.String(),
		Alias:    (*Alias)(&s),
	}
	return e.EncodeElement(aux, start)
}

// Setting configures the renderer.
// Holds configuration like content type and header settings.
// Used to initialize Renderer with specific options.
type Setting struct {
	Name          string
	ContentType   string
	EnableHeaders bool              // Enable sending headers (default true)
	Presets       map[string]Preset // Custom presets for content types
}

// Preset defines a preset for custom content types.
// Specifies content type and associated headers.
// Used in Setting to customize response headers.
type Preset struct {
	ContentType string
	Headers     http.Header
}

// -----------------------------------------------------------------------------
// Callback Types
// -----------------------------------------------------------------------------

// CallbackData carries information to callback functions.
// Holds response metadata like ID, status, and errors.
// Used by CallbackManager to pass data to callbacks.
type CallbackData struct {
	ID      string   `json:"id"`
	Status  string   `json:"status"` // Uses Status* constants
	Title   string   `json:"title,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Message string   `json:"message,omitempty"`
	Output  string   `json:"output,omitempty"`
	Err     error    `json:"-"` // Not marshaled, for internal use
}

// IsError checks if the callback data represents an error state.
// Returns true if the status is StatusError or StatusFatal.
// Used to determine if a callback indicates an error.
func (c CallbackData) IsError() bool {
	return c.Status == StatusError || c.Status == StatusFatal
}

// Error returns the error associated with the callback data.
// Returns the Err field of the CallbackData struct.
// Implements the error interface for CallbackData.
func (c CallbackData) Error() error {
	return c.Err
}

// Response is the standard response structure.
// Contains fields for status, message, data, and errors.
// Used by Renderer to structure response output.
type Response struct {
	Status  string                 `json:"status" xml:"status" msgpack:"status"`
	Title   string                 `json:"title,omitempty" xml:"title,omitempty" msgpack:"title"`
	Message string                 `json:"message,omitempty" xml:"message,omitempty" msgpack:"message"`
	Tags    []string               `json:"tags,omitempty" xml:"tags,omitempty" msgpack:"tags"`
	Info    interface{}            `json:"info,omitempty" xml:"info,omitempty" msgpack:"info"`
	Data    interface{}            `json:"data,omitempty" xml:"data,omitempty" msgpack:"data"`
	Meta    map[string]interface{} `json:"meta,omitempty" xml:"meta,omitempty" msgpack:"meta"`
	Errors  ErrorList              `json:"errors,omitempty" xml:"errors,omitempty" msgpack:"errors"`
	Actions []Action               `json:"actions,omitempty" xml:"actions,omitempty" msgpack:"actions"`
}

// Action represents a possible next step the client can take
type Action struct {
	Name        string                 `json:"name"`                  // Unique identifier for the action
	Description string                 `json:"description,omitempty"` // Human-readable description
	Method      string                 `json:"method,omitempty"`      // HTTP method (GET, POST, etc)
	Href        string                 `json:"href,omitempty"`        // URL or URI template
	Parameters  map[string]interface{} `json:"parameters,omitempty"`  // Required parameters
	Headers     map[string]string      `json:"headers,omitempty"`     // Required headers
}

// ErrorList is a custom type for a list of errors that implements JSON marshalling.
// Represents a slice of errors for response serialization.
// Used in Response to include multiple errors.
type ErrorList []error

// MarshalJSON implements custom JSON marshaling for ErrorList.
// Converts each error to its string representation.
// Returns JSON-encoded error strings or an error if marshaling fails.
func (el ErrorList) MarshalJSON() ([]byte, error) {
	errStrings := make([]string, len(el))
	for i, err := range el {
		if err != nil {
			errStrings[i] = err.Error()
		}
	}
	return json.Marshal(errStrings)
}

// UnmarshalJSON implements custom JSON unmarshaling for ErrorList.
// Converts JSON string array to a slice of errors.
// Returns an error if unmarshaling fails.
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
// Callback Management
// -----------------------------------------------------------------------------

// CallbackManager handles callback registration and triggering.
// Manages a slice of callback functions for response events.
// Used by Renderer to notify callbacks of response status.
type CallbackManager struct {
	callbacks []func(data CallbackData)
}

// NewCallbackManager creates a new CallbackManager.
// Initializes an empty CallbackManager for callback registration.
// Returns a *CallbackManager ready for use.
func NewCallbackManager() *CallbackManager {
	return &CallbackManager{}
}

// Clone creates a copy of the CallbackManager.
// Duplicates the callbacks slice for thread-safe operations.
// Returns a new *CallbackManager with copied callbacks.
func (cm *CallbackManager) Clone() *CallbackManager {
	newCM := &CallbackManager{
		callbacks: append([]func(data CallbackData){}, cm.callbacks...),
	}
	return newCM
}

// AddCallback registers one or more callbacks.
// Takes callback functions that accept CallbackData.
// Appends callbacks to the manager and returns it for chaining.
func (cm *CallbackManager) AddCallback(cb ...func(data CallbackData)) *CallbackManager {
	cm.callbacks = append(cm.callbacks, cb...)
	return cm
}

// Trigger calls all registered callbacks with the provided data.
// Takes ID, status, message, and optional error for callbacks.
// Executes each callback with constructed CallbackData.
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
