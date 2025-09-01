package beam

import (
	"errors"
	"sync"
)

// Empty is a constant representing an empty string.
// It serves as a default or placeholder for uninitialized or unset fields in Beam components.
const Empty = ""

// EmptyStruct is a zero-memory struct used as a placeholder.
// It represents an empty or null value in response fields like Info or Data.
type EmptyStruct struct{}

// Status constants define standardized response states for Renderer responses.
// They are used in the Response struct to indicate the outcome of an operation.
const (
	StatusError      = "-error"   // Indicates a non-fatal error
	StatusPending    = "?pending" // Indicates an operation is in progress
	StatusSuccessful = "+ok"      // Indicates a successful operation
	StatusFatal      = "*fatal"   // Indicates a critical error
	StatusWarning    = "*warning" // Indicates a non-critical warning
	StatusUnknown    = "*unknown" // Indicates an undefined or unknown state
)

// Header constants define standard HTTP header names and prefixes for metadata.
// They are used by Renderer to set response headers like Content-Type and Duration.
const (
	HeaderPrefix      = "X-Beam"       // Prefix for custom Beam headers
	HeaderContentType = "Content-Type" // Standard HTTP Content-Type header

	HeaderNameDuration  = "Duration"  // Duration of the operation
	HeaderNameTimestamp = "Timestamp" // Timestamp of the response
	HeaderNameApp       = "App"       // Application name
	HeaderNameServer    = "Server"    // Server identifier
	HeaderNameVersion   = "Version"   // Application version
	HeaderNameBuild     = "Build"     // Build identifier
	HeaderNamePlay      = "Play"      // Play mode or context
)

// Operation status constants indicate the success or failure of operations.
// They are used to represent the outcome of internal operations.
const (
	Unknown = 0  // Default state for an operation
	No      = -1 // Operation failed
	Yes     = 1  // Operation succeeded
)

// SystemShow defines modes for displaying system metadata in responses.
// It controls whether metadata appears in headers, body, both, or neither.
type SystemShow int

// SystemShow constants specify metadata display modes for Renderer configuration.
const (
	SystemShowNone    SystemShow = iota // No metadata in headers or body
	SystemShowHeaders                   // Metadata in headers only
	SystemShowBody                      // Metadata in response body only
	SystemShowBoth                      // Metadata in both headers and body
)

// Default error messages for Renderer responses.
// They provide consistent messaging when no custom message is specified.
const (
	defaultErrorMessage = "an error occurred"      // Default for non-fatal errors
	defaultFatalMessage = "a fatal error occurred" // Default for fatal errors

	// Logging field keys for structured error logging
	fieldMessage = "message" // Message associated with the log
	fieldID      = "id"      // Identifier for the request or operation
	fieldTags    = "tags"    // Tags for categorizing logs
	fieldSource  = "source"  // Source of the log or error
	fieldFile    = "file"    // File name for error context
	fieldLine    = "line"    // Line number for error context
	fieldFunc    = "func"    // Function name for error context
	fieldError   = "error"   // Primary error message
	fieldErrors  = "errors"  // Additional error details
	fieldMeta    = "meta"    // Metadata for logging
)

// Common errors for protocol handling.
// These reusable errors ensure consistent error reporting in ProtocolHandler and HTTPProtocol.
var (
	errHTTPWriterRequired = errors.New("HTTPProtocol requires an http.ResponseWriter")
)

// Predefined errors for common failure cases in Beam.
// These reusable error instances reduce fmt.Errorf allocations and ensure consistency.
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

// Predefined errors for special handling in Renderer.
// They control response behavior by suppressing or bypassing errors.
var (
	ErrHidden = errors.New("hidden") // Suppresses error details in responses
	ErrSkip   = errors.New("skip")   // Bypasses operations without failure
)

// responsePool manages a sync.Pool for reusing Response objects.
// It reduces memory allocations by recycling Response structs with an initialized Meta map.
var responsePool = sync.Pool{
	New: func() interface{} {
		return &Response{
			Meta: make(map[string]interface{}),
		}
	},
}

