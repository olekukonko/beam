package beam

import "net/http"

// -----------------------------------------------------------------------------
// Helper Functions for Deep Copying Mutable Fields
// -----------------------------------------------------------------------------

// cloneHeader creates a deep copy of the given http.Header.
func cloneHeader(h http.Header) http.Header {
	newHeader := make(http.Header)
	for k, v := range h {
		newVals := make([]string, len(v))
		copy(newVals, v)
		newHeader[k] = newVals
	}
	return newHeader
}

// cloneMap creates a shallow copy of a map.
func cloneMap(m map[string]interface{}) map[string]interface{} {
	newMap := make(map[string]interface{})
	for k, v := range m {
		newMap[k] = v
	}
	return newMap
}

// cloneSlice creates a deep copy of a string slice.
func cloneSlice(s []string) []string {
	newSlice := make([]string, len(s))
	copy(newSlice, s)
	return newSlice
}
