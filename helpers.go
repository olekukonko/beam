package beam

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Msg sends a successful response with a message.
// Sends a Response with StatusSuccessful and the provided message.
// Returns an error if the writer is unset or sending fails.
func (r *Renderer) Msg(msg string) error {
	if r.writer == nil {
		return errNoWriter
	}
	return r.WithStatus(http.StatusOK).Push(r.writer, Response{
		Status:  StatusSuccessful,
		Message: msg,
	})
}

// Info sends a successful response with a message and info data.
// Sends a Response with StatusSuccessful, message, and optional info.
// Returns an error if the writer is unset or sending fails.
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

// Data sends a successful response with a message and data items.
// Sends a Response with StatusSuccessful, message, and data slice.
// Returns an error if the writer is unset or sending fails.
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

// Response sends a successful response with message, info, and data.
// Sends a Response with StatusSuccessful, message, info, and data.
// Returns an error if the writer is unset or sending fails.
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

// Unknown sends a pending response with a message and info data.
// Sends a Response with StatusPending and the provided message/info.
// Returns an error if the writer is unset or sending fails.
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

// Titled sends a successful response with a title, message, and info.
// Sends a Response with StatusSuccessful, title, message, and info.
// Returns an error if the writer is unset or sending fails.
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

// Error sends an error response with a default summary message.
// Sends a Response with StatusError and filtered errors, if any.
// Returns an error if the writer is unset or sending fails; skips if all errors filtered.
func (r *Renderer) Error(errs ...error) error {
	if r.writer == nil {
		return errNoWriter
	}

	// Logic for non-fatal errors: determine if a response should be skipped.
	hasNonNilError, containsHidden := checkErrors(errs...)
	filteredErrs := r.filterErrors(errs)

	// The correct skip condition for Error methods:
	// Skip if there *was* an error, but all of them were filtered, and none were hidden.
	if hasNonNilError && len(filteredErrs) == 0 && !containsHidden {
		return nil
	}

	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusError
	if r.showError.Enabled() {
		resp.Errors = filteredErrs
	}
	resp.Message = defaultErrorMessage

	return r.WithStatus(http.StatusBadRequest).Push(r.writer, *resp)
}

// ErrorWith sends an error response with a custom message and errors.
// Sends a Response with StatusError, custom message, and filtered errors.
// Returns an error if the writer is unset or sending fails; skips if all errors filtered.
func (r *Renderer) ErrorWith(message string, errs ...error) error {
	if r.writer == nil {
		return errNoWriter
	}

	hasNonNilError, containsHidden := checkErrors(errs...)
	filteredErrs := r.filterErrors(errs)

	// The correct skip condition for Error methods.
	if hasNonNilError && len(filteredErrs) == 0 && !containsHidden {
		return nil
	}

	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusError
	if r.showError.Enabled() {
		resp.Errors = filteredErrs
	}
	resp.Message = message

	return r.WithStatus(http.StatusBadRequest).Push(r.writer, *resp)
}

