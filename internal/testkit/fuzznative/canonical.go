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

package fuzznative

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// canonicalDepth bounds nested collections in a canonical value.
const canonicalDepth = 16

// CanonicalFields writes each exported field of a generated row canonically.
//
// Takes row (reflect.Value) which is a generated row struct.
//
// Returns []string which holds one canonical value per field in declaration order.
func CanonicalFields(row reflect.Value) []string {
	row = followValue(row)
	if !row.IsValid() || row.Kind() != reflect.Struct {
		return []string{Canonical(row)}
	}
	fields := make([]string, 0, row.NumField())
	for index := range row.NumField() {
		if row.Type().Field(index).IsExported() {
			fields = append(fields, Canonical(row.Field(index)))
		}
	}
	return fields
}

// Canonical writes a generated value so that equal SQL values from different Go
// representations compare equal, while NULL, empty and nil collections stay distinct.
//
// Takes value (reflect.Value) which is a generated field.
//
// Returns string which names the value's class and its exact contents.
func Canonical(value reflect.Value) string {
	return canonicalValue(value, 0)
}

// canonicalValue writes one value within the depth bound.
//
// Takes value (reflect.Value) which is a generated value.
// Takes depth (int) which counts enclosing collections.
//
// Returns string which is the canonical form.
func canonicalValue(value reflect.Value, depth int) string {
	current := followValue(value)
	if !current.IsValid() {
		return "null"
	}
	if depth > canonicalDepth {
		return "too-deep"
	}
	if text, ok := canonicalText(current); ok {
		return text
	}
	switch current.Kind() {
	case reflect.Slice:
		if current.IsNil() {
			return "nil-" + current.Type().Elem().Kind().String() + "s"
		}
		return canonicalCollection(current, depth)
	case reflect.Array:
		return canonicalCollection(current, depth)
	case reflect.Map:
		entries := make([]string, 0, current.Len())
		for iterator := current.MapRange(); iterator.Next(); {
			entries = append(entries, canonicalValue(iterator.Key(), depth+1)+"="+canonicalValue(iterator.Value(), depth+1))
		}
		slices.Sort(entries)
		return "map{" + strings.Join(entries, ",") + "}"
	case reflect.Struct:
		fields := make([]string, 0, current.NumField())
		for index := range current.NumField() {
			if current.Type().Field(index).IsExported() {
				fields = append(fields, canonicalValue(current.Field(index), depth+1))
			}
		}
		return "struct{" + strings.Join(fields, ",") + "}"
	default:
		return "other:" + fmt.Sprint(current.Interface())
	}
}

// canonicalText writes scalars and values with an exact text form.
//
// Takes value (reflect.Value) which is a non-NULL generated value.
//
// Returns string which is the canonical form.
// Returns bool which is false for collections and structs without a text form.
func canonicalText(value reflect.Value) (string, bool) {
	if moment, ok := reflect.TypeAssert[time.Time](value); ok {
		return "time:" + moment.Format(time.RFC3339Nano), true
	}
	if duration, ok := reflect.TypeAssert[time.Duration](value); ok {
		return "duration:" + strconv.FormatInt(int64(duration), scalarBase), true
	}
	switch {
	case value.Kind() == reflect.Bool:
		return "bool:" + strconv.FormatBool(value.Bool()), true
	case value.CanInt():
		return "number:" + strconv.FormatInt(value.Int(), scalarBase), true
	case value.CanUint() && value.Kind() != reflect.Uint8:
		return "number:" + strconv.FormatUint(value.Uint(), scalarBase), true
	case value.CanFloat():
		return "float:" + strconv.FormatFloat(value.Float(), 'g', -1, value.Type().Bits()), true
	case value.Kind() == reflect.String:
		return "text:" + strconv.Quote(value.String()), true
	case value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Uint8 && !value.IsNil():
		return "bytes:" + hex.EncodeToString(value.Bytes()), true
	}
	if value.Kind() == reflect.Struct || value.Kind() == reflect.Array {
		if !value.CanAddr() {

			copied := reflect.New(value.Type()).Elem()
			copied.Set(value)
			value = copied
		}
		if _, ok := reflect.TypeAssert[fmt.Stringer](value.Addr()); ok {
			return "text:" + strconv.Quote(observedText(value)), true
		}
	}
	return "", false
}

// canonicalCollection writes the elements of a slice or array in order.
//
// Takes value (reflect.Value) which is a slice or array.
// Takes depth (int) which counts enclosing collections.
//
// Returns string which lists the canonical elements.
func canonicalCollection(value reflect.Value, depth int) string {
	elements := make([]string, 0, value.Len())
	for index := range value.Len() {
		elements = append(elements, canonicalValue(value.Index(index), depth+1))
	}
	return "[" + strings.Join(elements, ",") + "]"
}
