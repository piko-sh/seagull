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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

const (
	// scalarBits is the widest scalar representation admitted by the protocol.
	scalarBits = 64

	// scalarBase is the decimal wire representation of integer values.
	scalarBase = 10

	// wallClockLayout retains microseconds without adding a time zone.
	wallClockLayout = "2006-01-02 15:04:05.999999"

	// followDepth bounds the pointer, interface and Null[T] layers followValue removes.
	followDepth = 64

	// jsonNull is the JSON spelling of a null document.
	jsonNull = "null"
)

// Argument converts an independent typed value into an actual generated Go type.
//
// Takes target (reflect.Type) which is the generated method's parameter type.
// Takes value (fuzzcase.TypedValue) which supplies an exact independent value.
//
// Returns reflect.Value which can be passed directly to the generated method.
// Returns error when the generated type cannot represent the modelled value.
func Argument(target reflect.Type, value fuzzcase.TypedValue) (reflect.Value, error) {
	if err := value.Validate(); err != nil {
		return reflect.Value{}, err
	}
	if value.Null {
		if _, wrapped := nullElement(target); wrapped {
			return reflect.Zero(target), nil
		}
		switch target.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
			return reflect.Zero(target), nil
		default:
			return reflect.Value{}, fmt.Errorf("non-nullable generated argument %s received SQL NULL", target)
		}
	}
	if element, wrapped := nullElement(target); wrapped {
		inner, err := Argument(element, value)
		if err != nil {
			return reflect.Value{}, err
		}
		result := reflect.New(target).Elem()
		result.Field(0).Set(inner)
		result.Field(1).SetBool(true)
		return result, nil
	}
	if target.Kind() == reflect.Pointer {
		inner, err := Argument(target.Elem(), value)
		if err != nil {
			return reflect.Value{}, err
		}
		result := reflect.New(target.Elem())
		result.Elem().Set(inner)
		return result, nil
	}
	return argumentScalar(target, value)
}

// argumentScalar constructs a non-pointer generated scalar or scanner.
//
// Takes target (reflect.Type) which names the concrete generated representation.
// Takes value (fuzzcase.TypedValue) which supplies an independently validated value.
//
// Returns reflect.Value which contains an assignable generated argument.
// Returns error when construction would lose information.
func argumentScalar(target reflect.Type, value fuzzcase.TypedValue) (reflect.Value, error) {
	result := reflect.New(target).Elem()
	if target == reflect.TypeFor[time.Time]() {
		moment, err := valueTime(value)
		if err != nil {
			return reflect.Value{}, err
		}
		result.Set(reflect.ValueOf(moment))
		return result, nil
	}
	if scanner, ok := reflect.TypeAssert[sql.Scanner](result.Addr()); ok {
		input, err := scannerInput(value)
		if err != nil {
			return reflect.Value{}, err
		}
		if err := scanner.Scan(input); err != nil {
			return reflect.Value{}, fmt.Errorf("constructing generated scanner %s: %w", target, err)
		}
		return result, nil
	}
	if err := assignScalar(result, value); err != nil {
		return reflect.Value{}, err
	}
	return result, nil
}

// Observed encodes an actual generated result without lossy JSON numbers.
//
// Takes value (reflect.Value) which holds the generated row field.
// Takes kind (string) which identifies the independent native value family.
// Takes null (bool) which is the independently selected SQL NULL flag.
//
// Returns fuzzcase.TypedValue which retains the observed value and NULL distinction.
// Returns error when generated output contradicts its native representation.
func Observed(value reflect.Value, kind string, null bool) (fuzzcase.TypedValue, error) {
	result := fuzzcase.TypedNumber(kind, numberZero, 0)
	current := followValue(value)
	if null {
		if !nilValue(current) {
			return result, errors.New("SQL NULL produced a populated generated field")
		}
		result.Null = true
		return result, nil
	}
	if !current.IsValid() {
		if kind == kindJSON {
			result.Text = jsonNull
			return result, nil
		}
		return result, errors.New("non-NULL result produced a nil generated field")
	}
	if err := observeScalar(&result, current); err != nil {
		return result, err
	}
	return result, nil
}