// Errorf sends an error response with a formatted message and errors.
// Formats the message with provided args, filtering errors for the response.
// Returns an error if the writer is unset or sending fails; skips if all errors filtered.
func (r *Renderer) Errorf(format string, args ...interface{}) error {
	if r.writer == nil {
		return errNoWriter
	}
	allErrorsFromArgs := Any2Error(args...)

	_, containsHidden := checkErrors(allErrorsFromArgs...)
	jsonErrorList := r.filterErrors(allErrorsFromArgs)

	// The correct skip condition for Error methods.
	if len(allErrorsFromArgs) > 0 && len(jsonErrorList) == 0 && !containsHidden {
		return nil
	}

	verbCount := strings.Count(format, "%") - (strings.Count(format, "%%") * 2)
	var messageFormatArgs []interface{}
	argsConsumed := 0
	for i := 0; i < verbCount && argsConsumed < len(args); {
		arg := args[argsConsumed]
		argsConsumed++
		err, isErr := arg.(error)
		if !isErr {
			messageFormatArgs = append(messageFormatArgs, arg)
			i++
			continue
		}
		isSkippable := false
		// We use r.isSkippable here only to format the message string correctly,
		// not to decide whether to skip the entire response.
		if r.isSkippable(err) {
			isSkippable = true
		}

		if errors.Is(err, ErrHidden) {
			messageFormatArgs = append(messageFormatArgs, "*hidden*")
		} else if isSkippable {
			// This is for the message only, don't append the error itself
		} else {
			messageFormatArgs = append(messageFormatArgs, err)
		}
		i++
	}
	resp := getResponse()
	defer putResponse(resp)
	resp.Status = StatusError

	if r.showError.Enabled() {
		resp.Errors = jsonErrorList
	}

	format = strings.ReplaceAll(format, "%w", "%v")
	// Custom formatting to handle missing args due to filtered errors
	actualFormat := r.formatWithSpecial(format, args)
	resp.Message = actualFormat
	return r.WithStatus(http.StatusBadRequest).Push(r.writer, *resp)
}

func (r *Renderer) Fatal(errs ...error) error {
	message := defaultFatalMessage
	// 'filtered' is for logging and skip logic ONLY.
	filtered := r.filterErrors(errs)

	hadSkippable := false
	hadHidden := false
	for _, e := range errs {
		if errors.Is(e, ErrSkip) {
			hadSkippable = true
		}
		if errors.Is(e, ErrHidden) {
			hadHidden = true
		}
	}
	if len(filtered) == 0 && hadSkippable && !hadHidden {
		return nil
	}
	var logErr error
	var logFields []interface{}
	if len(filtered) > 0 {
		logErr = filtered[0]
		logFields = make([]interface{}, len(filtered)-1)
		for i := 1; i < len(filtered); i++ {
			logFields[i-1] = filtered[i]
		}
	} else {
		logErr = errors.New(message)
	}
	if r.logger != nil {
		r.logger.Fatal(logErr, logFields...)
	}

	resp := Response{
		Status:  StatusFatal,
		Message: message,
		Errors:  prepareFatalResponseErrors(errs),
	}

	if r.showError.Disabled() {
		resp.Errors = nil
	}
	return r.Push(r.writer, resp)
}

func (r *Renderer) FatalWith(message string, errs ...error) error {
	if message == "" {
		message = defaultFatalMessage
	}
	filtered := r.filterErrors(errs)

	hadSkippable := false
	hadHidden := false
	for _, e := range errs {
		if errors.Is(e, ErrSkip) {
			hadSkippable = true
		}
		if errors.Is(e, ErrHidden) {
			hadHidden = true
		}
	}
	if len(filtered) == 0 && hadSkippable && !hadHidden {
		return nil
	}
	var logErr error
	var logFields []interface{}
	if len(filtered) > 0 {
		logErr = filtered[0]
		logFields = make([]interface{}, len(filtered)-1)
		for i := 1; i < len(filtered); i++ {
			logFields[i-1] = filtered[i]
		}
	} else {
		logErr = errors.New(message)
	}
	if r.logger != nil {
		r.logger.Fatal(logErr, logFields...)
	}
	resp := Response{
		Status:  StatusFatal,
		Message: message,
		Errors:  prepareFatalResponseErrors(errs),
	}

	if r.showError.Disabled() {
		resp.Errors = nil
	}
	return r.Push(r.writer, resp)
}

