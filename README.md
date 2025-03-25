# Beam - Flexible Response Rendering for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/yourusername/beam.svg)](https://pkg.go.dev/github.com/yourusername/beam)
[![Go Report Card](https://goreportcard.com/badge/github.com/yourusername/beam)](https://goreportcard.com/report/github.com/yourusername/beam)
[![Tests](https://github.com/yourusername/beam/actions/workflows/go.yml/badge.svg)](https://github.com/yourusername/beam/actions/workflows/go.yml)

Beam is a powerful Go package designed to simplify and standardize API response generation with support for multiple formats, streaming, and comprehensive error handling.

## Features

- **Multi-format support**: JSON, MsgPack, XML, Text, Binary, Form URL Encoded
- **Streaming capabilities**: Efficient handling of large payloads
- **Context-aware**: Full support for context cancellation
- **Error handling**: Built-in error classification and filtering
- **Customizable**: Extensible encoder system and callback hooks
- **Standardized responses**: Consistent response structure across your API

## Installation

```bash
go get github.com/yourusername/beam
```

## Quick Start

### Basic Usage

```go
package main

import (
	"net/http"
	
	"github.com/yourusername/beam"
)

func main() {
	// Create a new renderer with default settings
	renderer := beam.New(beam.Setting{
		Name:   "myapp",
		Format: beam.FormatJSON,
	})

	http.Handle("/data", renderer.Handler(func(r *beam.Renderer) error {
		data := map[string]interface{}{
			"id":   123,
			"name": "Example Data",
		}
		return r.Info("Data retrieved successfully", data)
	}))

	http.ListenAndServe(":8080", nil)
}
```

## Core Concepts

### Response Structure

Beam uses a standardized response format:

```go
type Response struct {
	Status  string                 `json:"status"`
	Title   string                 `json:"title,omitempty"`
	Message string                 `json:"message,omitempty"`
	Tags    []string               `json:"tags,omitempty"`
	Info    interface{}            `json:"info,omitempty"`
	Data    []interface{}          `json:"data,omitempty"`
	Meta    map[string]interface{} `json:"meta,omitempty"`
	Errors  []string               `json:"errors,omitempty"`
}
```

### Supported Formats

| Format | Content-Type | Description |
|--------|--------------|-------------|
| JSON | `application/json` | Default format, human-readable |
| MsgPack | `application/msgpack` | Binary format for efficiency |
| XML | `application/xml` | XML formatted responses |
| Text | `text/plain` | Simple text responses |
| Binary | `application/octet-stream` | Raw binary data |
| Form URL Encoded | `application/x-www-form-urlencoded` | URL-encoded form data |

## Advanced Usage

### Custom Encoders

```go
// Create a custom encoder for CSV format
type CSVEncoder struct{}

func (e *CSVEncoder) Marshal(v interface{}) ([]byte, error) {
	// Implement CSV marshaling logic
}

func (e *CSVEncoder) Unmarshal(data []byte, v interface{}) error {
	// Implement CSV unmarshaling logic
}

// Register the custom encoder
renderer := beam.New(beam.Setting{Name: "myapp"})
renderer.UseEncoder(beam.FormatText, &CSVEncoder{})
```

### Error Handling

```go
renderer.Error("Failed to process request", 
	errors.New("validation failed"),
	errors.New("database timeout"))

// With custom error filtering
renderer.FilterError(func(err error) bool {
	// Ignore specific errors
	return !errors.Is(err, sql.ErrNoRows)
})
```

### Streaming Large Data

```go
// Stream binary data in chunks
err := renderer.StreamBytes(writer, func(chunk []byte) error {
	// Process each chunk
	return nil
}, 64*1024) // 64KB buffer size

// Stream JSON array elements
err := renderer.StreamJSON(func(enc *json.Encoder) error {
	for _, item := range largeDataset {
		if err := enc.Encode(item); err != nil {
			return err
		}
	}
	return nil
})
```

### Context Cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

renderer.WithContext(ctx).Push(writer, beam.Response{
	Status:  beam.StatusSuccessful,
	Message: "Data processed",
})
```

## Configuration Options

### Renderer Settings

```go
type Setting struct {
	Name          string            // Application name for headers
	Format        Format            // Default output format
	EnableHeaders bool              // Enable sending headers
	Presets       map[string]Preset // Custom presets for content types
}
```

### System Metadata

```go
renderer.WithSystem(beam.SystemShowBoth, beam.System{
	App:      "MyApp",
	Server:   "production",
	Version:  "1.0.0",
	Build:    "2023-01-01",
	Play:     false,
	Duration: 0,
})
```

## Best Practices

1. **Consistent Responses**: Use the standardized response format across your entire API
2. **Error Classification**: Use the built-in error statuses (`StatusError`, `StatusFatal`)
3. **Streaming**: For payloads >1MB, use streaming methods
4. **Context Propagation**: Always pass context through for cancellation support
5. **Custom Encoders**: Register custom encoders at application startup

## Performance Considerations

- For small payloads (<1KB), the standard methods are most efficient
- For medium payloads (1KB-1MB), consider using `Raw()` with pre-encoded data
- For large payloads (>1MB), always use streaming methods
- Reuse renderer instances when possible to benefit from pooled buffers

## Examples

### REST API Handler

```go
func GetUserHandler(w http.ResponseWriter, r *http.Request) {
	renderer := beam.New(beam.Setting{Name: "userapi"}).WithWriter(w)
	
	user, err := db.GetUser(r.URL.Query().Get("id"))
	if err != nil {
		renderer.Error("Failed to get user", err)
		return
	}
	
	renderer.Info("User retrieved", user)
}
```

### Streaming CSV Export

```go
func ExportCSVHandler(w http.ResponseWriter, r *http.Request) {
	renderer := beam.New(beam.Setting{
		Name:   "export",
		Format: beam.FormatText,
	}).WithWriter(w)
	
	renderer.WithHeader("Content-Type", "text/csv")
	renderer.WithHeader("Content-Disposition", "attachment; filename=export.csv")
	
	csvWriter := csv.NewWriter(w)
	defer csvWriter.Flush()
	
	err := renderer.StreamBytesToCallback(func(chunk []byte) error {
		return csvWriter.Write(strings.Split(string(chunk), ","))
	}, 32*1024)
	
	if err != nil {
		renderer.Fatal(err)
	}
}
```

## Contributing

Contributions are welcome! Please follow these guidelines:
1. Fork the repository
2. Create a feature branch
3. Add tests for your changes
4. Submit a pull request

## License

Beam is licensed under the MIT License. See [LICENSE](LICENSE) for details.