package beam

import "net/http"

// cloneHeader creates a deep copy of the given http.Header.
// Takes an http.Header to copy.
// Returns a new http.Header with copied keys and values.
// Ensures no shared references to the original header's slices.
func cloneHeader(h http.Header) http.Header {
	newHeader := make(http.Header, len(h))
	for k, v := range h {
		newVals := make([]string, len(v))
		copy(newVals, v)
		newHeader[k] = newVals
	}
	return newHeader
}

// cloneMap creates a shallow copy of a map.
// Takes a map[string]interface{} to copy.
// Returns a new map with the same key-value pairs.
// Pre-allocates capacity to avoid reallocations during copying.
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
// Takes a []string to copy.
// Returns a new slice with copied elements.
// Uses copy to ensure no shared references to the original slice.
func cloneSlice(s []string) []string {
	newSlice := make([]string, len(s))
	copy(newSlice, s)
	return newSlice
}
