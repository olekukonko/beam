## Migration Guide: What's New?

The latest version of Beam introduces a completely redesigned, more powerful, and granular error handling system, along with several new features to enhance API responses and improve the developer experience. While this update adds significant capabilities, it also includes several breaking changes. This guide will help you upgrade your code.

### 💥 Breaking Changes 💥

These are changes that will likely require you to modify your existing code to compile and work correctly.

#### 1. Complete Overhaul of Error Handling API

The previous methods for sending error responses (`Error`, `Warning`, `Fatal`) have been replaced with a more flexible and expressive API.

*   **OLD (`renderer.go`):**
    ```go
    // Simple methods with less flexibility
    func (r *Renderer) Error(format string, errs ...error) error
    func (r *Renderer) Warning(errs ...error) error
    func (r *Renderer) Fatal(errs ...error) error
    ```

*   **NEW (`helpers.go`):**
    The new API provides separate methods for default messages, custom messages, and formatted messages.
    ```go
    // For standard, non-fatal errors
    func (r *Renderer) Error(errs ...error) error
    func (r *Renderer) ErrorMsg(message string, errs ...error) error
    func (r *Renderer) Errorf(format string, args ...interface{}) error

    // For fatal errors that will be logged with severity
    func (r *Renderer) Fatal(errs ...error) error
    func (r *Renderer) FatalMsg(message string, errs ...error) error
    func (r *Renderer) Fatalf(format string, args ...interface{}) error
    ```

    **Migration:**
    *   If you used `r.Error("some context: %v", err)`, you should now use `r.Errorf("some context: %v", err)`.
    *   If you used `r.Fatal(err1, err2)`, the call remains the same: `r.Fatal(err1, err2)`.

#### 2. Advanced Error Filtering System

The simple error filter slice has been replaced by a structured `ErrorFilterSet`, allowing for more advanced error processing.

*   **OLD (`renderer.go`):**
    ```go
    // A simple slice of functions that would cause the response to be skipped.
    r.errorFilters []func(error) bool
    r.WithErrorFilters(...)
    ```

*   **NEW (`beam.go`, `renderer.go`):**
    A new `ErrorFilterSet` struct now supports three distinct actions:
    *   `Skip`: Omit an error from non-fatal responses entirely.
    *   `Redact`: Mask the error's message in the response (e.g., for sensitive data).
    *   `Convert`: Transform an error, for example, to change its severity from fatal to non-fatal.
    ```go
    // New configuration methods
    r.WithSkipFilter(...)
    r.WithRedactFilter(...)
    r.WithConvertFilter(...)
    ```

    **Migration:**
    Your old `WithErrorFilters` calls should be changed to `WithSkipFilter`.

#### 3. `Response.Data` Field Type Change

To provide more flexibility, the `Data` field in the `Response` struct is no longer a slice.

*   **OLD (`types.go`):** `Data []interface{}`
*   **NEW (`types.go`):** `Data interface{}`

    **Migration:**
    This allows you to pass a single struct, a map, or a slice directly without wrapping it.
    *   **Old:** `r.Data("users found", []interface{}{user1, user2})`
    *   **New:** `r.Data("users found", []User{user1, user2})` or `r.Data("user found", user1)`

#### 4. New `Logger` Interface

The `Logger` interface has been updated to support structured, leveled logging.

*   **OLD (`types.go`):**
    ```go
    type Logger interface {
        Log(err error) bool
    }
    ```
*   **NEW (`types.go`):**
    ```go
    type Logger interface {
        Error(err error, fields ...interface{})
        Fatal(err error, fields ...interface{})
    }
    ```
    **Migration:**
    If you have implemented a custom logger, you must update its method signatures to match the new interface. The `fields` parameter allows you to pass key-value pairs for structured logging (e.g., `fieldRequestID`, `"xyz-123"`).

---

### ✨ New Features & Enhancements ✨

This version is packed with new capabilities to make your life easier.

#### 1. Error Redaction (`ErrHidden`)

You can now wrap errors with `beam.ErrHidden` to automatically redact their details in the API response, preventing sensitive information from being exposed. The raw error is still available for logging.

```go
sensitiveErr := errors.New("password_is_incorrect")
// This will show "pass [REDACTED]" in the JSON response
// but will log the full error if a logger is configured.
r.FatalMsg("Authentication failed", fmt.Errorf("%w: %w", anError, beam.ErrHidden))
```

#### 2. Dynamic Error Severity (`ToFatal`, `ToNormal`)

You can now dynamically change how an error is treated. The default error filters automatically convert `sql.ErrNoRows` to a normal, non-fatal error, even when passed to `r.Fatal()`. You can add your own rules or use the helpers directly.

```go
// Mark an error to always be handled as fatal.
r.Error(beam.ToFatal(someError))

// Mark an error to be handled as non-fatal, even inside r.Fatal().
r.Fatal(beam.ToNormal(someError))
```

#### 3. Structured Fatal Logging with Caller Info

When you call `r.Fatal()`, `r.Fatalf()`, or `r.FatalMsg()`, the logger (if configured) will now automatically be populated with the **file, line number, and function name** of where you made the call, dramatically speeding up debugging.

#### 4. Response Actions (HATEOAS)

The `Response` struct now includes an `Actions` slice, allowing you to build APIs that follow HATEOAS principles by telling the client what they can do next.

```go
// In types.go
type Action struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description,omitempty"`
    Method      string                 `json:"method,omitempty"`
    Href        string                 `json:"href,omitempty"`
    // ... and more
}

// In your handler
r.WithAction(beam.Action{
    Name:   "get-user-details",
    Method: "GET",
    Href:   "/api/users/123",
}).Info("User created", nil)
```

#### 5. Request Body Parsing Helpers

The `Renderer` now includes convenient helpers for parsing incoming request bodies, powered by the `hauler` package.

```go
var user User
err := r.JSON(req, &user) // Decodes JSON body into the user struct
if err != nil {
    r.ErrorMsg("Invalid request", err)
    return
}
```

#### 6. New Statuses and Warning Methods

The library now includes `StatusWarning` and `StatusUnknown` constants, along with new convenience methods `r.Warning()` and `r.Warningf()` for responses that are not successful but are not necessarily critical errors.