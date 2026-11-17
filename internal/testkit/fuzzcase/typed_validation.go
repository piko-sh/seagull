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

package fuzzcase

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"
)

// scalarValidators checks the representation of each scalar family that has a spelling
// rule.
var scalarValidators = map[string]func(value TypedValue) error{
	typedSigned:      validateSigned,
	typedUnsigned:    validateUnsigned,
	typedDecimal:     validateDecimal,
	typedBoolean:     validateBoolean,
	typedDate:        validateLayout(time.DateOnly, "date"),
	typedTimestamp:   validateLayout(typedTimestampLayout, "timestamp"),
	typedTimestamptz: validateLayout(time.RFC3339Nano, "zoned timestamp"),
	typedUUID:        TypedValue.validateUUID,
	typedJSON:        validateJSON,
}

// typedUUIDPartLengths retains each component of a canonical UUID.
var typedUUIDPartLengths = [...]int{8, 4, 4, 4, 12}

// validate checks bounded collections and scalar representations.
//
// Takes depth (int) which records collection nesting.
//
// Returns error when a value exceeds its bounds or violates its family contract.
func (value TypedValue) validate(depth int) error {
	if depth > typedMaxDepth || value.exceedsBounds() {
		return errors.New("typed value exceeds its bounds")
	}
	if value.Scale < -typedScaleBound || value.Scale > typedScaleBound {
		return errors.New("decimal scale exceeds its bounds")
	}
	if !slices.Contains(typedKinds, value.Kind) {
		return fmt.Errorf("unknown typed value family %q", value.Kind)
	}
	if value.Null {
		return nil
	}
	if value.Kind == typedArray {
		for _, element := range value.Elements {
			if err := element.validate(depth + 1); err != nil {
				return err
			}
		}
		return nil
	}
	return value.validateScalar()
}

// validateScalar checks the spelling and range of a non-null scalar.
//
// Returns error when a non-null scalar has an invalid representation.
func (value TypedValue) validateScalar() error {
	validate, ok := scalarValidators[value.Kind]
	if !ok {
		return nil
	}
	return validate(value)
}

// validateUUID requires exact group widths and hexadecimal digits.
//
// Returns error when UUID groups or hexadecimal digits are invalid.
func (value TypedValue) validateUUID() error {
	parts := strings.Split(value.Text, typedMinus)
	if len(parts) != len(typedUUIDPartLengths) {
		return errors.New("invalid UUID spelling")
	}
	for index, length := range typedUUIDPartLengths {
		if len(parts[index]) != length {
			return errors.New("invalid UUID group")
		}
	}
	if _, err := hex.DecodeString(strings.Join(parts, "")); err != nil {
		return fmt.Errorf("invalid UUID digits: %w", err)
	}
	return nil
}

// exceedsBounds reports whether a value holds more data than the protocol admits.
//
// Text is measured in bytes because the bound limits the encoded payload.
//
// Returns bool which is true when a collection, payload or number is too large.
func (value TypedValue) exceedsBounds() bool {
	return len(value.Elements) > typedCollectionBound || len(value.Bytes) > typedScalarBound ||
		len(value.Text) > typedScalarBound || len(value.Number) > typedNumberBound
}

// validateSigned requires a 64-bit signed integer.
//
// Takes value (TypedValue) which holds the integer spelling.
//
// Returns error when the spelling is not a signed 64-bit integer.
func validateSigned(value TypedValue) error {
	if _, err := strconv.ParseInt(value.Number, decimalBase, scalarBitWidth); err != nil {
		return fmt.Errorf("invalid signed value: %w", err)
	}
	return nil
}

// validateUnsigned requires a 64-bit unsigned integer.
//
// Takes value (TypedValue) which holds the integer spelling.
//
// Returns error when the spelling is not an unsigned 64-bit integer.
func validateUnsigned(value TypedValue) error {
	if _, err := strconv.ParseUint(value.Number, decimalBase, scalarBitWidth); err != nil {
		return fmt.Errorf("invalid unsigned value: %w", err)
	}
	return nil
}

// validateDecimal requires an integer coefficient of any size.
//
// Takes value (TypedValue) which holds the coefficient.
//
// Returns error when the coefficient is not an integer.
func validateDecimal(value TypedValue) error {
	if _, ok := new(big.Int).SetString(value.Number, decimalBase); !ok {
		return errors.New("invalid decimal coefficient")
	}
	return nil
}

// validateBoolean requires zero or one.
//
// Takes value (TypedValue) which holds the Boolean spelling.
//
// Returns error when the spelling is neither zero nor one.
func validateBoolean(value TypedValue) error {
	if value.Number != typedZero && value.Number != "1" {
		return errors.New("invalid Boolean value")
	}
	return nil
}

// validateJSON requires a valid JSON document.
//
// Takes value (TypedValue) which holds the document text.
//
// Returns error when the text is not valid JSON.
func validateJSON(value TypedValue) error {
	if !json.Valid([]byte(value.Text)) {
		return errors.New("invalid JSON value")
	}
	return nil
}

// validateLayout builds a check that a temporal spelling follows a layout.
//
// Takes layout (string) which is the time layout.
// Takes name (string) which names the family in errors.
//
// Returns func(TypedValue) error which checks one value.
func validateLayout(layout, name string) func(value TypedValue) error {
	return func(value TypedValue) error {
		if _, err := time.Parse(layout, value.Number); err != nil {
			return fmt.Errorf("invalid %s: %w", name, err)
		}
		return nil
	}
}
