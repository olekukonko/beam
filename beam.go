package beam

import (
	"errors"
	"sync"
)

// Empty is a constant representing an empty string.
// Used as a default or placeholder value in various Beam components.
// Simplifies checks for uninitialized or unset fields.
const (
	Empty = ""
)

// Status constants for response states.
// Define standardized response statuses for Renderer responses.
// Used in Response struct to indicate success, error, or other states.
const (
	StatusError      = "-error"
	StatusPending    = "?pending"
	StatusSuccessful = "+ok"
	StatusFatal      = "*fatal"
	StatusWarning    = "*warning"
)

// Header information constants for HTTP headers.
// Define standard header names and prefixes for metadata output.
// Used by Renderer to set response headers like Content-Type and Duration.
const (
	HeaderPrefix      = "X-Beam"
	HeaderContentType = "Content-Type"

	HeaderNameDuration  = "Duration"
	HeaderNameTimestamp = "Timestamp"
	HeaderNameApp       = "App"
	HeaderNameServer    = "Server"
	HeaderNameVersion   = "Version"
	HeaderNameBuild     = "Build"
	HeaderNamePlay      = "Play"
)

// -----------------------------------------------------------------------------
// System Metadata and Renderer Settings
// -----------------------------------------------------------------------------

// SystemShow controls where system metadata is displayed in responses.
// Defines modes for including metadata in headers, body, both, or none.
// Used by Renderer to configure System struct output.
type SystemShow int

// SystemShow constants for metadata display modes.
// Specify whether system metadata appears in headers, body, or both.
// Used with SystemShow type in Renderer configuration.
const (
	SystemShowNone SystemShow = iota
	SystemShowHeaders
	SystemShowBody
	SystemShowBoth
)

// defaultErrorMessage is the default message for error responses.
// Used when no custom error message is provided in Renderer methods.
// Ensures consistent error messaging across responses.
const (
	defaultErrorMessage = "An error occurred"
)

// Common errors for protocol handling.
// Define reusable errors for protocol-related operations.
// Used in ProtocolHandler and HTTPProtocol for consistent error reporting.
var (
	errHTTPWriterRequired = errors.New("HTTPProtocol requires an http.ResponseWriter")
)

// Pre-defined errors to reduce fmt.Errorf allocations.
// Provide reusable error instances for common failure cases in Beam.
// Used across Renderer and other components for efficiency.
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

// Pre-defined errors for special handling in Renderer.
// ErrHidden suppresses error details; ErrSkip bypasses operations without failure.
// Used in error filters to control response behavior.
var (
	ErrHidden = errors.New("hidden")
	ErrSkip   = errors.New("skip")
)

// responsePool reuses Response objects to reduce memory allocations.
// Manages a sync.Pool for efficient allocation of Response structs.
// Initializes each Response with an empty Meta map.
var responsePool = sync.Pool{
	New: func() interface{} {
		return &Response{
			Meta: make(map[string]interface{}),
		}
	},
}

// getResponse retrieves a Response object from the pool.
// Returns a Response with an initialized Meta map for reuse.
// Caller must call putResponse to return it to the pool.
func getResponse() *Response {
	r := responsePool.Get().(*Response)
	return r
}

// putResponse returns a Response to the pool after resetting it.
// Clears all fields to prevent data leakage between uses.
// Ensures safe reuse in the responsePool for future allocations.
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

// streamBufferPool reuses byte slices for streaming to reduce allocations.
// Manages a sync.Pool for byte buffers with 4KB initial capacity.
// Used in streaming operations to optimize memory usage.
var streamBufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, 4096) // Initial capacity of 4KB
	},
}
