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
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// NativeValue decodes a raw database observation independently of generated row types.
//
// Takes kind (string) which names the independent value family.
// Takes raw (any) which is the native driver's untyped column observation.
// Takes null (bool) which records a separately selected SQL NULL predicate.
//
// Returns fuzzcase.TypedValue which retains exact digits, bits and NULL state.
// Returns error when the native observation cannot represent the modelled family.
func NativeValue(kind string, raw any, null bool) (fuzzcase.TypedValue, error) {
	if null {
		return Observed(reflect.ValueOf(raw), kind, true)
	}
	if kind == kindBoolean {
		truth, err := observedBoolean(reflect.ValueOf(raw))
		if err != nil {
			return fuzzcase.TypedValue{}, err
		}
		number := numberZero
		if truth {
			number = numberOne
		}
		return fuzzcase.TypedNumber(kind, number, 0), nil
	}
	if bytes, ok := raw.([]byte); ok && kind != kindBinary {
		raw = string(bytes)
	}
	if kind == kindArray {
		text, ok := raw.(string)
		if !ok {
			return fuzzcase.TypedValue{}, errors.New("native array reference is not JSON text")
		}
		return nativeArray(text)
	}
	return Observed(reflect.ValueOf(raw), kind, false)
}

// nativeArray reads exact integer array elements from the native JSON projection.
//
// Takes text (string) which contains the database's independently rendered array.
//
// Returns fuzzcase.TypedValue which preserves element ordering and NULL masks.
// Returns error when the array contains a value outside the declared model.
func nativeArray(text string) (fuzzcase.TypedValue, error) {
	if !json.Valid([]byte(text)) {
		return fuzzcase.TypedValue{}, errors.New("invalid native array JSON")
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var elements []any
	if err := decoder.Decode(&elements); err != nil {
		return fuzzcase.TypedValue{}, fmt.Errorf("decoding native array: %w", err)
	}
	result := fuzzcase.TypedNumber(kindArray, numberZero, 0)
	for _, element := range elements {
		value := fuzzcase.TypedNumber(kindSigned, numberZero, 0)
		if element == nil {
			value.Null = true
		} else {
			switch number := element.(type) {
			case json.Number:
				value.Number = string(number)
			case string:
				value.Number = number
			default:
				return result, fmt.Errorf("native integer array contains %T", element)
			}
			if _, err := strconv.ParseInt(value.Number, scalarBase, scalarBits); err != nil {
				return result, fmt.Errorf("native integer array element: %w", err)
			}
		}
		result.Elements = append(result.Elements, value)
	}
	return result, result.Validate()
}