func (r *Renderer) Fatalf(format string, args ...interface{}) error {
	var allErrorsFromArgs []error
	for _, arg := range args {
		if e, ok := arg.(error); ok {
			allErrorsFromArgs = append(allErrorsFromArgs, e)
		}
	}

	filtered := r.filterErrors(allErrorsFromArgs)

	hadSkippable := false
	hadHidden := false
	for _, e := range allErrorsFromArgs {
		if errors.Is(e, ErrSkip) {
			hadSkippable = true
		}
		if errors.Is(e, ErrHidden) {
			hadHidden = true
		}
	}

	message := r.formatWithSpecial(format, args)
	if message == "" {
		message = defaultFatalMessage
	}

	if len(filtered) == 0 && hadSkippable && !hadHidden {
		return nil
	}

	var logErr error
	var logFields []interface{}
	if len(filtered) > 0 {
		logErr = filtered[0]
		logFields = make([]interface{}, len(filtered)-1)
		for i := 1; i < len(filtered); i++ {
			logFields[i-1] = filtered[i]
		}
	} else {
		logErr = errors.New(message)
	}

	if r.logger != nil {
		r.logger.Fatal(logErr, logFields...)
	}

	resp := Response{
		Status:  StatusFatal,
		Message: message,
		Errors:  prepareFatalResponseErrors(allErrorsFromArgs),
	}

	if r.showError.Disabled() {
		resp.Errors = nil
	}
	return r.Push(r.writer, resp)
}

func (r *Renderer) formatWithSpecial(format string, args []interface{}) string {
	var builder strings.Builder
	i := 0
	argIndex := 0
	for i < len(format) {
		if format[i] != '%' {
			builder.WriteByte(format[i])
			i++
			continue
		}
		if i+1 < len(format) && format[i+1] == '%' {
			builder.WriteByte('%')
			i += 2
			continue
		}
		// Parse the verb
		start := i
		j := i + 1
		// Flags
		flags := "#+- 0"
		for j < len(format) && strings.Contains(flags, string(format[j])) {
			j++
		}
		// Width
		if j < len(format) && format[j] == '*' {
			j++
		} else {
			for j < len(format) && '0' <= format[j] && format[j] <= '9' {
				j++
			}
		}
		// Precision
		if j < len(format) && format[j] == '.' {
			j++
			if j < len(format) && format[j] == '*' {
				j++
			} else {
				for j < len(format) && '0' <= format[j] && format[j] <= '9' {
					j++
				}
			}
		}
		// Verb char
		if j >= len(format) {
			builder.WriteString(format[start:])
			return builder.String()
		}
		verbChar := format[j]
		verb := format[start : j+1]
		verbLetters := "bcdefgopqstuvwxXEGTU"
		if !strings.Contains(verbLetters, string(verbChar)) {
			// Not a valid verb, copy as is
			builder.WriteString(format[start : j+1])
			i = j + 1
			continue
		}
		i = j + 1
		if argIndex >= len(args) {
			builder.WriteString("%!v(MISSING)")
			continue
		}
		arg := args[argIndex]
		argIndex++
		e, isError := arg.(error)
		if !isError {
			builder.WriteString(fmt.Sprintf(verb, arg))
			continue
		}
		if r.isSkippable(e) {
			builder.WriteString("%!v(MISSING)")
			continue
		}
		if errors.Is(e, ErrHidden) {
			builder.WriteString("*hidden*")
			continue
		}
		if e == nil {
			builder.WriteString("<nil>")
			continue
		}
		// Normal error
		builder.WriteString(fmt.Sprintf(verb, e))
	}
	return builder.String()
}

// Helper to determine if an error is skippable (e.g., for ErrSkip or sql.ErrNoRows).
// Include any custom filters here.
func (r *Renderer) isSkippable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrSkip) {
		return true
	}
	for _, f := range r.errorFilters {
		if f(err) {
			return true
		}
	}
	return false
}

func (r *Renderer) filterErrors(errs []error) []error {
	var filtered []error
	for _, err := range errs {
		if err == nil || errors.Is(err, ErrHidden) || r.isSkippable(err) {
			continue
		}
		filtered = append(filtered, err)
	}
	return filtered
}

func prepareFatalResponseErrors(errs []error) []error {
	if errs == nil {
		return nil
	}
	responseErrors := make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil && !errors.Is(err, ErrHidden) && !errors.Is(err, ErrSkip) {
			responseErrors = append(responseErrors, err)
		}
	}
	return responseErrors
}