// frameworkPatterns lists patterns for filtering framework-related errors.
// Used to identify and exclude errors from common Go frameworks or libraries in logging or responses.
var frameworkPatterns = []string{
	"/beam/", "beam.", // Beam framework
	"/net/http/", "net/http.", // Standard HTTP package
	"/runtime/", "runtime.", // Go runtime
	"/testing/", "testing.", // Go testing package
	"/mux", "mux.", // Router frameworks (e.g., Gorilla mux, chi)
	"/chi/", "chi.", // Chi router
	"/echo/", "echo.", // Echo framework
	"/gin/", "gin.", // Gin framework
	"/handler", "handler.", // Generic handler patterns
	"/controller", "controller.", // Controller patterns
	"/middleware", "middleware.", // Middleware patterns
	"/vendor/", // Vendor dependencies
}

// ErrorFilterSet holds functions to filter, redact, or convert errors before inclusion in responses.
type ErrorFilterSet struct {
	Skip    []func(error) bool  // Determines errors to omit from non-fatal responses
	Redact  []func(error) bool  // Determines errors to mask in responses
	Convert []func(error) error // Transforms errors, e.g., to change severity
}

// isSkipped checks if an error should be omitted based on Skip filters.
// Returns true if any Skip function matches the error.
func (fs *ErrorFilterSet) isSkipped(err error) bool {
	for _, f := range fs.Skip {
		if f(err) {
			return true
		}
	}
	return false
}

// isRedacted checks if an error should be masked based on Redact filters.
// Returns true if any Redact function matches the error.
func (fs *ErrorFilterSet) isRedacted(err error) bool {
	for _, f := range fs.Redact {
		if f(err) {
			return true
		}
	}
	return false
}

// applyConverters applies all Convert functions to an error in sequence.
// Returns the transformed error after all conversions.
func (fs *ErrorFilterSet) applyConverters(err error) error {
	for _, f := range fs.Convert {
		err = f(err)
	}
	return err
}

// clone creates a deep copy of the ErrorFilterSet.
// Returns a new ErrorFilterSet with copied function slices to prevent shared state.
func (fs *ErrorFilterSet) clone() ErrorFilterSet {
	return ErrorFilterSet{
		Skip:    append([]func(error) bool{}, fs.Skip...),
		Redact:  append([]func(error) bool{}, fs.Redact...),
		Convert: append([]func(error) error{}, fs.Convert...),
	}
}

// getResponse retrieves a Response object from the responsePool.
// Returns a Response with an initialized Meta map for reuse.
// Callers must call putResponse to return the object to the pool.
func getResponse() *Response {
	return responsePool.Get().(*Response)
}

// putResponse returns a Response to the responsePool after resetting it.
// Clears all fields to prevent data leakage between uses.
func putResponse(r *Response) {
	r.Status = ""
	r.Title = ""
	r.Message = ""
	r.Info = EmptyStruct{}
	r.Data = make([]any, 0)
	for k := range r.Meta {
		delete(r.Meta, k)
	}
	r.Tags = r.Tags[:0]
	r.Errors = r.Errors[:0]
	responsePool.Put(r)
}

// streamBufferPool manages a sync.Pool for reusing byte slices in streaming operations.
// It provides buffers with an initial 4KB capacity to reduce memory allocations.
var streamBufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, 4096) // Initial capacity of 4KB
	},
}

// fatalError wraps an error to mark it for fatal handling.
// It implements the error interface and supports unwrapping.
type fatalError struct{ error }

// Unwrap returns the underlying error.
func (e fatalError) Unwrap() error { return e.error }

// normalError wraps an error to mark it for non-fatal handling.
// It implements the error interface and supports unwrapping.
type normalError struct{ error }

// Unwrap returns the underlying error.
func (e normalError) Unwrap() error { return e.error }

// ToFatal wraps an error to mark it as fatal.
// Returns a fatalError wrapping the provided error.
func ToFatal(err error) error {
	return fatalError{err}
}

// ToNormal wraps an error to mark it as non-fatal.
// Returns a normalError wrapping the provided error.
func ToNormal(err error) error {
	return normalError{err}
}

// maskedError wraps sensitive errors to provide a redacted error message.
// It implements the error interface to mask details in responses.
type maskedError struct {
	original error
}

// Error returns a redacted version of the original error message.
// Shows up to 4 characters of the original message (or fewer for short messages) followed by "[REDACTED]".
func (m maskedError) Error() string {
	originalMsg := m.original.Error()
	if len(originalMsg) == 0 {
		return "[REDACTED]"
	}
	visibleLen := 4
	if len(originalMsg) < visibleLen {
		visibleLen = len(originalMsg)
	}
	if visibleLen == 0 {
		visibleLen = 1 // Ensure at least one character for non-empty strings
	}
	return originalMsg[:visibleLen] + " [REDACTED]"
}
