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
type Writer interface {
	Write(data []byte) (int, error)
}

// Logger is an interface for logging errors.
type Logger interface {
	Log(err error) bool // Returns true if logged successfully
}

// Finalizer defines a function to handle errors after rendering.
type Finalizer func(w Writer, err error)

// ErrSkip is a predefined error for skipping.
var ErrSkip = errors.New("skip")

// ErrContextCanceled is a predefined error for context cancellation.
var ErrContextCanceled = errors.New("context canceled")

// System holds system metadata and display preferences.
type System struct {
	App      string        `json:"app" xml:"App"`
	Server   string        `json:"server,omitempty" xml:"Server,omitempty"`
	Version  string        `json:"version,omitempty" xml:"Version,omitempty"`
	Build    string        `json:"build,omitempty" xml:"Build,omitempty"`
	Play     bool          `json:"play,omitempty" xml:"Play,omitempty"`
	Show     SystemShow    `json:"-" xml:"-"`
	Duration time.Duration `json:"duration" xml:"Duration"`
}

// MarshalJSON provides a custom JSON encoding for System.
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
type Setting struct {
	Name          string
	ContentType   string
	EnableHeaders bool              // Enable sending headers (default true)
	Presets       map[string]Preset // Custom presets for content types
}

// Preset defines a preset for custom content types.
type Preset struct {
	ContentType string
	Headers     http.Header
}

// -----------------------------------------------------------------------------
// Callback Types
// -----------------------------------------------------------------------------

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

// Response is the standard response structure.
type Response struct {
	Status  string                 `json:"status" xml:"status" msgpack:"status"`
	Title   string                 `json:"title,omitempty" xml:"title,omitempty" msgpack:"title"`
	Message string                 `json:"message,omitempty" xml:"message,omitempty" msgpack:"message"`
	Tags    []string               `json:"tags,omitempty" xml:"tags,omitempty" msgpack:"tags"`
	Info    interface{}            `json:"info,omitempty" xml:"info,omitempty" msgpack:"info"`
	Data    []interface{}          `json:"data,omitempty" xml:"data,omitempty" msgpack:"data"`
	Meta    map[string]interface{} `json:"meta,omitempty" xml:"meta,omitempty" msgpack:"meta"`
	Errors  ErrorList              `json:"errors,omitempty" xml:"errors,omitempty" msgpack:"errors"`
}

// ErrorList is a custom type for a list of errors that implements JSON marshalling.
type ErrorList []error

// MarshalJSON implements custom JSON marshaling for ErrorList.
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
