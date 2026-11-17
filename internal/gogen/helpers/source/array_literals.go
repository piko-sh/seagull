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

import (
	"strconv"
	"strings"
)

// sglArrayLiteralBase is the base an integer element is written in.
const sglArrayLiteralBase = 10

// sglArrayLiteralDigits is the longest an integer element is written, the length of the
// smallest int64.
const sglArrayLiteralDigits = len("-9223372036854775808")

// sglLiteralElement lists the Go types of the elements of a slice sglArrayLiteral writes.
type sglLiteralElement interface {
	string | *string | int64 | *int64 | int32 | *int32 | int16 | *int16 | int | *int | bool | *bool
}

// sglArrayLiteral writes a slice as a one-dimensional SQL array literal.
//
// Strings escape quotes and backslashes, and nil elements become NULL. A nil or empty
// slice becomes an empty array rather than SQL NULL.
//
// Takes values ([]E) which is the slice.
//
// Returns string which is the array's text form.
func sglArrayLiteral[E sglLiteralElement](values []E) string {
	var builder strings.Builder
	builder.Grow(sglArrayLiteralSize(values))
	builder.WriteByte('{')
	for index := range values {
		if index > 0 {
			builder.WriteByte(',')
		}
		sglWriteArrayElement(&builder, &values[index])
	}
	builder.WriteByte('}')
	return builder.String()
}

// sglArrayLiteralSize estimates the length of a slice's array text form, so the text is
// written into one allocation for the common sizes.
//
// Takes values ([]E) which is the slice.
//
// Returns int which is the estimated length.
func sglArrayLiteralSize[E sglLiteralElement](values []E) int {
	size := len("{}") + len(values)
	for index := range values {
		switch value := any(&values[index]).(type) {
		case *string:
			size += len(*value) + len(`""`)
		case **string:
			if *value != nil {
				size += len(**value) + len(`""`)
			}
		default:
			size += sglArrayLiteralDigits
		}
	}
	return size
}

// sglWriteArrayElement writes one element of an array's text form.
//
// Takes builder (*strings.Builder) which receives the element.
// Takes element (*E) which points to the element.
func sglWriteArrayElement[E sglLiteralElement](builder *strings.Builder, element *E) {
	var integer int64
	switch value := any(element).(type) {
	case *string:
		sglWriteArrayText(builder, *value)
		return
	case *bool:
		builder.WriteString(strconv.FormatBool(*value))
		return
	case *int64:
		integer = *value
	case *int32:
		integer = int64(*value)
	case *int16:
		integer = int64(*value)
	case *int:
		integer = int64(*value)
	default:
		sglWriteArrayPointer(builder, element)
		return
	}
	var digits [sglArrayLiteralDigits]byte
	builder.Write(strconv.AppendInt(digits[:0], integer, sglArrayLiteralBase))
}

// sglWriteArrayPointer writes an element held by a pointer, or NULL for a nil one. The
// element constraint allows no other type, so the switch needs no default.
//
// Takes builder (*strings.Builder) which receives the element.
// Takes element (*E) which points to the pointer.
func sglWriteArrayPointer[E sglLiteralElement](builder *strings.Builder, element *E) {
	switch value := any(element).(type) {
	case **string:
		sglWriteArrayDereferenced(builder, *value)
	case **int64:
		sglWriteArrayDereferenced(builder, *value)
	case **int32:
		sglWriteArrayDereferenced(builder, *value)
	case **int16:
		sglWriteArrayDereferenced(builder, *value)
	case **int:
		sglWriteArrayDereferenced(builder, *value)
	case **bool:
		sglWriteArrayDereferenced(builder, *value)
	}
}

// sglWriteArrayDereferenced writes the value a pointer points to, or NULL for nil.
//
// Takes builder (*strings.Builder) which receives the element.
// Takes value (*T) which is the pointer.
func sglWriteArrayDereferenced[T string | int64 | int32 | int16 | int | bool](builder *strings.Builder, value *T) {
	if value == nil {
		builder.WriteString("NULL")
		return
	}
	sglWriteArrayElement(builder, value)
}

// sglWriteArrayText writes a string element quoted, with its quotes and backslashes
// escaped, so no text, not even NULL or an empty string, reads as anything but itself.
//
// Takes builder (*strings.Builder) which receives the element.
// Takes text (string) which is the element.
func sglWriteArrayText(builder *strings.Builder, text string) {
	builder.WriteByte('"')
	for start := 0; ; {
		next := strings.IndexAny(text[start:], `"\`)
		if next < 0 {
			builder.WriteString(text[start:])
			break
		}
		builder.WriteString(text[start : start+next])
		builder.WriteByte('\\')
		builder.WriteByte(text[start+next])
		start += next + 1
	}
	builder.WriteByte('"')
}
