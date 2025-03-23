package beam

//import (
//	"encoding/json"
//	"encoding/xml"
//	"fmt"
//	"gopkg.in/vmihailenco/msgpack.v2"
//)
//
//// Default Encoders
//type JSONEncoder struct{}
//
//func (e *JSONEncoder) Marshal(v interface{}) ([]byte, error) { return json.Marshal(v) }
//func (e *JSONEncoder) Unmarshal(data []byte, v interface{}) error {
//	return json.Unmarshal(data, v)
//}
//
//type MsgPackEncoder struct{}
//
//func (e *MsgPackEncoder) Marshal(v interface{}) ([]byte, error) { return msgpack.Marshal(v) }
//func (e *MsgPackEncoder) Unmarshal(data []byte, v interface{}) error {
//	return msgpack.Unmarshal(data, v)
//}
//
//type XMLEncoder struct{}
//
//func (e *XMLEncoder) Marshal(v interface{}) ([]byte, error) { return xml.Marshal(v) }
//func (e *XMLEncoder) Unmarshal(data []byte, v interface{}) error {
//	return xml.Unmarshal(data, v)
//}
//
//type TextEncoder struct{}
//
//func (e *TextEncoder) Marshal(v interface{}) ([]byte, error) {
//	return []byte(fmt.Sprintf("%v", v)), nil
//}
//func (e *TextEncoder) Unmarshal(data []byte, v interface{}) error { return nil }
