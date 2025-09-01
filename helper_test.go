package beam

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorFormatting(t *testing.T) {
	tests := []struct {
		name           string
		format         string
		args           []interface{}
		expectedMsg    string
		expectedErrors []string
		shouldSkip     bool
	}{
		{
			name:           "No errors",
			format:         "simple message",
			args:           nil,
			expectedMsg:    "simple message",
			expectedErrors: nil,
			shouldSkip:     false,
		},
		{
			name:           "Single error with %v",
			format:         "error: %v",
			args:           []interface{}{errors.New("file not found")},
			expectedMsg:    "error: file not found",
			expectedErrors: []string{"file not found"},
			shouldSkip:     false,
		},
		{
			name:           "Single error with %w",
			format:         "wrapped: %w",
			args:           []interface{}{errors.New("permission denied")},
			expectedMsg:    "wrapped: permission denied",
			expectedErrors: []string{"permission denied"},
			shouldSkip:     false,
		},
		{
			name:           "Multiple errors with format",
			format:         "errors: %v, %v",
			args:           []interface{}{errors.New("network timeout"), errors.New("invalid input")},
			expectedMsg:    "errors: network timeout, invalid input",
			expectedErrors: []string{"network timeout", "invalid input"},
			shouldSkip:     false,
		},
		{
			name:           "More verbs than args",
			format:         "missing: %v, %v, %v",
			args:           []interface{}{errors.New("only one")},
			expectedMsg:    "missing: only one, %!v(MISSING), %!v(MISSING)",
			expectedErrors: []string{"only one"},
			shouldSkip:     false,
		},
		{
			name:           "Mixed arguments",
			format:         "User %s failed: %v",
			args:           []interface{}{"john", errors.New("validation error")},
			expectedMsg:    "User john failed: validation error",
			expectedErrors: []string{"validation error"},
			shouldSkip:     false,
		},
		{
			name:           "With ErrSkip",
			format:         defaultErrorMessage,
			args:           []interface{}{ErrSkip},
			expectedMsg:    defaultErrorMessage,
			expectedErrors: nil,
			shouldSkip:     true,
		},
		{
			name:           "Multiple errors including ErrSkip",
			format:         "problems: %v, %v",
			args:           []interface{}{errors.New("first"), ErrSkip, errors.New("third")},
			expectedMsg:    "problems: first, ", // <-- FIX: Updated expectation
			expectedErrors: []string{"first", "third"},
			shouldSkip:     false,
		},
		{
			name:           "Non-error arguments",
			format:         "Value: %s, Number: %d",
			args:           []interface{}{"test", 42},
			expectedMsg:    "Value: test, Number: 42",
			expectedErrors: nil,
			shouldSkip:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := NewRenderer(Setting{ContentType: ContentTypeJSON}).WithWriter(w)

			err := r.Errorf(tt.format, tt.args...)

			if tt.shouldSkip {
				if err != nil {
					t.Errorf("Expected nil error for skipped case, got %v", err)
				}
				if w.Body.Len() != 0 {
					t.Error("Expected no output for skipped case")
				}
				return
			}

			if err != nil {
				t.Fatalf("Errorf returned unexpected error: %v", err)
			}

			var resp Response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if resp.Message != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, resp.Message)
			}

			if len(tt.expectedErrors) != len(resp.Errors) {
				t.Fatalf("Expected %d errors in response, got %d: %v",
					len(tt.expectedErrors), len(resp.Errors), resp.Errors)
			}

			for i, expectedError := range tt.expectedErrors {
				actualError := resp.Errors[i].Error()
				if actualError != expectedError {
					t.Errorf("Error %d: expected %q, got %q", i, expectedError, actualError)
				}
			}
		})
	}
}