// assignScalar fills an allocated generated value from its independent wire value.
//
// Takes target (reflect.Value) which is addressable and non-null.
// Takes value (fuzzcase.TypedValue) which supplies the expected family.
//
// Returns error when conversion would overflow or discard structural information.
func assignScalar(target reflect.Value, value fuzzcase.TypedValue) error {
	switch target.Kind() {
	case reflect.String:
		target.SetString(valueText(value))
	case reflect.Bool:
		if value.Kind != kindBoolean {
			return errors.New("non-Boolean argument mapped to bool")
		}
		target.SetBool(value.Number == numberOne)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number, err := strconv.ParseInt(value.Number, scalarBase, target.Type().Bits())
		if err != nil {
			return fmt.Errorf("generated %s argument: %w", target.Type(), err)
		}
		target.SetInt(number)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number, err := strconv.ParseUint(value.Number, scalarBase, target.Type().Bits())
		if err != nil {
			return fmt.Errorf("generated %s argument: %w", target.Type(), err)
		}
		target.SetUint(number)
	case reflect.Float32, reflect.Float64:
		if value.Kind != kindFloat {
			return errors.New("exact numeric argument mapped to floating point")
		}
		number := math.Float64frombits(value.Bits)
		if target.OverflowFloat(number) {
			return errors.New("generated floating-point argument overflows")
		}
		target.SetFloat(number)
	case reflect.Slice, reflect.Array:
		return assignCollection(target, value)
	default:
		return fmt.Errorf("no lossless constructor for generated %s holding %s", target.Type(), value.Kind)
	}
	return nil
}

// assignCollection preserves bytes, JSON and ordered array members.
//
// Takes target (reflect.Value) which is the generated slice or array.
// Takes value (fuzzcase.TypedValue) which supplies bytes or collection members.
//
// Returns error when the generated collection cannot represent the value.
func assignCollection(target reflect.Value, value fuzzcase.TypedValue) error {
	if target.Kind() == reflect.Slice && target.Type().Elem().Kind() == reflect.Uint8 && value.Kind != kindArray {
		target.SetBytes([]byte(valueText(value)))
		return nil
	}
	if value.Kind != kindArray {
		return fmt.Errorf("generated %s requires a collection", target.Type())
	}
	if target.Kind() == reflect.Array && target.Len() != len(value.Elements) {
		return errors.New("generated array has the wrong length")
	}
	if target.Kind() == reflect.Slice {
		target.Set(reflect.MakeSlice(target.Type(), len(value.Elements), len(value.Elements)))
	}
	for index, element := range value.Elements {
		converted, err := Argument(target.Type().Elem(), element)
		if err != nil {
			return err
		}
		target.Index(index).Set(converted)
	}
	return nil
}

// observeScalar records a generated value according to the independent family.
//
// Takes result (*fuzzcase.TypedValue) which carries the expected family.
// Takes value (reflect.Value) which holds the dereferenced generated value.
//
// Returns error when the observed representation cannot be recorded exactly.
func observeScalar(result *fuzzcase.TypedValue, value reflect.Value) error {
	switch result.Kind {
	case kindSigned, kindUnsigned, kindFloat, kindDecimal, kindBoolean:
		return observeNumber(result, value)
	case kindText, kindUUID:
		result.Text = observedText(value)
		return nil
	case kindBinary:
		return observeBinary(result, value)
	case kindDate, kindTimestamp, kindTimestampTZ:
		return observeTime(result, value)
	case kindJSON:
		return observeJSON(result, value)
	case kindArray:
		return observeArray(result, value)
	default:
		return fmt.Errorf("unknown observed value family %q", result.Kind)
	}
}

// observeNumber records one native numeric representation without rounding.
//
// Takes result (*fuzzcase.TypedValue) which holds the independently selected family.
// Takes value (reflect.Value) which holds the actual generated field.
//
// Returns error when the field does not preserve the declared representation.
func observeNumber(result *fuzzcase.TypedValue, value reflect.Value) error {
	switch result.Kind {
	case kindSigned:
		if !value.CanInt() {
			return errors.New("signed result has a non-integer Go type")
		}
		result.Number = strconv.FormatInt(value.Int(), scalarBase)
	case kindUnsigned:
		if !value.CanUint() {
			var err error
			result.Number, err = observedUnsigned(observedText(value))
			return err
		}
		result.Number = strconv.FormatUint(value.Uint(), scalarBase)
	case kindFloat:
		if !value.CanFloat() {
			return errors.New("floating-point result has a non-floating Go type")
		}
		result.Bits = math.Float64bits(value.Float())
	case kindDecimal:
		decimal, err := observedDecimal(observedText(value))
		if err != nil {
			return err
		}
		*result = decimal
	case kindBoolean:
		if value.Kind() != reflect.Bool {
			return errors.New("boolean result has a non-Boolean Go type")
		}
		if value.Bool() {
			result.Number = numberOne
		}
	}
	return nil
}

