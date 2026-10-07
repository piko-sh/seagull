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
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

// JSON holds a JSON document read from or written to a JSON column. A nil JSON is SQL
// NULL and encodes as the JSON null literal.
//
//sgl:when jsonType
type JSON []byte

// Scan implements sql.Scanner. It accepts the shapes drivers return for JSON columns:
// bytes (copied, never aliased), text, or an already decoded value, which is re-encoded.
//
// Takes source (any) which is the value supplied by the driver.
//
// Returns error when the column value cannot be decoded into the destination.
//
//sgl:when jsonType
func (j *JSON) Scan(source any) error {
	switch value := source.(type) {
	case nil:
		*j = nil
		return nil
	case []byte:
		clone := make(JSON, len(value))
		copy(clone, value)
		*j = clone
		return nil
	case string:
		*j = JSON(value)
		return nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("sgl: encode %T to JSON: %w", source, err)
		}
		*j = encoded
		return nil
	}
}

// Value implements driver.Valuer. A nil JSON is written as SQL NULL, and any other as the
// text it holds, byte for byte: JSON is text, and a driver binds bytes as a binary value,
// which a column may store as binary, never equal to the same text, or refuse.
//
// Returns driver.Value which is the stored JSON text, or nil for a nil value.
// Returns error which is always nil.
//
//sgl:when jsonType
func (j JSON) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return string(j), nil
}

// MarshalJSON implements json.Marshaler. A nil JSON encodes as the JSON null literal.
//
// Returns []byte which contains the stored JSON, or the literal null for a nil value.
// Returns error which is always nil.
//
//sgl:when jsonType
func (j JSON) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON implements json.Unmarshaler, keeping a copy of the raw document.
//
// Takes data ([]byte) which is the JSON text to copy into the value.
//
// Returns error which is always nil.
//
//sgl:when jsonType
func (j *JSON) UnmarshalJSON(data []byte) error {
	clone := make(JSON, len(data))
	copy(clone, data)
	*j = clone
	return nil
}

// sglJSONScanner decodes a JSON column into a typed destination.
//
//sgl:when scanInto
type sglJSONScanner[T any] struct {
	// dest points to the value filled by the scan adapter.
	dest *T
}

// Scan implements sql.Scanner. It accepts bytes, text, or an already decoded value, which
// is re-encoded before decoding into the destination.
//
// Takes source (any) which is the value supplied by the driver.
//
// Returns error when the column value cannot be decoded into the destination.
//
//sgl:when scanInto
func (s sglJSONScanner[T]) Scan(source any) error {
	var raw []byte
	switch value := source.(type) {
	case nil:
		var zero T
		*s.dest = zero
		return nil
	case []byte:
		raw = value
	case string:
		raw = []byte(value)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("sgl: re-encode %T to JSON: %w", source, err)
		}
		raw = encoded
	}
	if len(raw) == 0 {
		var zero T
		*s.dest = zero
		return nil
	}
	if err := sglDecodeJSONColumn(raw, s.dest); err != nil {
		var zero T
		*s.dest = zero
		return fmt.Errorf("sgl: decode JSON column: %w", err)
	}
	return nil
}

// sglScanInto adapts a JSON column into a typed scan destination.
//
// It adapts dest into a sql.Scanner that decodes a JSON column into it. A NULL or empty
// column leaves dest at its zero value. A column holding more than one JSON document is
// refused. A null array element or object value is read only into a Go type that holds
// it, as sglJSONNulls describes.
//
// Takes dest (*T) which receives the decoded value.
//
// Returns sql.Scanner which decodes a driver value into dest.
//
//sgl:when scanInto
func sglScanInto[T any](dest *T) sql.Scanner {
	return sglJSONScanner[T]{dest: dest}
}

// sglDecodeJSONColumn decodes a non-empty JSON column into dest: a single document, with
// nothing but space around it, decoded again in the forms an engine writes when
// json.Unmarshal refuses it, and refused when it holds a null array element or object
// value dest cannot hold.
//
// Takes raw ([]byte) which contains exactly one JSON document.
// Takes dest (*T) which receives the decoded value.
//
// Returns error when JSON decoding or a collection nullability check fails.
//
//sgl:when scanInto
func sglDecodeJSONColumn[T any](raw []byte, dest *T) error {
	fast := !bytes.Contains(raw, []byte("null"))
	var decodeErr error
	if fast {
		decodeErr = json.Unmarshal(raw, dest)
		if decodeErr == nil {
			return nil
		}
	}
	document, err := sglJSONDocument(raw)
	if err != nil {
		return err
	}
	if !fast {
		decodeErr = json.Unmarshal(raw, dest)
	}
	if decodeErr != nil && !sglDecodeEngineJSON(document, dest) {
		return decodeErr
	}
	return sglJSONNulls(document, reflect.TypeFor[T]())
}