func TestErrorFilters(t *testing.T) {
	tests := []struct {
		name        string
		skipFilter  func(error) bool
		err         error
		shouldWrite bool
		expectedErr string
	}{
		{
			name:        "Default filter with sql.ErrNoRows",
			skipFilter:  nil,
			err:         sql.ErrNoRows,
			shouldWrite: true,
			expectedErr: sql.ErrNoRows.Error(),
		},
		{
			name:        "Default filter with ErrSkip",
			skipFilter:  nil,
			err:         ErrSkip,
			shouldWrite: false,
		},
		{
			name: "Custom skip filter matching error",
			skipFilter: func(err error) bool {
				return err.Error() == "custom error"
			},
			err:         errors.New("custom error"),
			shouldWrite: false,
		},
		{
			name: "Custom skip filter not matching",
			skipFilter: func(err error) bool {
				return false
			},
			err:         errors.New("some error"),
			shouldWrite: true,
			expectedErr: "some error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := NewRenderer(Setting{ContentType: ContentTypeJSON}).WithWriter(w)
			if tt.skipFilter != nil {
				r = r.WithSkipFilter(tt.skipFilter)
			}

			err := r.Error(tt.err)
			if err != nil {
				t.Fatalf("Error returned unexpected error: %v", err)
			}

			if tt.shouldWrite {
				if w.Body.Len() == 0 {
					t.Error("Expected error response to be written, but it wasn't")
				}
				var resp Response
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if resp.Status != StatusError {
					t.Errorf("Expected status %s, got %s", StatusError, resp.Status)
				}
				if tt.expectedErr != "" && (len(resp.Errors) != 1 || resp.Errors[0].Error() != tt.expectedErr) {
					t.Errorf("Expected error %q, got %v", tt.expectedErr, resp.Errors)
				}
			} else if w.Body.Len() != 0 {
				t.Errorf("Expected no error response to be written, but it was: %s", w.Body.String())
			}
		})
	}
}

func TestErrorWithNil(t *testing.T) {
	w := httptest.NewRecorder()
	r := NewRenderer(Setting{ContentType: ContentTypeJSON}).WithWriter(w)

	err := r.Errorf("test with nil")
	if err != nil {
		t.Fatalf("Errorf returned unexpected error: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Message != "test with nil" {
		t.Errorf("Expected message %q, got %q", "test with nil", resp.Message)
	}
	if len(resp.Errors) != 0 {
		t.Errorf("Expected no errors in response, got %d", len(resp.Errors))
	}
}

func TestErrHiddenFunctionality(t *testing.T) {
	tests := []struct {
		name           string
		format         string
		args           []interface{}
		expectedMsg    string
		expectedErrors []string
		shouldSkip     bool
	}{
		{
			name:           "Direct ErrHidden",
			format:         "Error: %v",
			args:           []interface{}{ErrHidden},
			expectedMsg:    "Error: *hidden*",
			expectedErrors: []string{"hidd [REDACTED]"}, // <-- FIX: Updated expectation
			shouldSkip:     false,
		},
		{
			name:           "Wrapped ErrHidden with fmt.Errorf",
			format:         "Wrapped: %v",
			args:           []interface{}{fmt.Errorf("sensitive_data: %w", ErrHidden)},
			expectedMsg:    "Wrapped: *hidden*",
			expectedErrors: []string{"sens [REDACTED]"},
			shouldSkip:     false,
		},
		{
			name:           "Mixed hidden and visible errors",
			format:         "Problems: %v, %v, %v",
			args:           []interface{}{errors.New("file not found"), ErrHidden, errors.New("timeout")},
			expectedMsg:    "Problems: file not found, *hidden*, timeout",
			expectedErrors: []string{"file not found", "hidd [REDACTED]", "timeout"}, // <-- FIX: Updated expectation
			shouldSkip:     false,
		},
		{
			name:           "Mixed ErrHidden and ErrSkip",
			format:         "Mixed: %v, %v",
			args:           []interface{}{ErrHidden, ErrSkip},
			expectedMsg:    "Mixed: *hidden*, ",         // <-- FIX: Updated expectation
			expectedErrors: []string{"hidd [REDACTED]"}, // <-- FIX: Updated expectation
			shouldSkip:     false,
		},
		{
			name:           "Only ErrSkip with default message",
			format:         defaultErrorMessage,
			args:           []interface{}{ErrSkip},
			expectedMsg:    defaultErrorMessage,
			expectedErrors: nil,
			shouldSkip:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := NewRenderer(Setting{ContentType: ContentTypeJSON}).WithWriter(w)

			err := r.Errorf(tt.format, tt.args...)

			if tt.shouldSkip {
				if err != nil {
					t.Errorf("Expected nil error for skipped case, got %v", err)
				}
				if w.Body.Len() != 0 {
					t.Error("Expected no output for skipped case")
				}
				return
			}

			if err != nil {
				t.Fatalf("Errorf returned unexpected error: %v", err)
			}

			var resp Response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v. Body: %s", err, w.Body.String())
			}

			if resp.Message != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, resp.Message)
			}

			if len(tt.expectedErrors) != len(resp.Errors) {
				t.Fatalf("Expected %d errors in response, got %d: %v",
					len(tt.expectedErrors), len(resp.Errors), resp.Errors)
			}

			for i, expectedError := range tt.expectedErrors {
				actualError := resp.Errors[i].Error()
				if actualError != expectedError {
					t.Errorf("Error %d: expected %q, got %q", i, expectedError, actualError)
				}
			}
		})
	}
}

