# Beam - Flexible Response Rendering for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/olekukonko/beam.svg)](https://pkg.go.dev/github.com/olekukonko/beam)
[![Go Report Card](https://goreportcard.com/badge/github.com/olekukonko/beam)](https://goreportcard.com/report/github.com/olekukonko/beam)
[![Tests](https://github.com/olekukonko/beam/actions/workflows/go.yml/badge.svg)](https://github.com/olekukonko/beam/actions/workflows/go.yml)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Beam is a high-performance Go package for streamlined API response generation. It emphasizes flexibility, consistency, and a first-class developer experience with a powerful, modern error handling system. It supports multiple formats, efficient streaming, and helps you build robust, scalable web services with ease.

> **Upgrading?**
> This version introduces a powerful new error handling API and other enhancements. Please see the **[Migration Guide](MIGRATION.md)** for details on breaking changes.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Core Concepts](#core-concepts)
    - [The Renderer](#the-renderer)
    - [Response Structure](#response-structure)
- [Advanced Error Handling](#advanced-error-handling)
    - [Error, Warning, and Fatal Responses](#error-warning-and-fatal-responses)
    - [Filtering and Transforming Errors](#filtering-and-transforming-errors)
    - [Redacting Sensitive Errors](#redacting-sensitive-errors)
    - [Controlling Error Severity](#controlling-error-severity)
- [Parsing Request Bodies](#parsing-request-bodies)
- [Advanced Usage](#advanced-usage)
    - [Streaming Data](#streaming-data)
    - [Response Actions (HATEOAS)](#response-actions-hateoas)
    - [System Metadata](#system-metadata)
    - [Custom Encoders](#custom-encoders)
    - [Context Support](#context-support)
- [Full Application Example](#full-application-example)
- [Contributing](#contributing)
- [License](#license)

## Features

-   **Powerful Error Handling**: Granular control to `Skip`, `Redact`, or `Convert` errors.
-   **Leveled Logging**: Structured `Error` and `Fatal` logging with automatic caller info (file, line, function).
-   **Sensitive Data Protection**: Automatically redact sensitive error details in responses while keeping them in logs.
-   **Request Parsing**: Built-in helpers (`r.JSON`, `r.XML`, etc.) for easy request body decoding.
-   **Multi-format Support**: JSON, XML, MsgPack, text, binary, and images (PNG, JPEG, GIF, WebP).
-   **HATEOAS Support**: Add `Actions` to your responses to guide API clients.
-   **Efficient Streaming**: Handle large datasets with minimal memory usage via `r.Stream()`.
-   **Context-Aware**: Integrates with Go’s `context` package for cancellation and timeouts.
-   **Extensible**: Add custom encoders for any format (e.g., CSV, Protobuf).
-   **Memory Optimized**: Uses `sync.Pool` for response objects and buffers to reduce GC pressure.

## Installation

```bash
go get github.com/olekukonko/beam
```

## Quick Start

Create a simple HTTP server that returns a structured JSON response.

```go
package main

import (
	"net/http"
	"github.com/olekukonko/beam"
)

func main() {
	// Initialize a renderer. It's safe for concurrent use.
	renderer := beam.NewRenderer(beam.Setting{
		Name: "myapp",
	})

	// Define an HTTP handler using Beam's helper
	http.Handle("/users/1", renderer.Handler(func(r *beam.Renderer) error {
		// Define the data to be sent
		user := map[string]string{"id": "1", "name": "Alice"}
		
		// Send a successful response with a message and data payload
		return r.Data("User found successfully", user)
	}))

	// Start the server
	http.ListenAndServe(":8080", nil)
}
```

**Output** (at `http://localhost:8080/users/1`):

```json
{
    "status": "+ok",
    "message": "User found successfully",
    "data": {
        "id": "1",
        "name": "Alice"
    }
}
```

## Core Concepts

### The Renderer

The `*beam.Renderer` is the heart of the library. It's an **immutable builder** for your responses. Every call to a `With...` method (e.g., `r.WithStatus(404)`) returns a *new copy* of the renderer with the change applied. This makes it safe to pass around and use concurrently without worrying about race conditions.

```go
// Start with a base renderer
baseRenderer := beam.NewRenderer(beam.Setting{Name: "api"})

// Create a specialized renderer for a specific handler
func myHandler(w http.ResponseWriter, req *http.Request) {
    // Each request gets its own configured renderer
    r := baseRenderer.WithWriter(w).WithContext(req.Context())
    
    // This renderer is configured for XML output, but the baseRenderer remains unchanged
    xmlRenderer := r.WithContentType(beam.ContentTypeXML)
    xmlRenderer.Msg("Hello from XML!")
}
```

### Response Structure

Beam uses a consistent `Response` struct for all structured outputs, ensuring your API is predictable.

```go
type Response struct {
    Status  string                 `json:"status"`
    Title   string                 `json:"title,omitempty"`
    Message string                 `json:"message,omitempty"`
    Tags    []string               `json:"tags,omitempty"`
    Info    interface{}            `json:"info,omitempty"`
    Data    interface{}            `json:"data,omitempty"`
    Meta    map[string]interface{} `json:"meta,omitempty"`
    Errors  ErrorList              `json:"errors,omitempty"`
    Actions []Action               `json:"actions,omitempty"` // For HATEOAS
}
```

## Advanced Error Handling

This is where Beam truly shines. It provides a sophisticated system for managing errors gracefully and securely.

### Error, Warning, and Fatal Responses

The API offers a clear distinction between error severities.

```go
// A standard, non-fatal error (HTTP 400 Bad Request)
if err != nil {
    return r.Errorf("Validation failed for user %s: %w", userID, err)
}

// A warning that doesn't halt the entire process
r.Warningf("Cache miss for key: %s", cacheKey)

// A critical, fatal error (HTTP 500 Internal Server Error)
// This will also log the error with stack context if a logger is configured.
if err != nil {
    return r.FatalMsg("Failed to connect to database", err)
}
```

### Filtering and Transforming Errors

Configure the renderer to globally `Skip`, `Redact`, or `Convert` certain errors.

```go
renderer := beam.NewRenderer(beam.Setting{}).
    // 1. Skip: Don't treat `sql.ErrNoRows` as an error for the client.
    WithSkipFilter(func(err error) bool {
        return errors.Is(err, sql.ErrNoRows)
    }).
    // 2. Redact: Hide details of a specific error type.
    WithRedactFilter(func(err error) bool {
        return errors.Is(err, &MySensitiveError{})
    }).
    // 3. Convert: Treat a specific error as non-fatal, even in r.Fatal().
    WithConvertFilter(func(err error) error {
        if errors.Is(err, &NonCriticalDBError{}) {
            return beam.ToNormal(err) // Downgrade severity
        }
        return err
    })

// This call will now do nothing and return `nil`, because the filter skips it.
r.Error(sql.ErrNoRows) 
```

### Redacting Sensitive Errors

To prevent leaking sensitive information, wrap an error with `beam.ErrHidden`. The details will be masked in the API response but fully visible in your logs.

```go
// The original error contains sensitive info
authErr := errors.New("invalid password for user 'admin'")

// Wrap it to redact the output
err := fmt.Errorf("auth failed: %w", beam.ErrHidden)

r.ErrorMsg("Login failed", authErr)
```

**JSON Output:**
```json
{
    "status": "-error",
    "message": "Login failed",
    "errors": [
        "inva [REDACTED]"
    ]
}
```

### Controlling Error Severity

You can dynamically change an error's severity using `beam.ToFatal` and `beam.ToNormal`.

```go
// Even though we call r.Error(), this will be treated as a fatal error
// because we've wrapped it with ToFatal.
r.Error(beam.ToFatal(errors.New("corruption detected")))
```

## Parsing Request Bodies

Beam integrates the `hauler` package to provide simple helpers for decoding request bodies.

```go
func CreateUser(r *beam.Renderer, req *http.Request) error {
    var user User
    // Automatically decodes JSON, XML, MsgPack, etc., based on Content-Type
    if err := r.Request(req, &user); err != nil {
        return r.Errorf("Invalid request body: %w", err)
    }

    // Or use a specific parser
    if err := r.JSON(req, &user); err != nil {
        return r.Errorf("Invalid JSON: %w", err)
    }

    // ... process user ...
    return r.Msg("User created")
}
```

## Advanced Usage

### Streaming Data

Stream large datasets like CSV files or logs without consuming excessive memory.

```go
func exportUsers(r *beam.Renderer) error {
    // Set headers for file download
    r = r.WithContentType("text/csv").
        WithHeader("Content-Disposition", "attachment; filename=users.csv")

    // The callback is called repeatedly until it returns io.EOF
    return r.Stream(func(renderer *beam.Renderer) (interface{}, error) {
        users, err := getNextBatchOfUsers() // Your function to get data chunks
        if err == io.EOF {
            return nil, io.EOF // Signal end of stream
        }
        if err != nil {
            return nil, err // Propagate errors
        }
        return convertUsersToCSV(users), nil
    })
}```

### Response Actions (HATEOAS)

Guide your API clients by telling them what actions they can perform next.

```go
r.WithAction(beam.Action{
    Name:   "view-profile",
    Method: "GET",
    Href:   "/api/users/123",
}, beam.Action{
    Name:   "update-profile",
    Method: "PUT",
    Href:   "/api/users/123",
}).Data("User logged in", user)
```

**JSON Output:**
```json
{
    "status": "+ok",
    "message": "User logged in",
    "data": { "...": "..." },
    "actions": [
        { "name": "view-profile", "method": "GET", "href": "/api/users/123" },
        { "name": "update-profile", "method": "PUT", "href": "/api/users/123" }
    ]
}
```

### System Metadata

Include application metadata in headers, the response body, or both.

```go
renderer := beam.NewRenderer(beam.Setting{}).
    WithSystem(beam.SystemShowBoth, beam.System{
        App:     "MyApp",
        Version: "1.2.3",
        Build:   "abcdef",
    })

// This will add X-MyApp-Version headers AND a "system" object in the JSON body.
renderer.Msg("Ready")
```

### Custom Encoders

Beam is fully extensible. Add support for any format by implementing the `Encoder` interface.

```go
type CSVEncoder struct{}
func (e *CSVEncoder) Marshal(v interface{}) ([]byte, error) { /* ... */ }
func (e *CSVEncoder) Unmarshal(data []byte, v interface{}) error { /* ... */ }
func (e *CSVEncoder) ContentType() string { return "text/csv" }

// Register and use it
renderer.UseEncoder(&CSVEncoder{}).
    WithContentType("text/csv").
    Raw("id,name\n1,alice")
```

### Context Support

All operations respect `context.Context` for handling cancellations and deadlines.

```go
ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
defer cancel()

// If the context expires, the Push operation will return ErrContextCanceled
err := r.WithContext(ctx).Push(w, response)
```

## Full Application Example

Here is a complete example using the `chi` router and showcasing advanced features like logging, error handling, and request parsing.

```go
package main

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/olekukonko/beam"
)

// A simple structured logger
type AppLogger struct{}
func (l *AppLogger) Error(err error, fields ...interface{}) { 
    fmt.Println("[ERROR]", err, fields) 
}
func (l *AppLogger) Fatal(err error, fields ...interface{}) { 
    fmt.Println("[FATAL]", err, fields)
}

func main() {
	// 1. Initialize a base renderer with global settings
	renderer := beam.NewRenderer(beam.Setting{Name: "beam-app"}).
		WithLogger(&AppLogger{}).
		WithSystem(beam.SystemShowBody, beam.System{
			App: "MyAwesomeApp", Version: "1.0.0",
		})

	r := chi.NewRouter()

	// 2. Handle successful requests
	r.Get("/hello", renderer.Handler(func(r *beam.Renderer) error {
		return r.Info("Hello, world!", map[string]string{"greeting": "Hi!"})
	}))

	// 3. Handle requests with potential errors
	r.Get("/users/{id}", renderer.Handler(func(r *beam.Renderer) error {
		// This error would be logged as fatal
		return r.Fatal(errors.New("database connection failed"))
	}))

    // 4. Handle request body parsing
    type CreateRequest struct { Name string `json:"name"` }
    r.Post("/users", renderer.Handler(func(r *beam.Renderer) error {
        var reqData CreateRequest
        // Use the built-in JSON parser
        if err := r.JSON(chi.RequestFromContext(r.ctx), &reqData); err != nil {
            // Send a 400 Bad Request for invalid input
            return r.Errorf("Invalid user data: %w", err)
        }
        return r.Msgf("User '%s' created!", reqData.Name)
    }))

	fmt.Println("Server listening on :4040")
	http.ListenAndServe(":4040", r)
}
```

## Contributing

Contributions are welcome! Please fork the repository, create a feature branch, and submit a pull request with clear descriptions and tests. See [CONTRIBUTING.md](CONTRIBUTING.md) for more details.

## License

Beam is licensed under the MIT License. See [LICENSE](LICENSE) for details.