// observeBinary records one native binary representation without rounding.
//
// Takes result (*fuzzcase.TypedValue) which holds the independently selected family.
// Takes value (reflect.Value) which holds the actual generated field.
//
// Returns error when the field does not preserve the declared representation.
func observeBinary(result *fuzzcase.TypedValue, value reflect.Value) error {
	if value.Kind() == reflect.String {
		result.Bytes = []byte(value.String())
	} else if value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Uint8 {
		result.Bytes = append([]byte{}, value.Bytes()...)
	} else {
		return errors.New("binary result has a non-binary Go type")
	}
	return nil
}

// observeJSON records one native JSON representation without rounding.
//
// Takes result (*fuzzcase.TypedValue) which holds the independently selected family.
// Takes value (reflect.Value) which holds the actual generated field.
//
// Returns error when the field does not preserve the declared representation.
func observeJSON(result *fuzzcase.TypedValue, value reflect.Value) error {
	if value.Kind() == reflect.String {
		result.Text = value.String()
	} else if value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Uint8 {
		result.Text = string(value.Bytes())
	} else {
		encoded, err := json.Marshal(value.Interface())
		if err != nil {
			return err
		}
		result.Text = string(encoded)
	}
	if !json.Valid([]byte(result.Text)) {
		return errors.New("generated JSON is invalid")
	}
	return nil
}

// observeArray records one native array representation without rounding.
//
// Takes result (*fuzzcase.TypedValue) which holds the independently selected family.
// Takes value (reflect.Value) which holds the actual generated field.
//
// Returns error when the field does not preserve the declared representation.
func observeArray(result *fuzzcase.TypedValue, value reflect.Value) error {
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return errors.New("array result has a non-collection Go type")
	}
	for index := range value.Len() {
		member, err := Observed(value.Index(index), kindSigned, false)
		if err != nil {
			return err
		}
		result.Elements = append(result.Elements, member)
	}
	return nil
}

// observeTime preserves calendar dates, wall times and timestamp instants separately.
//
// Takes result (*fuzzcase.TypedValue) which identifies the temporal family.
// Takes value (reflect.Value) which holds a native time or declared text representation.
//
// Returns error when a native temporal value has an unsupported representation.
func observeTime(result *fuzzcase.TypedValue, value reflect.Value) error {
	if moment, ok := reflect.TypeAssert[time.Time](value); ok {
		switch result.Kind {
		case kindDate:
			result.Number = moment.Format(time.DateOnly)
		case kindTimestamp:
			result.Number = moment.Format(wallClockLayout)
		case kindTimestampTZ:
			result.Number = moment.UTC().Format(time.RFC3339Nano)
		}
		return nil
	}
	if value.Kind() != reflect.String {
		return errors.New("temporal result is neither time.Time nor text")
	}
	result.Number = value.String()
	return nil
}

// scannerInput supplies lossless values to generated scanner types.
//
// Takes value (fuzzcase.TypedValue) which supplies the independent scalar.
//
// Returns any which is accepted without an intermediate floating-point conversion.
// Returns error when the scalar cannot be represented directly.
func scannerInput(value fuzzcase.TypedValue) (any, error) {
	switch value.Kind {
	case kindSigned, kindUnsigned, kindDecimal, kindUUID, kindJSON:
		return valueText(value), nil
	default:
		return value.Scalar()
	}
}

// valueText preserves the textual representation of a scalar family.
//
// Takes value (fuzzcase.TypedValue) which holds the independent scalar.
//
// Returns string which retains exact digits or binary bytes.
func valueText(value fuzzcase.TypedValue) string {
	switch value.Kind {
	case kindText, kindUUID, kindJSON:
		return value.Text
	case kindBinary:
		return string(value.Bytes)
	case kindDecimal:
		return value.Decimal()
	default:
		return value.Number
	}
}