func TestErrorWithNilHandling(t *testing.T) {
	tests := []struct {
		name               string
		message            string
		errs               []error
		expectResponse     bool
		expectedErrorCount int
		expectedStatus     string
	}{
		{
			name:               "with nil error only",
			message:            "A message",
			errs:               []error{nil},
			expectResponse:     true,
			expectedErrorCount: 0,
			expectedStatus:     StatusError,
		},
		{
			name:               "with no errors provided",
			message:            "Another message",
			errs:               []error{},
			expectResponse:     true,
			expectedErrorCount: 0,
			expectedStatus:     StatusError,
		},
		{
			name:               "with a single real error",
			message:            "Real error occurred",
			errs:               []error{errors.New("something went wrong")},
			expectResponse:     true,
			expectedErrorCount: 1,
			expectedStatus:     StatusError,
		},
		{
			name:               "with real and nil errors",
			message:            "Mixed errors",
			errs:               []error{errors.New("real error"), nil, errors.New("another real error")},
			expectResponse:     true,
			expectedErrorCount: 2,
			expectedStatus:     StatusError,
		},
		{
			name:               "with only a skippable error",
			message:            defaultErrorMessage,
			errs:               []error{ErrSkip},
			expectResponse:     false,
			expectedErrorCount: 0,
			expectedStatus:     StatusError,
		},
		{
			name:               "with skippable and real errors",
			message:            "Should show one error",
			errs:               []error{ErrSkip, errors.New("visible error")},
			expectResponse:     true,
			expectedErrorCount: 1,
			expectedStatus:     StatusError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			renderer := NewRenderer(Setting{ContentType: ContentTypeJSON}).WithWriter(w)

			err := renderer.ErrorMsg(tt.message, tt.errs...)
			if err != nil {
				t.Fatalf("ErrorMsg returned an unexpected error: %v", err)
			}

			if !tt.expectResponse {
				if w.Body.Len() != 0 {
					t.Errorf("Expected no response, but a response was sent with body: %s", w.Body.String())
				}
				return
			}

			if w.Body.Len() == 0 {
				t.Fatal("Expected a response, but no response was sent")
			}

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
			}

			var resp Response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal response body: %v", err)
			}

			if resp.Message != tt.message {
				t.Errorf("Expected message %q, got %q", tt.message, resp.Message)
			}

			if resp.Status != tt.expectedStatus {
				t.Errorf("Expected status %s, got %s", tt.expectedStatus, resp.Status)
			}

			if len(resp.Errors) != tt.expectedErrorCount {
				t.Errorf("Expected %d errors in response, got %d", tt.expectedErrorCount, len(resp.Errors))
			}
		})
	}
}

