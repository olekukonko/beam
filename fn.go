package beam

import (
	"errors"
	"net/http"
	"runtime"
	"strings"
)

// Error2Any converts a slice of errors to a slice of interfaces.
// It transforms errors into a format suitable for variadic functions expecting interface{}.
// Returns a new slice containing the errors as interface{} values.
func Error2Any(errs ...error) []interface{} {
	args := make([]interface{}, len(errs))
	for i, e := range errs {
		args[i] = e
	}
	return args
}

// Any2Error converts a slice of interfaces to a slice of errors.
// It filters out non-error values and nil errors, retaining only valid errors.
// Returns a new slice containing non-nil error values.
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
// It duplicates all keys and values from the input http.Header into a new map.
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
// It duplicates key-value pairs from the input map into a new map with pre-allocated capacity.
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
// It duplicates all elements from the input slice into a new slice.
// Returns a new slice with no shared references to the original.
/*
func cloneSlice(s []string) []string {
	newSlice := make([]string, len(s))
	copy(newSlice, s)
	return newSlice
}
*/

// checkErrors inspects a slice of errors for non-nil and hidden errors.
// It checks if any errors are non-nil and if any are marked as ErrHidden.
// Returns two booleans: whether non-nil errors exist and whether any are hidden.
func checkErrors(errs ...error) (hasNonNil, hasHidden bool) {
	for _, err := range errs {
		if err != nil {
			hasNonNil = true
			if errors.Is(err, ErrHidden) {
				hasHidden = true
			}
		}
	}
	return hasNonNil, hasHidden
}

// isFrameworkFrame checks if a stack frame belongs to a framework package.
// It matches the file path or function name against known framework patterns.
// Returns true if the frame is part of a framework package, false otherwise.
func isFrameworkFrame(filePath, funcName string) bool {
	for _, pattern := range frameworkPatterns {
		if strings.Contains(filePath, pattern) || strings.Contains(funcName, pattern) {
			return true
		}
	}
	return false
}

// getCallerInfo retrieves details about the first non-framework caller in the call stack.
// It walks the stack to find the first frame not belonging to a framework package.
// Returns the file name, line number, and function name of the caller, or "unknown" values if none is found.
func getCallerInfo() (file string, line int, funcName string) {
	// Start at 2 to skip:
	// 0: runtime.Caller itself
	// 1: this function (getCallerInfo)
	// 2+: potential user or framework frames
	for i := 2; ; i++ {
		pc, filePath, lineNum, ok := runtime.Caller(i)
		if !ok {
			// Reached the top of the stack
			break
		}

		fn := runtime.FuncForPC(pc)
		if fn == nil {
			continue
		}
		fullFuncName := fn.Name()

		// Return the first non-framework frame
		if !isFrameworkFrame(filePath, fullFuncName) {
			// Extract the filename from the full path
			parts := strings.Split(filePath, "/")
			shortFile := parts[len(parts)-1]

			// Extract the function name without the package path
			parts = strings.Split(fullFuncName, ".")
			shortFuncName := parts[len(parts)-1]

			return shortFile, lineNum, shortFuncName
		}
	}
	return "unknown", 0, "unknown"
}
