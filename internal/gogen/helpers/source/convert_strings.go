// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package source

// sglConvertStrings returns a slice of values of one string type as values of another,
// keeping a nil slice nil. The generated code binds a slice of an enum type as Go's own
// strings through it, and reads one back from them, for a driver that takes and fills
// only []string.
//
// Takes values ([]From) which holds the string values to convert.
//
// Returns []To which holds converted values, or is nil when values is nil.
//
//sgl:when slice
func sglConvertStrings[To, From ~string](values []From) []To {
	if values == nil {
		return nil
	}
	converted := make([]To, len(values))
	for index, value := range values {
		converted[index] = To(value)
	}
	return converted
}

// sglConvertStringPointers copies nullable values between string types.
//
// It returns a slice of pointers to values of one string type as pointers to copies of
// them in another, keeping a nil slice nil and a nil element nil. The generated code
// binds a slice of nullable enum values as Go's own *string through it, and reads one
// back from them, for a driver that takes and fills only []*string.
//
// Takes values ([]*From) which holds the nullable string values to convert.
//
// Returns []*To which holds independent converted values, preserving nil slices and
// elements.
//
//sgl:when pointers
func sglConvertStringPointers[To, From ~string](values []*From) []*To {
	if values == nil {
		return nil
	}
	converted := make([]*To, len(values))
	for index, value := range values {
		if value != nil {
			copied := To(*value)
			converted[index] = &copied
		}
	}
	return converted
}

// sglConvertStringMap converts map values between string types.
//
// It returns a map whose values are of one string type as a map of the same keys whose
// values are of another, keeping a nil map nil. The generated code binds a map of enum
// values as Go's own strings through it, and reads one back from them, for a driver that
// takes and fills only a map of string values.
//
// Takes values (map[Key]From) which maps keys to the string values to convert.
//
// Returns map[Key]To which holds the same keys with converted values, preserving nil
// maps.
//
//sgl:when map
func sglConvertStringMap[To ~string, Key comparable, From ~string](values map[Key]From) map[Key]To {
	if values == nil {
		return nil
	}
	converted := make(map[Key]To, len(values))
	for key, value := range values {
		converted[key] = To(value)
	}
	return converted
}

// sglDereference reads a pointer, returning the zero value for nil.
//
// It returns the value a pointer points at, or the zero value for a nil pointer. The
// generated code reads an optional parameter whose Go type is a pointer to a slice or a
// map of an enum type through it before converting the elements, so a nil pointer binds
// as NULL, as a nil slice or map does.
//
// Takes value (*T) which is the optional value to read.
//
// Returns T which is the pointed-to value, or its zero value for nil.
//
//sgl:when dereference
func sglDereference[T any](value *T) T {
	if value == nil {
		var zero T
		return zero
	}
	return *value
}