// sglJSONDocument decodes the one JSON document a column holds, keeping its numbers as
// written, so a number wider than a float64 still decodes. Text that is not exactly one
// JSON document, such as a document followed by another, is refused with the syntax error
// json.Unmarshal reports.
//
// Takes raw ([]byte) which contains the JSON text to decode.
//
// Returns any which is the decoded document with numbers preserved as json.Number.
// Returns error when the text does not contain exactly one valid JSON document.
//
//sgl:when scanInto
func sglJSONDocument(raw []byte) (any, error) {
	var document any
	if !json.Valid(raw) {
		return nil, json.Unmarshal(raw, &document)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	return document, nil
}

// sglDecodeEngineJSON retries decoding after rewriting engine-specific JSON values.
//
// It decodes a document json.Unmarshal refused, after writing the values an engine writes
// in its own form, as its to-JSON function does for the elements of an array, in the
// forms Go reads: a timestamp without a zone, read as UTC, and a date, for a time.Time; a
// number, as written, for a string or a type that unmarshals itself from text, such as a
// decimal; and binary data written as \x and hex digits, for a []byte. It reports whether
// the document decoded.
//
// Takes document (any) which is the decoded document whose engine-specific forms need
// conversion.
// Takes dest (*T) which receives the decoded value.
//
// Returns bool which reports whether the rewritten document decoded successfully.
//
//sgl:when scanInto
func sglDecodeEngineJSON[T any](document any, dest *T) bool {
	rewritten, err := json.Marshal(sglEngineJSONValue(document, reflect.TypeFor[T]()))
	if err != nil {
		return false
	}
	var zero T
	*dest = zero
	return json.Unmarshal(rewritten, dest) == nil
}

// sglEngineJSONValue writes one decoded value, and the values inside it, in the form the
// Go type that reads it takes, as sglDecodeEngineJSON describes.
//
// Takes value (any) which is the caller-supplied value to adapt.
// Takes target (reflect.Type) which is the destination Go type.
//
// Returns any which holds the value rewritten for the destination type.
//
//sgl:when scanInto
func sglEngineJSONValue(value any, target reflect.Type) any {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	switch typed := value.(type) {
	case []any:
		if target.Kind() == reflect.Slice || target.Kind() == reflect.Array {
			return sglEngineJSONElements(typed, target.Elem())
		}
	case map[string]any:
		if target.Kind() == reflect.Map {
			rewritten := make(map[string]any, len(typed))
			for key, element := range typed {
				rewritten[key] = sglEngineJSONValue(element, target.Elem())
			}
			return rewritten
		}
	case json.Number:
		if target.Kind() == reflect.String || sglUnmarshalsText(target) {
			return string(typed)
		}
	case string:
		return sglEngineJSONText(typed, target)
	}
	return value
}

// sglEngineJSONElements writes the elements of a JSON array as sglEngineJSONValue writes
// each.
//
// Takes elements ([]any) which holds the decoded JSON array elements.
// Takes target (reflect.Type) which is the destination Go type.
//
// Returns []any which holds rewritten elements in the original order.
//
//sgl:when scanInto
func sglEngineJSONElements(elements []any, target reflect.Type) []any {
	rewritten := make([]any, len(elements))
	for index, element := range elements {
		rewritten[index] = sglEngineJSONValue(element, target)
	}
	return rewritten
}

// sglEngineJSONText writes a JSON string an engine wrote in the form the Go type that
// reads it takes: a timestamp without a zone or a date as the RFC 3339 text a time.Time
// reads, and \x followed by hex digits as the bytes a []byte reads.
//
// Takes text (string) which is the engine's JSON string value.
// Takes target (reflect.Type) which is the destination Go type.
//
// Returns any which is rewritten date or binary data, or the original text.
//
//sgl:when scanInto
func sglEngineJSONText(text string, target reflect.Type) any {
	switch {
	case target == reflect.TypeFor[time.Time]():
		for _, layout := range []string{"2006-01-02T15:04:05.999999999", time.DateOnly} {
			if moment, err := time.ParseInLocation(layout, text, time.UTC); err == nil {
				return moment.Format(time.RFC3339Nano)
			}
		}
	case target.Kind() == reflect.Slice && target.Elem().Kind() == reflect.Uint8 && !sglUnmarshalsJSON(target):
		if digits, found := strings.CutPrefix(text, `\x`); found {
			if decoded, err := hex.DecodeString(digits); err == nil {
				return decoded
			}
		}
	}
	return text
}

// sglUnmarshalsText reports whether a Go type reads itself from text and not from JSON,
// so json.Unmarshal reads it only from a JSON string.
//
// Takes target (reflect.Type) which is the destination Go type.
//
// Returns bool which reports whether the type reads text without a JSON decoder.
//
//sgl:when scanInto
func sglUnmarshalsText(target reflect.Type) bool {
	return reflect.PointerTo(target).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) && !sglUnmarshalsJSON(target)
}