// observedText reads actual text or a native type's exact String representation.
//
// Takes value (reflect.Value) which holds a generated scalar.
//
// Returns string which contains its textual representation.
func observedText(value reflect.Value) string {
	if value.Kind() == reflect.String {
		return value.String()
	}
	if value.CanAddr() {
		if text, ok := reflect.TypeAssert[fmt.Stringer](value.Addr()); ok {
			return text.String()
		}
	}
	if text, ok := reflect.TypeAssert[fmt.Stringer](value); ok {
		return text.String()
	}
	return fmt.Sprint(value.Interface())
}

// valueTime parses an independently specified temporal value.
//
// Takes value (fuzzcase.TypedValue) which carries the temporal family and spelling.
//
// Returns time.Time which preserves the calendar, wall time or instant.
// Returns error when the family or spelling is invalid.
func valueTime(value fuzzcase.TypedValue) (time.Time, error) {
	layout := wallClockLayout
	switch value.Kind {
	case kindDate:
		layout = time.DateOnly
	case kindTimestamp:
		layout = wallClockLayout
	case kindTimestampTZ:
		layout = time.RFC3339Nano
	default:
		return time.Time{}, errors.New("non-temporal argument mapped to time.Time")
	}
	return time.Parse(layout, value.Number)
}

// followValue dereferences bounded pointer, interface and generated Null[T] chains.
//
// A chain longer than followDepth is returned as it stands, so a later type check reports
// it.
//
// Takes value (reflect.Value) which is the generated field.
//
// Returns reflect.Value which is invalid for a nil pointer or interface or an invalid
// Null[T].
func followValue(value reflect.Value) reflect.Value {
	for depth := 0; value.IsValid() && depth < followDepth; depth++ {
		if _, wrapped := nullElement(value.Type()); wrapped {
			if !value.Field(1).Bool() {
				return reflect.Value{}
			}
			inner := value.Field(0)
			if !inner.CanAddr() {

				copied := reflect.New(inner.Type()).Elem()
				copied.Set(inner)
				inner = copied
			}
			value = inner
			continue
		}
		if value.Kind() != reflect.Pointer && value.Kind() != reflect.Interface {
			return value
		}
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

// nilValue identifies a missing value without conflating it with zero or empty data.
//
// Takes value (reflect.Value) which is a generated field after pointer traversal.
//
// Returns bool which reports an invalid or nil collection value.
func nilValue(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		return value.IsNil()
	default:
		return false
	}
}

// observedBoolean accepts native Boolean and exact zero-or-one integer results.
//
// Takes value (reflect.Value) which holds an independently selected predicate result.
//
// Returns bool which retains the native truth value.
// Returns error when the result is not a Boolean representation.
func observedBoolean(value reflect.Value) (bool, error) {
	value = followValue(value)
	if !value.IsValid() {
		return false, errors.New("boolean result is NULL")
	}
	switch {
	case value.Kind() == reflect.Bool:
		return value.Bool(), nil
	case value.CanInt() && (value.Int() == 0 || value.Int() == 1):
		return value.Int() == 1, nil
	case value.CanUint() && (value.Uint() == 0 || value.Uint() == 1):
		return value.Uint() == 1, nil
	default:
		return false, errors.New("result is not an exact Boolean")
	}
}

// nullElement recognises the generated package's Null[T] value type. It is a struct named
// Null[...] whose only fields are V and a Boolean Valid.
//
// Takes target (reflect.Type) which is a generated parameter or field type.
//
// Returns reflect.Type which is T when target is a Null[T].
// Returns bool which reports whether target is a Null[T].
func nullElement(target reflect.Type) (reflect.Type, bool) {
	if target.Kind() != reflect.Struct || !strings.HasPrefix(target.Name(), "Null[") || target.NumField() != 2 {
		return nil, false
	}
	value, valid := target.Field(0), target.Field(1)
	if value.Name != "V" || valid.Name != "Valid" || valid.Type.Kind() != reflect.Bool {
		return nil, false
	}
	return value.Type, true
}

// signedInteger converts an unsigned value that fits int64.
//
// Takes value (uint64) which is a generated unsigned integer.
//
// Returns int64 which holds the same number.
// Returns error when the value exceeds math.MaxInt64.
func signedInteger(value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("unsigned value %d exceeds the signed integer range", value)
	}
	return int64(value), nil
}
