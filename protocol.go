package beam

import (
	"fmt"
	"net/http"
)

// Protocol defines protocol-specific behavior.
type Protocol interface {
	ApplyHeaders(w Writer, code int) error
}

// -----------------------------------------------------------------------------
// ProtocolHandler and Protocol Interface
// -----------------------------------------------------------------------------

// ProtocolHandler manages protocol-specific behavior.
type ProtocolHandler struct {
	protocol Protocol
}

// NewProtocolHandler creates a new ProtocolHandler.
func NewProtocolHandler(p Protocol) *ProtocolHandler {
	return &ProtocolHandler{protocol: p}
}

// ApplyHeaders applies protocol-specific headers to the writer.
func (ph *ProtocolHandler) ApplyHeaders(w Writer, code int) error {
	if ph.protocol == nil {
		return fmt.Errorf("protocol cannot be nil")
	}
	return ph.protocol.ApplyHeaders(w, code)
}

// -----------------------------------------------------------------------------
// Protocol Implementations
// -----------------------------------------------------------------------------

// HTTPProtocol implements the HTTP protocol.
type HTTPProtocol struct{}

// ApplyHeaders applies HTTP-specific headers and status code.
func (p *HTTPProtocol) ApplyHeaders(w Writer, code int) error {
	if hw, ok := w.(http.ResponseWriter); ok {
		hw.WriteHeader(code)
		return nil
	}
	return fmt.Errorf("HTTPProtocol requires an http.ResponseWriter")
}

// TCPProtocol implements a basic TCP protocol.
type TCPProtocol struct{}

// ApplyHeaders applies TCP-specific headers (none in this basic implementation).
func (p *TCPProtocol) ApplyHeaders(w Writer, code int) error {
	// TCP doesn’t use headers in the same way as HTTP; this is a no-op for now.
	return nil
}