// sglUnmarshalsJSON reports whether a Go type reads itself from JSON.
//
// Takes target (reflect.Type) which is the destination Go type.
//
// Returns bool which reports whether a pointer to the type implements json.Unmarshaler.
//
//sgl:when scanInto
func sglUnmarshalsJSON(target reflect.Type) bool {
	return reflect.PointerTo(target).Implements(reflect.TypeFor[json.Unmarshaler]())
}

// sglJSONNulls checks that JSON collection nulls fit their destination Go types.
//
// It reports the first null a JSON array element or object value holds where the Go type
// that reads it, looked through any pointers to it, cannot hold NULL, which
// json.Unmarshal would leave at its zero value, so a NULL element never reads as a value
// the database does not hold. The fields of a struct are left to json's own rules.
//
// Takes document (any) which is the decoded JSON value to check.
// Takes target (reflect.Type) which is the destination Go type.
//
// Returns error when an array element or map value is null and its Go type cannot hold
// it.
//
//sgl:when scanInto
func sglJSONNulls(document any, target reflect.Type) error {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	switch typed := document.(type) {
	case []any:
		if target.Kind() == reflect.Slice || target.Kind() == reflect.Array {
			return sglJSONElementNulls(typed, target.Elem())
		}
	case map[string]any:
		if target.Kind() == reflect.Map {
			return sglJSONValueNulls(typed, target.Elem())
		}
	}
	return nil
}

// sglJSONElementNulls reports the first null among a JSON array's elements, or inside
// them, that element, the Go type of the elements, cannot hold.
//
// Takes elements ([]any) which holds the decoded array elements.
// Takes element (reflect.Type) which is the destination element type.
//
// Returns error when an element contains a null that its Go type cannot hold.
//
//sgl:when scanInto
func sglJSONElementNulls(elements []any, element reflect.Type) error {
	for index, value := range elements {
		if err := sglJSONNull(value, element); err != nil {
			return fmt.Errorf("element %d: %w", index, err)
		}
	}
	return nil
}

// sglJSONValueNulls reports the first null among a JSON object's values, or inside them,
// in key order, that element, the Go type of the values, cannot hold.
//
// Takes values (map[string]any) which holds the decoded object values.
// Takes element (reflect.Type) which is the destination map value type.
//
// Returns error when a value contains a null that its Go type cannot hold.
//
//sgl:when scanInto
func sglJSONValueNulls(values map[string]any, element reflect.Type) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if err := sglJSONNull(values[key], element); err != nil {
			return fmt.Errorf("value of key %s: %w", key, err)
		}
	}
	return nil
}

// sglJSONNull reports a JSON null read into a Go type that cannot hold NULL, one that is
// not a pointer, an interface, a slice or a map and does not decode JSON itself, and the
// nulls of a value's own elements.
//
// Takes value (any) which is the caller-supplied value to adapt.
// Takes target (reflect.Type) which is the destination Go type.
//
// Returns error when a null value cannot be held by the destination type.
//
//sgl:when scanInto
func sglJSONNull(value any, target reflect.Type) error {
	if value != nil {
		return sglJSONNulls(value, target)
	}
	switch target.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		return nil
	}
	if reflect.PointerTo(target).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	return fmt.Errorf("converting NULL to %s is unsupported", target)
}
