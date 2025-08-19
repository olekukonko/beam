package beam

import (
	"net/http"
)

// Protocol defines protocol-specific behavior.
// Specifies a method to apply headers to a Writer.
// Implemented by HTTPProtocol, TCPProtocol, and custom protocols.
type Protocol interface {
	ApplyHeaders(w Writer, code int) error
}

// ProtocolHandler manages protocol-specific behavior.
// Wraps a Protocol to handle header application.
// Used by Renderer to apply protocol-specific headers.
type ProtocolHandler struct {
	protocol Protocol
}

// NewProtocolHandler creates a new ProtocolHandler.
// Takes a Protocol to manage header application.
// Returns a *ProtocolHandler with the specified protocol.
func NewProtocolHandler(p Protocol) *ProtocolHandler {
	return &ProtocolHandler{protocol: p}
}

// ApplyHeaders applies protocol-specific headers to the writer.
// Takes a Writer and HTTP status code to apply headers.
// Returns an error if the protocol is nil or header application fails.
func (ph *ProtocolHandler) ApplyHeaders(w Writer, code int) error {
	if ph.protocol == nil {
		return errNilProtocol
	}
	return ph.protocol.ApplyHeaders(w, code)
}

// HTTPProtocol implements the HTTP protocol.
// Provides HTTP-specific header application for responses.
// Writes status codes to http.ResponseWriter.
type HTTPProtocol struct{}

// ApplyHeaders applies HTTP-specific headers and status code.
// Takes a Writer and HTTP status code to write the status.
// Returns an error if the Writer is not an http.ResponseWriter.
func (p *HTTPProtocol) ApplyHeaders(w Writer, code int) error {
	if hw, ok := w.(http.ResponseWriter); ok {
		hw.WriteHeader(code)
		return nil
	}
	return errHTTPWriterRequired
}

// TCPProtocol implements a basic TCP protocol.
// Provides TCP-specific header application (currently a no-op).
// Suitable for protocols without header requirements.
type TCPProtocol struct{}

// ApplyHeaders applies TCP-specific headers (none in this basic implementation).
// Takes a Writer and HTTP status code (ignored for TCP).
// Returns nil as TCP does not use headers in this implementation.
func (p *TCPProtocol) ApplyHeaders(w Writer, code int) error {
	// TCP doesn’t use headers in the same way as HTTP; this is a no-op for now.
	return nil
}
