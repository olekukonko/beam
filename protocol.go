package beam

//
//// ProtocolHandler manages protocol-specific behavior.
//type ProtocolHandler struct {
//	protocol Protocol
//}
//
//func NewProtocolHandler(p Protocol) *ProtocolHandler {
//	return &ProtocolHandler{protocol: p}
//}
//
//func (ph *ProtocolHandler) ApplyHeaders(w Writer, code int) error {
//	return ph.protocol.ApplyHeaders(w, code)
//}
//
//// Protocol defines protocol-specific behavior.
//type Protocol interface {
//	ApplyHeaders(w Writer, code int) error
//}
//
//// HTTPProtocol implements the HTTP protocol.
//type HTTPProtocol struct{}
//
//func (p *HTTPProtocol) ApplyHeaders(w Writer, code int) error {
//	if hw, ok := w.(http.ResponseWriter); ok {
//		hw.WriteHeader(code)
//	}
//	return nil
//}
//
//// TCPProtocol implements a basic TCP protocol.
//type TCPProtocol struct{}
//
//func (p *TCPProtocol) ApplyHeaders(w Writer, code int) error {
//	return nil
//}