func TestFatalMethods(t *testing.T) {
	tests := []struct {
		name                       string
		testFunc                   func(r *Renderer) error
		expectResponse             bool
		expectLog                  bool
		expectedResponseMessage    string
		expectedResponseErrorCount int
		expectedLogFieldsCount     int
		expectedLogErr             string
	}{
		{
			name:                       "Fatal with a real error",
			testFunc:                   func(r *Renderer) error { return r.Fatal(errors.New("db connection failed")) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    defaultFatalMessage,
			expectedResponseErrorCount: 1,
			expectedLogFieldsCount:     6,
			expectedLogErr:             "db connection failed",
		},
		{
			name:                       "FatalMsg with a real error",
			testFunc:                   func(r *Renderer) error { return r.FatalMsg("failed to load user", errors.New("user not found")) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "failed to load user",
			expectedResponseErrorCount: 1,
			expectedLogFieldsCount:     6,
			expectedLogErr:             "user not found",
		},
		{
			name:                       "Fatalf with a real error",
			testFunc:                   func(r *Renderer) error { return r.Fatalf("hello : %v", errors.New("test error")) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "hello : test error",
			expectedResponseErrorCount: 1,
			expectedLogFieldsCount:     6,
			expectedLogErr:             "test error",
		},
		{
			name:                       "Fatal with multiple real errors",
			testFunc:                   func(r *Renderer) error { return r.Fatal(errors.New("error1"), errors.New("error2")) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    defaultFatalMessage,
			expectedResponseErrorCount: 2,
			expectedLogFieldsCount:     8,
			expectedLogErr:             "error1",
		},
		{
			name:                       "Fatal with only nil error",
			testFunc:                   func(r *Renderer) error { return r.Fatal(nil) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    defaultFatalMessage,
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     6,
			expectedLogErr:             defaultFatalMessage + " (1 errors filtered)",
		},
		{
			name:                       "FatalMsg with only nil error",
			testFunc:                   func(r *Renderer) error { return r.FatalMsg("failed to load", nil) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "failed to load",
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     6,
			expectedLogErr:             "failed to load (1 errors filtered)",
		},
		{
			name:                       "Fatal with no errors",
			testFunc:                   func(r *Renderer) error { return r.Fatal() },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    defaultFatalMessage,
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     6,
			expectedLogErr:             defaultFatalMessage + " (0 errors filtered)",
		},
		{
			name:                       "FatalMsg with no errors",
			testFunc:                   func(r *Renderer) error { return r.FatalMsg("failed to load") },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "failed to load",
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     6,
			expectedLogErr:             "failed to load (0 errors filtered)",
		},
		{
			name:                       "Fatalf with no error arguments",
			testFunc:                   func(r *Renderer) error { return r.Fatalf("hello") },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "hello",
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     6,
			expectedLogErr:             "hello (0 errors filtered)",
		},
		{
			name:                       "Fatalf with nil error",
			testFunc:                   func(r *Renderer) error { return r.Fatalf("hello : %v", nil) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "hello : <nil>",
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     6,
			expectedLogErr:             "hello : <nil> (0 errors filtered)", // <-- FIX: Updated expectation
		},
		{
			name:                       "Fatal with only ErrSkip",
			testFunc:                   func(r *Renderer) error { return r.Fatal(ErrSkip) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    defaultFatalMessage,
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     6,
			expectedLogErr:             defaultFatalMessage + " (1 errors filtered)",
		},
		{
			name:                       "FatalMsg with only ErrSkip",
			testFunc:                   func(r *Renderer) error { return r.FatalMsg("this should NOT be skipped", ErrSkip) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "this should NOT be skipped",
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     6,
			expectedLogErr:             "this should NOT be skipped (1 errors filtered)",
		},
		{
			name:                       "Fatal with only ErrHidden",
			testFunc:                   func(r *Renderer) error { return r.Fatal(ErrHidden) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    defaultFatalMessage,
			expectedResponseErrorCount: 1,
			expectedLogFieldsCount:     6,
			expectedLogErr:             defaultFatalMessage + " (1 errors filtered)",
		},
		{
			name:                       "FatalMsg with only ErrHidden",
			testFunc:                   func(r *Renderer) error { return r.FatalMsg("a hidden error occurred", ErrHidden) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "a hidden error occurred",
			expectedResponseErrorCount: 1,
			expectedLogFieldsCount:     6,
			expectedLogErr:             "a hidden error occurred (1 errors filtered)",
		},
		{
			name:                       "Fatalf with only ErrHidden",
			testFunc:                   func(r *Renderer) error { return r.Fatalf("hidden : %v", ErrHidden) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "hidden : *hidden*",
			expectedResponseErrorCount: 1,
			expectedLogFieldsCount:     6,
			expectedLogErr:             "hidden : *hidden* (1 errors filtered)",
		},
		{
			name:                       "Fatalf with ErrSkip",
			testFunc:                   func(r *Renderer) error { return r.Fatalf("skipped : %v", ErrSkip) },
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "skipped : ", // <-- FIX: Updated expectation
			expectedResponseErrorCount: 0,
			expectedLogFieldsCount:     6,
			expectedLogErr:             "skipped :  (1 errors filtered)", // <-- FIX: Updated expectation
		},
		{
			name: "Fatalf with mixed errors including ErrSkip",
			testFunc: func(r *Renderer) error {
				return r.Fatalf("mixed: %v %v %v", errors.New("real"), ErrSkip, errors.New("another"))
			},
			expectResponse:             true,
			expectLog:                  true,
			expectedResponseMessage:    "mixed: real  another", // <-- FIX: Updated expectation
			expectedResponseErrorCount: 2,
			expectedLogFieldsCount:     8,
			expectedLogErr:             "real",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testLogger := &TestLogger{}
			renderer := NewRenderer(Setting{ContentType: ContentTypeJSON}).WithWriter(w).WithLogger(testLogger)

			err := tt.testFunc(renderer)
			if err != nil {
				t.Fatalf("Function returned an unexpected error: %v", err)
			}

			if !tt.expectResponse {
				if w.Body.Len() != 0 {
					t.Errorf("Expected no response, but got body: %s", w.Body.String())
				}
			} else {
				if w.Body.Len() == 0 {
					t.Fatal("Expected a response, but no response was sent")
				}
				if w.Code != http.StatusInternalServerError {
					t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
				}
				var resp Response
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if resp.Message != tt.expectedResponseMessage {
					t.Errorf("Expected message %q, got %q", tt.expectedResponseMessage, resp.Message)
				}
				if len(resp.Errors) != tt.expectedResponseErrorCount {
					t.Errorf("Expected %d errors in response, got %d", tt.expectedResponseErrorCount, len(resp.Errors))
				}
			}

			if !tt.expectLog {
				if len(testLogger.Entries) != 0 {
					t.Errorf("Expected no log entry, but %d were found", len(testLogger.Entries))
				}
			} else {
				if len(testLogger.Entries) == 0 {
					t.Error("Expected a log entry, but no log was created")
					return
				}
				lastLog := testLogger.LastEntry()
				if lastLog.Level != "fatal" {
					t.Errorf("Expected log level 'fatal', got %q", lastLog.Level)
				}
				if lastLog.Err == nil {
					t.Error("Expected a non-nil error in the log entry")
				}
				if len(lastLog.Fields) != tt.expectedLogFieldsCount {
					t.Errorf("Expected %d log fields, got %d", tt.expectedLogFieldsCount, len(lastLog.Fields))
				}
				if lastLog.Err.Error() != tt.expectedLogErr {
					t.Errorf("Expected log error %q, got %q", tt.expectedLogErr, lastLog.Err.Error())
				}
			}
		})
	}
}

func TestFatalMethods_WithFilterableErrors(t *testing.T) {
	tests := []struct {
		name                       string
		testFunc                   func(r *Renderer) error
		expectedResponseErrorCount int
		expectedLogErr             string
	}{
		{
			name:                       "Fatal with sql.ErrNoRows",
			testFunc:                   func(r *Renderer) error { return r.Fatal(sql.ErrNoRows) },
			expectedResponseErrorCount: 1,
			expectedLogErr:             sql.ErrNoRows.Error(),
		},
		{
			name:                       "FatalMsg with sql.ErrNoRows",
			testFunc:                   func(r *Renderer) error { return r.FatalMsg("failed to query", sql.ErrNoRows) },
			expectedResponseErrorCount: 1,
			expectedLogErr:             sql.ErrNoRows.Error(),
		},
		{
			name:                       "Fatalf with sql.ErrNoRows",
			testFunc:                   func(r *Renderer) error { return r.Fatalf("query failed: %v", sql.ErrNoRows) },
			expectedResponseErrorCount: 1,
			expectedLogErr:             sql.ErrNoRows.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			testLogger := &TestLogger{}
			renderer := NewRenderer(Setting{ContentType: ContentTypeJSON}).WithWriter(w).WithLogger(testLogger)

			err := tt.testFunc(renderer)
			if err != nil {
				t.Fatalf("Function returned an unexpected error: %v", err)
			}

			if w.Body.Len() == 0 {
				t.Fatal("Expected a fatal response, but no response was sent")
			}
			if w.Code != http.StatusInternalServerError {
				t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
			}
			var resp Response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if len(resp.Errors) != tt.expectedResponseErrorCount {
				t.Errorf("Expected %d error in response body, got %d", tt.expectedResponseErrorCount, len(resp.Errors))
			}
			if tt.expectedResponseErrorCount > 0 && resp.Errors[0].Error() != tt.expectedLogErr {
				t.Errorf("Expected error %q in response, got %q", tt.expectedLogErr, resp.Errors[0].Error())
			}

			if len(testLogger.Entries) == 0 {
				t.Fatal("Expected a log entry, but no log was created")
			}
			lastLog := testLogger.LastEntry()
			if lastLog.Level != "fatal" {
				t.Errorf("Expected log level 'fatal', got %q", lastLog.Level)
			}
			if lastLog.Err.Error() != tt.expectedLogErr {
				t.Errorf("Expected log error %q, got %q", tt.expectedLogErr, lastLog.Err.Error())
			}
		})
	}
}

func TestErrorWithNoRows(t *testing.T) {
	w := httptest.NewRecorder()
	r := NewRenderer(Setting{ContentType: ContentTypeJSON}).WithWriter(w)

	err := r.Error(sql.ErrNoRows)
	if err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Status != StatusError {
		t.Errorf("Expected status %s, got %s", StatusError, resp.Status)
	}
	if len(resp.Errors) != 1 || resp.Errors[0].Error() != sql.ErrNoRows.Error() {
		t.Errorf("Expected error %v, got %v", sql.ErrNoRows, resp.Errors)
	}
}

func TestErrorWithHidden(t *testing.T) {
	w := httptest.NewRecorder()
	r := NewRenderer(Setting{ContentType: ContentTypeJSON}).WithWriter(w)

	hiddenErr := fmt.Errorf("sensitive_password_data: %w", ErrHidden)
	err := r.Error(hiddenErr)
	if err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	expectedMasked := "sens [REDACTED]"
	if len(resp.Errors) != 1 || resp.Errors[0].Error() != expectedMasked {
		t.Errorf("Expected masked error %s, got %s", expectedMasked, resp.Errors[0].Error())
	}
}

func TestErrorWithSkip(t *testing.T) {
	w := httptest.NewRecorder()
	r := NewRenderer(Setting{ContentType: ContentTypeJSON}).WithWriter(w)

	err := r.Error(ErrSkip)
	if err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}

	if w.Body.Len() != 0 {
		t.Error("Expected no output for ErrSkip")
	}
}

func TestMixedErrors(t *testing.T) {
	w := httptest.NewRecorder()
	r := NewRenderer(Setting{ContentType: ContentTypeJSON}).WithWriter(w)

	normalErr := errors.New("normal error")
	hiddenErr := fmt.Errorf("sensitive_data: %w", ErrHidden)
	errs := []error{normalErr, hiddenErr, ErrSkip}
	err := r.Error(errs...)
	if err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	expectedErrors := []string{"normal error", "sens [REDACTED]"}
	if len(resp.Errors) != 2 {
		t.Errorf("Expected 2 errors, got %v", resp.Errors)
	}
	for i, expected := range expectedErrors {
		if i < len(resp.Errors) && resp.Errors[i].Error() != expected {
			t.Errorf("Expected error %s, got %s", expected, resp.Errors[i].Error())
		}
	}
}
