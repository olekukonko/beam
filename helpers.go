package beam

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Msg sends a successful HTTP response with a simple message.
// It constructs a Response with StatusSuccessful and the provided message.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) Msg(msg string) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
	})
}

// Msgf sends a successful HTTP response with a formatted message.
// It formats the message using fmt.Sprintf and constructs a Response with StatusSuccessful.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) Msgf(format string, args ...interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: fmt.Sprintf(format, args...),
	})
}

// Send sends an HTTP response with an unknown status, message, and optional info.
// It constructs a Response with StatusUnknown, the provided message, and info data.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) Send(msg string, info interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.Push(r.writer, Response{
		Status:  StatusUnknown,
		Message: msg,
		Info:    info,
	})
}

// Info sends a successful HTTP response with a message and optional info data.
// It constructs a Response with StatusSuccessful, the provided message, and info.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) Info(msg string, info interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
		Info:    info,
	})
}

// Data sends a successful HTTP response with a message and optional data.
// It constructs a Response with StatusSuccessful, the provided message, and data.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) Data(msg string, data interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
		Data:    data,
	})
}

// Response sends a successful HTTP response with a message, optional info, and data.
// It constructs a Response with StatusSuccessful, the provided message, info, and data.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) Response(msg string, info interface{}, data interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
		Info:    info,
		Data:    data,
	})
}

// Pending sends a pending HTTP response with a message and optional info data.
// It constructs a Response with StatusPending and HTTP status 202 (Accepted).
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) Pending(msg string, info interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusAccepted).Push(r.writer, Response{
		Status:  StatusPending,
		Message: msg,
		Info:    info,
	})
}

// Titled sends a successful HTTP response with a title, message, and optional info.
// It constructs a Response with StatusSuccessful, the provided title, message, and info.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) Titled(title, msg string, info interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Title:   title,
		Message: msg,
		Info:    info,
	})
}

// Error sends an error HTTP response with a default message and optional errors.
// It constructs a Response with StatusError and filtered errors, if any.
// Skips sending if all errors are filtered and no custom message is intended.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) Error(errs ...error) error {
	return r.handleErrorResponse(defaultErrorMessage, false, nil, errs...)
}

// ErrorMsg sends an error HTTP response with a custom message and optional errors.
// It constructs a Response with StatusError, the provided message, and filtered errors.
// Skips sending if all errors are filtered and no custom message is intended.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) ErrorMsg(message string, errs ...error) error {
	return r.handleErrorResponse(message, false, nil, errs...)
}

// Errorf sends an error HTTP response with a formatted message and optional errors.
// It formats the message using fmt.Sprintf, filtering errors for the response.
// Skips sending if all errors are filtered and no custom message is intended.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) Errorf(format string, args ...interface{}) error {
	message := r.formatWithSpecial(format, args)
	errs := Any2Error(args...)
	return r.handleErrorResponse(message, false, nil, errs...)
}

// ErrorInfo sends an error HTTP response with a custom message, info data, and optional errors.
// It constructs a Response with StatusError, the provided message, info, and filtered errors.
// Skips sending if all errors are filtered and no custom message is intended.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) ErrorInfo(message string, info interface{}, errs ...error) error {
	return r.handleErrorResponse(message, false, info, errs...)
}

// Fatal sends a fatal error HTTP response with a default message and optional errors.
// It constructs a Response with StatusFatal and filtered errors, logging errors if a logger is present.
// Always sends the response, using HTTP status 500 (Internal Server Error).
// Returns an error if sending the response fails.
func (r *Renderer) Fatal(errs ...error) error {
	return r.handleErrorResponse(defaultFatalMessage, true, nil, errs...)
}

// FatalMsg sends a fatal error HTTP response with a custom message and optional errors.
// It constructs a Response with StatusFatal, the provided message, and filtered errors, logging errors if a logger is present.
// Always sends the response, using HTTP status 500 (Internal Server Error).
// Returns an error if sending the response fails.
func (r *Renderer) FatalMsg(message string, errs ...error) error {
	return r.handleErrorResponse(message, true, nil, errs...)
}

// Fatalf sends a fatal error HTTP response with a formatted message and optional errors.
// It formats the message using fmt.Sprintf and constructs a Response with StatusFatal and filtered errors, logging errors if a logger is present.
// Always sends the response, using HTTP status 500 (Internal Server Error).
// Returns an error if sending the response fails.
func (r *Renderer) Fatalf(format string, args ...interface{}) error {
	message := r.formatWithSpecial(format, args)
	errs := Any2Error(args...)
	return r.handleErrorResponse(message, true, nil, errs...)
}

// FatalInfo sends a fatal error HTTP response with a custom message, info data, and optional errors.
// It constructs a Response with StatusFatal, the provided message, info, and filtered errors, logging errors if a logger is present.
// Always sends the response, using HTTP status 500 (Internal Server Error).
// Returns an error if sending the response fails.
func (r *Renderer) FatalInfo(message string, info interface{}, errs ...error) error {
	return r.handleErrorResponse(message, true, info, errs...)
}

