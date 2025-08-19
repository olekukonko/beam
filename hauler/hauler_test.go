package hauler

import (
	"bytes"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	h := New()
	if h == nil {
		t.Fatal("New() returned nil")
	}
}

func TestRead_NilRequest(t *testing.T) {
	var data interface{}
	err := Read(nil, &data)
	if !errors.Is(err, ErrNilRequest) {
		t.Errorf("Expected ErrNilRequest, got %v", err)
	}
}

func TestRead_JSON(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		content string
		want    string
		wantErr bool
	}{
		{
			name:    "valid json",
			body:    `{"name":"test"}`,
			content: ContentTypeJSON,
			want:    "test",
		},
		{
			name:    "invalid json",
			body:    `{"name":}`,
			content: ContentTypeJSON,
			wantErr: true,
		},
		{
			name:    "json with charset",
			body:    `{"name":"test"}`,
			content: ContentTypeJSON + "; charset=utf-8",
			want:    "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.content)

			var data struct{ Name string }
			err := Read(req, &data)

			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if data.Name != tt.want {
				t.Errorf("Expected %q, got %q", tt.want, data.Name)
			}
		})
	}
}

func TestRead_XML(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		content string
		want    string
		wantErr bool
	}{
		{
			name:    "valid xml",
			body:    `<Data><Name>test</Name></Data>`,
			content: ContentTypeXML,
			want:    "test",
		},
		{
			name:    "invalid xml",
			body:    `<Data><Name>test</Data>`,
			content: ContentTypeXML,
			wantErr: true,
		},
		{
			name:    "text/xml",
			body:    `<Data><Name>test</Name></Data>`,
			content: "text/xml",
			want:    "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.content)

			var data struct {
				Name string `xml:"Name"`
			}
			err := Read(req, &data)

			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if data.Name != tt.want {
				t.Errorf("Expected %q, got %q", tt.want, data.Name)
			}
		})
	}
}

func TestRead_FormURLEncoded(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    map[string]string
		wantErr bool
		errText string // expected error substring
	}{
		{
			name: "simple form",
			body: "name=test&value=123",
			want: map[string]string{"name": "test", "value": "123"},
		},
		{
			name:    "empty key",
			body:    "name=test&=123",
			wantErr: true,
			errText: "empty key",
		},
		{
			name:    "malformed encoding",
			body:    "name=test&%zz=123",
			wantErr: true,
			errText: "invalid form data",
		},
		{
			name: "empty form",
			body: "",
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", ContentTypeFormURLEncoded)

			var data map[string]string
			err := Read(req, &data)

			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if tt.errText != "" && !strings.Contains(err.Error(), tt.errText) {
					t.Errorf("Expected error containing %q, got %q", tt.errText, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if len(data) != len(tt.want) {
				t.Fatalf("Expected %d items, got %d", len(tt.want), len(data))
			}
			for k, v := range tt.want {
				if data[k] != v {
					t.Errorf("Expected %q=%q, got %q=%q", k, v, k, data[k])
				}
			}
		})
	}
}

func TestRead_Text(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "simple text",
			body: "plain text",
			want: "plain text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", ContentTypeText)

			var data string
			err := Read(req, &data)

			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if data != tt.want {
				t.Errorf("Expected %q, got %q", tt.want, data)
			}
		})
	}
}

func TestRead_MsgPack(t *testing.T) {
	// MsgPack test requires binary data - using a simple example
	msgpackData := []byte{0x81, 0xA4, 0x6E, 0x61, 0x6D, 0x65, 0xA4, 0x74, 0x65, 0x73, 0x74}

	t.Run("valid msgpack", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader(msgpackData))
		req.Header.Set("Content-Type", ContentTypeMsgPack)

		var data map[string]string
		err := Read(req, &data)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if data["name"] != "test" {
			t.Errorf("Expected 'test', got %q", data["name"])
		}
	})
}

func TestRead_UnsupportedType(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/unknown")

	var data interface{}
	err := Read(req, &data)
	if err == nil {
		t.Fatal("Expected error for unsupported type")
	}
	if !strings.Contains(err.Error(), "unsupported content type") {
		t.Errorf("Expected unsupported content type error, got %v", err)
	}
}

func TestRead_InvalidPointer(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
	req.Header.Set("Content-Type", ContentTypeJSON)

	err := Read(req, nil)
	if err != ErrInvalidPointer {
		t.Errorf("Expected ErrInvalidPointer, got %v", err)
	}
}

func TestRegister(t *testing.T) {
	h := New()
	customParser := &testParser{}

	h.Register(customParser)

	req := httptest.NewRequest("POST", "/", strings.NewReader("test"))
	req.Header.Set("Content-Type", "test/type")

	var data string
	err := h.Read(req, &data)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if data != "parsed" {
		t.Errorf("Expected 'parsed', got %q", data)
	}
}

type testParser struct{}

func (p *testParser) CanParse(contentType string) bool {
	return contentType == "test/type"
}

func (p *testParser) Parse(body io.Reader, v interface{}) error {
	if s, ok := v.(*string); ok {
		*s = "parsed"
		return nil
	}
	return errors.New("invalid type")
}

func TestDefaultReader(t *testing.T) {
	if DefaultReader == nil {
		t.Fatal("DefaultReader is nil")
	}
}
