package utils

// StringValue safely dereferences a string pointer and returns an empty string
// when the pointer is nil.
func StringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// StringPtr returns a pointer to the provided string.
func StringPtr(value string) *string {
	return &value
}