// handleErrorResponse processes and sends error-related HTTP responses.
// It handles both fatal and non-fatal errors, applying filters and determining the response status (StatusError or StatusFatal).
// For fatal responses, it logs errors with additional context if a logger is present.
// Returns an error if the writer is nil or sending the response fails.
func (r *Renderer) handleErrorResponse(message string, isInitiallyFatal bool, info interface{}, errs ...error) error {
	if r.writer == nil {
		return errNoWriter
	}

	responseErrors, fatalErrors, hasHidden := r.processErrors(isInitiallyFatal, errs...)
	finalErrors := append(responseErrors, fatalErrors...)
	isEffectivelyFatal := isInitiallyFatal || len(fatalErrors) > 0

	// Skip non-fatal responses with no errors, no hidden errors, and default or empty message
	if !isEffectivelyFatal && len(errs) > 0 && len(finalErrors) == 0 && !hasHidden && (message == "" || message == defaultErrorMessage) {
		return nil
	}

	// Set response status and message
	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusError
	resp.Message = message
	resp.Info = info // Set the info field
	if message == "" {
		resp.Message = defaultErrorMessage
	}
	if isEffectivelyFatal {
		resp.Status = StatusFatal
		if message == "" || message == defaultErrorMessage {
			resp.Message = defaultFatalMessage
		}
	}

	// Include errors in response if enabled
	if r.showError.Enabled() {
		resp.Errors = finalErrors
	}

	// Log fatal errors with context
	if isEffectivelyFatal && r.logger != nil {
		loggingErrors := r.filterErrorsForLogging(errs)
		var logErr error
		logFields := []interface{}{}
		file, line, funcName := getCallerInfo()
		logFields = append(logFields, fieldFile, file, fieldLine, line, fieldFunc, funcName)

		nilCount := 0
		for _, err := range errs {
			if err == nil {
				nilCount++
			}
		}
		filteredCount := len(errs) - len(loggingErrors) - nilCount + nilCount
		if len(loggingErrors) > 0 {
			logErr = loggingErrors[0]
			for i, err := range loggingErrors[1:] {
				logFields = append(logFields, fmt.Sprintf("error_%d", i+1), err)
			}
		} else {
			logErr = fmt.Errorf("%s (%d errors filtered)", resp.Message, filteredCount)
		}
		r.logger.Fatal(logErr, logFields...)
	}

	// Set HTTP status code
	statusCode := http.StatusBadRequest
	if isEffectivelyFatal {
		statusCode = http.StatusInternalServerError
	}

	return r.WithStatus(statusCode).Push(r.writer, *resp)
}

// processErrors filters and categorizes errors for response or logging.
// It applies error converters, identifies fatal and normal errors, and handles redacted or skipped errors.
// Returns response-ready errors, fatal errors, and a boolean indicating if any errors were hidden.
func (r *Renderer) processErrors(isCalledFromFatal bool, errs ...error) (responseErrors, fatalErrors []error, hasHidden bool) {
	for _, err := range errs {
		if err == nil {
			continue
		}

		convertedErr := r.errorFilters.applyConverters(err)

		var isFatal bool
		var fe fatalError
		var ne normalError
		if errors.As(convertedErr, &fe) {
			isFatal = true
		} else if errors.As(convertedErr, &ne) {
			isFatal = false
		} else {
			isFatal = isCalledFromFatal
		}

		if r.errorFilters.isSkipped(err) {
			continue
		}

		var processedErr error
		if r.errorFilters.isRedacted(err) {
			hasHidden = true
			processedErr = maskedError{original: err}
		} else {
			processedErr = errors.Unwrap(convertedErr)
			if processedErr == nil {
				processedErr = convertedErr
			}
		}

		if isFatal {
			fatalErrors = append(fatalErrors, processedErr)
		} else {
			responseErrors = append(responseErrors, processedErr)
		}
	}
	return
}

// formatWithSpecial formats a string with arguments, handling errors specially.
// It filters out skippable errors, redacts hidden errors with "*hidden*", and adjusts format verbs (e.g., %w to %v for errors).
// Returns the formatted string with processed arguments.
func (r *Renderer) formatWithSpecial(format string, args []interface{}) string {
	newFormat := &strings.Builder{}
	newArgs := []interface{}{}
	argIdx := 0

	p := 0
	for p < len(format) {
		idx := strings.Index(format[p:], "%")
		if idx == -1 {
			newFormat.WriteString(format[p:])
			break
		}
		newFormat.WriteString(format[p : p+idx])
		p += idx

		verbStart := p
		p++ // move past '%'

		if p < len(format) && format[p] == '%' {
			newFormat.WriteByte('%')
			p++
			continue
		}

		verbEnd := p
		for verbEnd < len(format) && strings.ContainsAny(string(format[verbEnd]), "#+- 0123456789.") {
			verbEnd++
		}
		if verbEnd >= len(format) {
			newFormat.WriteString(format[verbStart:])
			break
		}
		verbEnd++ // include the verb char

		if argIdx < len(args) {
			arg := args[argIdx]
			if err, ok := arg.(error); ok {
				if r.errorFilters.isSkipped(err) {
					// Skip the format verb and argument
				} else if r.errorFilters.isRedacted(err) {
					newFormat.WriteString("%s")
					newArgs = append(newArgs, "*hidden*")
				} else {
					verb := format[verbStart:verbEnd]
					if verb == "%w" {
						newFormat.WriteString("%v")
					} else {
						newFormat.WriteString(verb)
					}
					newArgs = append(newArgs, err)
				}
			} else {
				newFormat.WriteString(format[verbStart:verbEnd])
				newArgs = append(newArgs, arg)
			}
			argIdx++
		} else {
			newFormat.WriteString(format[verbStart:verbEnd])
		}
		p = verbEnd
	}

	return fmt.Sprintf(newFormat.String(), newArgs...)
}

// filterErrorsForLogging filters errors for logging purposes.
// It excludes skippable and redacted errors, returning only raw errors suitable for logging.
func (r *Renderer) filterErrorsForLogging(errs []error) []error {
	var filtered []error
	for _, err := range errs {
		if err == nil {
			continue
		}
		if r.errorFilters.isSkipped(err) || r.errorFilters.isRedacted(err) {
			continue
		}
		filtered = append(filtered, err)
	}
	return filtered
}
