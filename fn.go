package beam

import (
	"net/http"
)

// Error2Any converts a slice of errors to a slice of interfaces.
// Transforms errors into a format suitable for variadic functions expecting interface{}.
// Returns a new slice containing the errors as interface{} values.
func Error2Any(errs ...error) []interface{} {
	args := make([]interface{}, len(errs))
	for i, e := range errs {
		args[i] = e
	}
	return args
}

// Any2Error converts a slice of interfaces to a slice of errors with type checking.
// Filters out non-error values and nil errors from the input slice.
// Returns a new slice containing only valid, non-nil error values.
func Any2Error(as ...interface{}) []error {
	args := make([]error, 0, len(as))
	for _, e := range as {
		if err, ok := e.(error); ok && err != nil {
			args = append(args, err)
		}
	}
	return args
}

// cloneHeader creates a deep copy of an HTTP header map.
// Copies all keys and values from the provided http.Header to a new map.
// Returns a new http.Header with no shared references to the original.
func cloneHeader(h http.Header) http.Header {
	newHeader := make(http.Header, len(h))
	for k, v := range h {
		newVals := make([]string, len(v))
		copy(newVals, v)
		newHeader[k] = newVals
	}
	return newHeader
}

// cloneMap creates a shallow copy of a string-to-interface map.
// Copies key-value pairs from the input map to a new map with pre-allocated capacity.
// Returns a new map or nil if the input map is nil.
func cloneMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	newMap := make(map[string]interface{}, len(m))
	for k, v := range m {
		newMap[k] = v
	}
	return newMap
}

// cloneSlice creates a deep copy of a string slice.
// Copies all elements from the input slice to a new slice.
// Returns a new slice with no shared references to the original.
func cloneSlice(s []string) []string {
	newSlice := make([]string, len(s))
	copy(newSlice, s)
	return newSlice
}
