# Beam - Flexible Response Rendering for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/olekukonko/beam.svg)](https://pkg.go.dev/github.com/olekukonko/beam)
[![Go Report Card](https://goreportcard.com/badge/github.com/olekukonko/beam)](https://goreportcard.com/report/github.com/olekukonko/beam)
[![Tests](https://github.com/olekukonko/beam/actions/workflows/go.yml/badge.svg)](https://github.com/olekukonko/beam/actions/workflows/go.yml)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Beam is a versatile Go package designed to streamline API response generation with a focus on flexibility, performance, and consistency. Whether you need to render JSON, stream large datasets, or handle errors gracefully, Beam provides a robust toolkit for building modern, scalable web services.

---

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
  - [Streaming Large Data](#streaming-large-data)
  - [Context Cancellation](#context-cancellation)
- [Configuration Options](#configuration-options)
  - [Renderer Settings](#renderer-settings)
  - [System Metadata](#system-metadata)
- [Best Practices and Performance Considerations](#best-practices-and-performance-considerations)
- [Examples](#examples)
  - [REST API Handler](#rest-api-handler)
  - [Streaming CSV Export](#streaming-csv-export)
  - [Sample Full Application](#sample-full-application)
- [Puller Package](#puller-package)
  - [Reader](#reader)
  - [Streamer](#streamer)
- [Contributing](#contributing)
- [License](#license)

---

## Features

- **Multi-format Rendering**: Supports JSON, XML, MsgPack, text, binary, and more out of the box.
- **Streaming Support**: Efficiently streams large payloads to reduce memory usage.
- **Context Awareness**: Integrates with Go’s `context` package for cancellation and timeouts.
- **Robust Error Handling**: Classifies errors, supports custom filters, and triggers callbacks.
- **Extensibility**: Add custom encoders to support additional formats like CSV.
- **Consistent Responses**: Enforces a standardized response structure for all endpoints.

---

## Installation

Install Beam using:

```bash
go get github.com/olekukonko/beam
```

**Note**: Some features (e.g., image rendering) require additional standard library packages like `image/png`

---

## Quick Start

Below is a simple HTTP server that returns a JSON response using Beam:

```go
package main

import (
	"net/http"
	"github.com/olekukonko/beam"
)

func main() {
	// Initialize a renderer with default JSON output.
	renderer := beam.NewRenderer(beam.Setting{
		Name:          "myapp",
		ContentType:   beam.ContentTypeJSON,
		EnableHeaders: true,
	})

	// Define a handler using Beam's Handler helper.
	http.Handle("/data", renderer.Handler(func(r *beam.Renderer) error {
		data := map[string]string{"id": "123", "name": "Example"}
		return r.Info("Data retrieved", data)
	}))

	// Start the HTTP server.
	http.ListenAndServe(":8080", nil)
}
```

When you visit `http://localhost:8080/data`, you will receive a JSON response similar to:

```json
{
  "status": "+ok",
  "message": "Data retrieved",
  "info": {"id": "123", "name": "Example"}
}
```

---

## Core Concepts

### Response Structure

Beam uses a consistent response structure for all outputs:

```go
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
```

**Example Output (JSON):**

```json
{
  "status": "+ok",
  "message": "Operation completed",
  "info": {"user": "Alice"},
  "meta": {"system": {"app": "MyApp", "version": "1.0.0"}}
}
```

### Supported Formats

Beam supports these content types:

| Format               | Content-Type                                    | Description                     |
|----------------------|-------------------------------------------------|---------------------------------|
| **JSON**             | `application/json`                              | Default, human-readable         |
| **MsgPack**          | `application/msgpack`                           | Compact binary format           |
| **XML**              | `application/xml`                               | Structured XML output           |
| **Text**             | `text/plain`                                    | Plain text responses            |
| **Binary**           | `application/octet-stream`                      | Raw binary data                 |
| **Form URL Encoded** | `application/x-www-form-urlencoded`             | URL-encoded key-value pairs     |
| **Images**           | `image/png`, `image/jpeg`, etc.                 | Encoded image data              |

---

## Advanced Usage

### Custom Encoders

You can extend Beam by registering custom encoders. For example, a custom CSV encoder:

```go
type CSVEncoder struct{}

func (e *CSVEncoder) Marshal(v interface{}) ([]byte, error) {
	// Simplified CSV logic
	return []byte(fmt.Sprintf("%v", v)), nil
}

func (e *CSVEncoder) Unmarshal(data []byte, v interface{}) error {
	return fmt.Errorf("unmarshaling not implemented")
}

func (e *CSVEncoder) ContentType() string { return "text/csv" }

// Register the custom CSV encoder.
renderer := beam.NewRenderer(beam.Setting{Name: "myapp"}).UseEncoder(&CSVEncoder{})
```

### Error Handling

Handle errors uniformly using Beam's built-in methods:

```go
renderer.Error("Request failed: %s", errors.New("invalid input"))

// Custom error filtering:
renderer.FilterError(func(err error) bool {
	return errors.Is(err, sql.ErrNoRows) // Ignore "no rows" errors
})
```

### Streaming Large Data

Stream data incrementally to optimize memory usage:

```go
err := renderer.WithContentType(beam.ContentTypeText).Stream(func(r *beam.Renderer) (interface{}, error) {
	// Return a data chunk.
	chunk := "data chunk\n"
	if done {
		return nil, io.EOF // End stream when done.
	}
	return chunk, nil
})
if err != nil {
	renderer.Fatal(err)
}
```

### Context Cancellation

Beam integrates with Go’s `context` package to support timeouts and cancellation:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

renderer.WithContext(ctx).Info("Processing...", nil)
```

---

## Configuration Options

### Renderer Settings

Customize your renderer with various settings:

```go
type Setting struct {
	Name          string            // Application name (used in headers)
	ContentType   string            // Default output content type (e.g., beam.ContentTypeJSON)
	EnableHeaders bool              // Enable or disable header output
	Presets       map[string]Preset // Custom presets for content types
}
```

Example:

```go
renderer := beam.NewRenderer(beam.Setting{
	Name:        "myapp",
	ContentType: beam.ContentTypeJSON,
})
```

### System Metadata

Embed system metadata into responses or headers:

```go
renderer.WithSystem(beam.SystemShowBoth, beam.System{
	App:     "MyApp",
	Version: "1.0.0",
	Build:   "2023-01-01",
	Play:    false,
})
```

---

## Best Practices and Performance Considerations

- **Consistency**: Always use the standardized `Response` structure for uniform outputs.
- **Error Clarity**: Utilize status constants like `StatusError` and `StatusFatal` for clear error states.
- **Streaming**: For payloads larger than 1MB, leverage the `Stream` method to minimize memory usage.
- **Context Propagation**: Pass contexts to support cancellation and timeouts.
- **Logging**: Configure a logger using `SetLogger` for production-level debugging.
- **Reuse**: Benefit from Beam's internal buffer pooling for high-performance responses.

---

## Examples

### REST API Handler

A simple REST API endpoint using Beam:

```go
func GetUserHandler(w http.ResponseWriter, r *http.Request) {
	renderer := beam.NewRenderer(beam.Setting{Name: "api"}).WithWriter(w)
	user := map[string]string{"id": "1", "name": "Alice"}
	if err := dbError(); err != nil {
		renderer.Error("Database error: %s", err)
		return
	}
	renderer.Info("User found", user)
}
```

### Streaming CSV Export

Export large datasets as CSV:

```go
func ExportCSV(w http.ResponseWriter, r *http.Request) {
	renderer := beam.NewRenderer(beam.Setting{
		Name:        "export",
		ContentType: "text/csv",
	}).WithWriter(w).WithHeader("Content-Disposition", "attachment; filename=data.csv")

	err := renderer.Stream(func(r *beam.Renderer) (interface{}, error) {
		// Return CSV data as a string.
		return "id,name\n1,Alice\n", io.EOF
	})
	if err != nil {
		renderer.Fatal(err)
	}
}
```

### Sample Full Application

The following complete sample application integrates Beam with the Chi router and demonstrates multiple endpoints:

```go
package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"net/http"

	"github.com/olekukonko/beam"
	"github.com/go-chi/chi/v5"
)

// SimpleLogger implements the Logger interface.
type SimpleLogger struct{}

func (l *SimpleLogger) Log(err error) bool {
	fmt.Println("LOG:", err.Error())
	return true
}

func main() {
	// Initialize the Beam renderer with custom settings.
	renderer := beam.NewRenderer(beam.Setting{
		Name:          "beam",
		EnableHeaders: true,
	}).SetLogger(&SimpleLogger{}).
		WithCallback(func(data beam.CallbackData) {
			if data.Status == beam.StatusSuccessful {
				fmt.Printf("Success: %+v\n", data)
			} else if data.IsError() {
				fmt.Printf("Error: %+v\n", data)
			}
		}).WithSystem(beam.SystemShowBoth, beam.System{
			App:     "MyApp",
			Server:  "localhost",
			Version: "1.0.0",
			Build:   "20250323",
			Play:    true,
		})

	// Create a Chi router.
	r := chi.NewRouter()

	// Define endpoints using the renderer.
	r.Get("/hello", renderer.Handler(func(r *beam.Renderer) error {
		return r.Info("Hello, world!", map[string]string{"greeting": "Hi!"})
	}))
	r.Get("/error", renderer.Handler(func(r *beam.Renderer) error {
		return r.Error("error %s", errors.New("oops"))
	}))
	r.Get("/xml", renderer.Handler(func(r *beam.Renderer) error {
		return r.WithContentType(beam.ContentTypeXML).Error("error %s", errors.New("oops"))
	}))
	r.Get("/meta", renderer.Handler(func(r *beam.Renderer) error {
		return r.WithContentType(beam.ContentTypeXML).
			WithMeta("custom", "value").
			Info("test", nil)
	}))
	r.Get("/fatal", renderer.Handler(func(r *beam.Renderer) error {
		return r.Fatal(errors.New("danger"))
	}))
	r.Get("/image", renderer.Handler(func(r *beam.Renderer) error {
		return r.WithHeader("name", "sample").Image(beam.ContentTypePNG, dummyImage(300, 300))
	}))

	fmt.Println("Listening on :4040")
	if err := http.ListenAndServe(":4040", r); err != nil {
		fmt.Printf("Server failed: %v\n", err)
	}
}

// dummyImage creates a simple solid blue image for demonstration.
func dummyImage(width, height int) image.Image {
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

In this sample:
- A custom logger is set via `SetLogger`.
- Several endpoints demonstrate standard responses, error handling, XML output, custom metadata, fatal errors, and image rendering.
- The `dummyImage` function (corrected from `dummayImage`) generates a simple blue image.

---

## Puller Package

The Beam repository also includes the `puller` package, which provides tools for decoding data from an `io.Reader`. It offers both a one-shot Reader and a streaming Streamer for handling large or continuous data.

### Reader

The `puller.Reader` type supports reading all data and decoding it using various formats (JSON, XML, MsgPack, etc.). For example:

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
		fmt.Println("Error decoding JSON:", err)
	} else {
		fmt.Println("Decoded JSON:", result)
	}
}
```

### Streamer

The `puller.Streamer` type enables you to process data in chunks using a callback. For example:

```go
package main

import (
	"bytes"
	"fmt"
	"io"
	"github.com/olekukonko/beam/puller"
)

func main() {
	// Simulate a large input stream.
	data := bytes.Repeat([]byte("chunk data\n"), 100)
	streamer := puller.NewStreamer(bytes.NewReader(data))
	
	err := streamer.Bytes(func(chunk []byte) error {
		fmt.Print(string(chunk))
		return nil
	}, 1024)
	
	if err != nil && err != io.EOF {
		fmt.Println("Error streaming data:", err)
	}
}
```

---

## License

Beam is licensed under the MIT License. See [LICENSE](LICENSE) for details.
