# Beam - Flexible Response Rendering for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/olekukonko/beam.svg)](https://pkg.go.dev/github.com/olekukonko/beam)
[![Go Report Card](https://goreportcard.com/badge/github.com/olekukonko/beam)](https://goreportcard.com/report/github.com/olekukonko/beam)
[![Tests](https://github.com/olekukonko/beam/actions/workflows/go.yml/badge.svg)](https://github.com/olekukonko/beam/actions/workflows/go.yml)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Beam is a Go package for streamlined API response generation, emphasizing flexibility, performance, and consistency. It supports multiple formats, efficient streaming, and robust error handling, making it ideal for modern, scalable web services.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Core Concepts](#core-concepts)
    - [Response Structure](#response-structure)
    - [Supported Formats](#supported-formats)
- [Advanced Usage](#advanced-usage)
    - [Custom Encoders](#custom-encoders)
    - [Error Handling](#error-handling)
    - [Streaming Data](#streaming-data)
    - [Context Support](#context-support)
- [Configuration](#configuration)
    - [Renderer Settings](#renderer-settings)
    - [System Metadata](#system-metadata)
- [Performance Tips](#performance-tips)
- [Examples](#examples)
    - [Basic REST API](#basic-rest-api)
    - [Streaming CSV Export](#streaming-csv-export)
    - [Full Application](#full-application)
- [Puller Package](#puller-package)
    - [Reader](#reader)
    - [Streamer](#streamer)
- [Contributing](#contributing)
- [License](#license)

## Features

- **Multi-format Support**: JSON, XML, MsgPack, text, binary, form-urlencoded, and images (PNG, JPEG, GIF, WebP).
- **Efficient Streaming**: Handles large datasets with minimal memory usage via streaming.
- **Context-Aware**: Integrates with Go’s `context` package for cancellation and timeouts.
- **Robust Error Handling**: Supports error classification, custom filters, and callback triggers.
- **Extensible**: Allows custom encoders for additional formats (e.g., CSV).
- **Consistent Responses**: Enforces a standardized response structure for all endpoints.
- **Memory Optimization**: Uses `sync.Pool` for response objects and buffers to reduce allocations.

## Installation

Install Beam using:

```bash
go get github.com/olekukonko/beam
```

**Dependencies**:
- Standard library: `net/http`, `image`, `image/png`, `image/jpeg`, `image/gif`.
- External: `github.com/vmihailenco/msgpack/v5` for MsgPack, `github.com/HugoSmits86/nativewebp` for WebP.

## Quick Start

Create a simple HTTP server with Beam to return a JSON response:

```go
package main

import (
    "net/http"
    "github.com/olekukonko/beam"
)

func main() {
    // Initialize renderer with JSON output
    renderer := beam.NewRenderer(beam.Setting{
        Name:          "myapp",
        ContentType:   beam.ContentTypeJSON,
        EnableHeaders: true,
    })

    // Define a handler for a /data endpoint
    http.Handle("/data", renderer.Handler(func(r *beam.Renderer) error {
        data := map[string]string{"id": "123", "name": "Example"}
        return r.Info("Data retrieved", data)
    }))

    // Start server
    http.ListenAndServe(":8080", nil)
}
```

**Output** (at `http://localhost:8080/data`):

```json
{
    "status": "+ok",
    "message": "Data retrieved",
    "info": {"id": "123", "name": "Example"}
}
```

## Core Concepts

### Response Structure

Beam uses a consistent `Response` struct for all outputs:

```go
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
```

**Example JSON Output**:

```json
{
    "status": "+ok",
    "message": "Operation completed",
    "info": {"user": "Alice"},
    "meta": {"system": {"app": "MyApp", "version": "1.0.0"}}
}
```

### Supported Formats

Beam supports multiple content types for flexible response rendering:

| Format               | Content-Type                                    | Description                     |
|----------------------|-------------------------------------------------|---------------------------------|
| JSON                 | `application/json`                              | Human-readable, default format  |
| MsgPack              | `application/msgpack`                           | Compact binary format           |
| XML                  | `application/xml`                               | Structured XML output           |
| Text                 | `text/plain`                                    | Plain text responses            |
| Binary               | `application/octet-stream`                      | Raw binary data                 |
| Form URL Encoded     | `application/x-www-form-urlencoded`             | URL-encoded key-value pairs     |
| Server-Sent Events   | `text/event-stream`                             | Streaming event data            |
| Images               | `image/png`, `image/jpeg`, `image/gif`, `image/webp` | Encoded image data        |

## Advanced Usage

### Custom Encoders

Extend Beam with custom encoders for additional formats (e.g., CSV):

```go
package main

import (
    "fmt"
    "github.com/olekukonko/beam"
)

// CSVEncoder implements a simple CSV encoder
type CSVEncoder struct{}

func (e *CSVEncoder) Marshal(v interface{}) ([]byte, error) {
    return []byte(fmt.Sprintf("%v", v)), nil // Simplified CSV logic
}

func (e *CSVEncoder) Unmarshal(data []byte, v interface{}) error {
    return fmt.Errorf("unmarshaling not implemented")
}

func (e *CSVEncoder) ContentType() string { return "text/csv" }

// Register custom encoder
renderer := beam.NewRenderer(beam.Setting{Name: "myapp"}).UseEncoder(&CSVEncoder{})
```

### Error Handling

Beam provides flexible error handling with predefined errors and filters:

```go
package main

import (
    "database/sql"
    "errors"
    "github.com/olekukonko/beam"
)

// Handle errors with custom message
renderer.ErrorWith("Request failed", errors.New("invalid input"))

// Add custom error filter to ignore specific errors
renderer = renderer.FilterError(func(err error) bool {
    return errors.Is(err, sql.ErrNoRows) // Ignore "no rows" errors
})
```

### Streaming Data

Stream large datasets efficiently to minimize memory usage:

```go
package main

import (
    "io"
    "github.com/olekukonko/beam"
)

func streamData(r *beam.Renderer) error {
    done := false
    return r.WithContentType(beam.ContentTypeText).Stream(func(r *beam.Renderer) (interface{}, error) {
        if done {
            return nil, io.EOF // Signal end of stream
        }
        done = true
        return "data chunk\n", nil
    })
}
```

### Context Support

Integrate with Go’s `context` package for cancellation and timeouts:

```go
package main

import (
    "context"
    "time"
    "github.com/olekukonko/beam"
)

func withContext(r *beam.Renderer) error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    return r.WithContext(ctx).Info("Processing...", nil)
}
```

## Configuration

### Renderer Settings

Configure the `Renderer` with a `Setting` struct:

```go
type Setting struct {
    Name          string            // Application name for header prefixes
    ContentType   string            // Default content type (e.g., ContentTypeJSON)
    EnableHeaders bool              // Enable/disable header output
    Presets       map[string]Preset // Custom headers for content types
}
```

**Example**:

```go
renderer := beam.NewRenderer(beam.Setting{
    Name:          "myapp",
    ContentType:   beam.ContentTypeJSON,
    EnableHeaders: true,
})
```

### System Metadata

Include system metadata in headers or response body:

```go
renderer.WithSystem(beam.SystemShowBoth, beam.System{
    App:     "MyApp",
    Version: "1.0.0",
    Build:   "2025-08-19",
    Play:    false,
})
```

## Performance Tips

- **Use Streaming**: For payloads >1MB, use `Stream` to reduce memory usage.
- **Leverage Pooling**: Beam’s `sync.Pool` for responses and buffers minimizes allocations.
- **Filter Errors**: Use `FilterError` to skip non-critical errors (e.g., `sql.ErrNoRows`).
- **Context Propagation**: Always pass `context.Context` for cancellation support.
- **Custom Encoders**: Implement efficient encoders for specific use cases to avoid overhead.
- **Logging**: Configure a logger with `WithLogger` for production debugging.

## Examples

### Basic REST API

Create a REST endpoint with error handling:

```go
package main

import (
    "database/sql"
    "net/http"
    "github.com/olekukonko/beam"
)

func GetUserHandler(w http.ResponseWriter, req *http.Request) {
    renderer := beam.NewRenderer(beam.Setting{Name: "api"}).WithWriter(w)
    user := map[string]string{"id": "1", "name": "Alice"}
    if err := dbQuery(); err != nil {
        return renderer.ErrorWith("Database error", err)
    }
    return renderer.Info("User found", user)
}

// Simulate database query
func dbQuery() error {
    return sql.ErrNoRows
}
```

### Streaming CSV Export

Export large datasets as CSV:

```go
package main

import (
    "io"
    "net/http"
    "github.com/olekukonko/beam"
)

func ExportCSV(w http.ResponseWriter, req *http.Request) {
    renderer := beam.NewRenderer(beam.Setting{
        Name:        "export",
        ContentType: "text/csv",
    }).WithWriter(w).WithHeader("Content-Disposition", "attachment; filename=data.csv")

    var count int
    return renderer.Stream(func(r *beam.Renderer) (interface{}, error) {
        if count >= 2 {
            return nil, io.EOF // End stream after two chunks
        }
        count++
        return fmt.Sprintf("id,name\n%d,User%d\n", count, count), nil
    })
}
```

### Full Application

A complete application using Beam with Chi router:

```go
package main

import (
    "errors"
    "fmt"
    "image"
    "image/color"
    "net/http"
    "github.com/go-chi/chi/v5"
    "github.com/olekukonko/beam"
)

// SimpleLogger implements beam.Logger
type SimpleLogger struct{}

func (l *SimpleLogger) Log(err error) bool {
    fmt.Println("LOG:", err.Error())
    return true
}

func main() {
    // Initialize renderer with custom settings
    renderer := beam.NewRenderer(beam.Setting{
        Name:          "beam",
        EnableHeaders: true,
        ContentType:   beam.ContentTypeJSON,
    }).WithLogger(&SimpleLogger{}).WithSystem(beam.SystemShowBoth, beam.System{
        App:     "MyApp",
        Server:  "localhost",
        Version: "1.0.0",
        Build:   "20250819",
        Play:    true,
    }).WithCallback(func(data beam.CallbackData) {
        fmt.Printf("Callback: Status=%s, Message=%s\n", data.Status, data.Message)
    })

    // Set up Chi router
    r := chi.NewRouter()

    // Define endpoints
    r.Get("/hello", renderer.Handler(func(r *beam.Renderer) error {
        return r.Info("Hello, world!", map[string]string{"greeting": "Hi!"})
    }))
    r.Get("/error", renderer.Handler(func(r *beam.Renderer) error {
        return r.ErrorWith("Operation failed", errors.New("invalid request"))
    }))
    r.Get("/xml", renderer.Handler(func(r *beam.Renderer) error {
        return r.WithContentType(beam.ContentTypeXML).Info("XML response", nil)
    }))
    r.Get("/image", renderer.Handler(func(r *beam.Renderer) error {
        return r.Image(beam.ContentTypePNG, createImage(300, 300))
    }))

    // Start server
    fmt.Println("Listening on :4040")
    if err := http.ListenAndServe(":4040", r); err != nil {
        fmt.Printf("Server failed: %v\n", err)
    }
}

// createImage generates a solid blue image
func createImage(width, height int) image.Image {
    img := image.NewRGBA(image.Rect(0, 0, width, height))
    blue := color.RGBA{0, 0, 255, 255}
    for y := 0; y < height; y++ {
        for x := 0; x < width; x++ {
            img.Set(x, y, blue)
        }
    }
    return img
}
```

**Endpoints**:
- `/hello`: Returns a JSON greeting with metadata.
- `/error`: Returns a JSON error response.
- `/xml`: Returns an XML response.
- `/image`: Returns a PNG image.

## Puller Package

The `puller` package provides utilities for decoding data from an `io.Reader`.

### Reader

`puller.Reader` decodes data in one shot (JSON, XML, MsgPack, etc.):

```go
package main

import (
    "bytes"
    "fmt"
    "github.com/olekukonko/beam/puller"
)

func main() {
    data := []byte(`{"name": "Alice", "age": 30}`)
    reader := puller.NewReader(bytes.NewReader(data))
    var result map[string]interface{}
    if err := reader.JSON(&result); err != nil {
        fmt.Println("Error:", err)
    } else {
        fmt.Println("Decoded:", result)
    }
}
```

### Streamer

`puller.Streamer` processes data in chunks for efficiency:

```go
package main

import (
    "bytes"
    "fmt"
    "github.com/olekukonko/beam/puller"
)

func main() {
    data := bytes.Repeat([]byte("chunk\n"), 10)
    streamer := puller.NewStreamer(bytes.NewReader(data))
    err := streamer.Bytes(func(chunk []byte) error {
        fmt.Print(string(chunk))
        return nil
    }, 1024)
    if err != nil {
        fmt.Println("Error:", err)
    }
}
```

## Contributing

Contributions are welcome! Please:
1. Fork the repository.
2. Create a feature branch (`git checkout -b feature/xyz`).
3. Submit a pull request with clear descriptions and tests.

See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## License

Beam is licensed under the MIT License. See [LICENSE](LICENSE) for details.