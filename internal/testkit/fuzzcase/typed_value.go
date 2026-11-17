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
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// TypedValue preserves a scalar or collection without lossy JSON numbers.
type TypedValue struct {
	// Kind identifies the independently modelled SQL value family.
	Kind string

	// Number retains an integer coefficient or temporal spelling.
	Number string

	// Text retains textual data without conflating it with SQL NULL.
	Text string

	// Bytes retains arbitrary binary data.
	Bytes []byte

	// Elements holds ordered collection members.
	Elements []TypedValue

	// Bits retains the exact binary representation of a floating-point value.
	Bits uint64

	// Scale gives the number of decimal places in Number.
	Scale int

	// Null distinguishes SQL NULL from empty values.
	Null bool
}

// Validate rejects ambiguous, oversized or malformed values.
//
// Returns error when a value cannot be represented by the protocol.
func (value TypedValue) Validate() error {
	return value.validate(0)
}

// Decimal writes a coefficient at its exact decimal scale.
//
// Returns string which preserves every decimal digit.
func (value TypedValue) Decimal() string {
	negative := strings.HasPrefix(value.Number, typedMinus)
	digits := strings.TrimPrefix(value.Number, typedMinus)
	if value.Scale <= 0 {
		digits += strings.Repeat("0", -value.Scale)
	} else {
		if len(digits) <= value.Scale {
			digits = strings.Repeat("0", value.Scale-len(digits)+1) + digits
		}
		position := len(digits) - value.Scale
		digits = digits[:position] + "." + digits[position:]
	}
	if negative {
		return typedMinus + digits
	}
	return digits
}

// Scalar supplies a lossless value suitable for a scalar driver binding.
//
// Returns any which retains the native scalar representation.
// Returns error when the value requires a collection adapter or is malformed.
func (value TypedValue) Scalar() (any, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	if value.Null {
		return nil, nil
	}
	switch value.Kind {
	case typedSigned:
		number, err := strconv.ParseInt(value.Number, decimalBase, scalarBitWidth)
		if err != nil {
			return nil, fmt.Errorf("reading signed value: %w", err)
		}
		return number, nil
	case typedUnsigned:
		number, err := strconv.ParseUint(value.Number, decimalBase, scalarBitWidth)
		if err != nil {
			return nil, fmt.Errorf("reading unsigned value: %w", err)
		}
		return number, nil
	case typedDecimal:
		return value.Decimal(), nil
	case typedFloat:
		return math.Float64frombits(value.Bits), nil
	case typedBoolean:
		return value.Number == "1", nil
	case typedBinary:
		return append([]byte{}, value.Bytes...), nil
	case typedDate, typedTimestamp:
		return value.Number, nil
	case typedTimestamptz:
		instant, err := time.Parse(time.RFC3339Nano, value.Number)
		if err != nil {
			return nil, fmt.Errorf("reading zoned timestamp: %w", err)
		}
		return instant, nil
	case typedText, typedUUID, typedJSON:
		return value.Text, nil
	default:
		return nil, errors.New("collection values require an engine adapter")
	}
}

// Literal emits an independently escaped scalar SQL literal.
//
// Takes engine (string) which selects escaping and binary notation.
//
// Returns string which contains the scalar literal.
// Returns error when the literal needs a dialect-specific type constructor.
func (value TypedValue) Literal(engine string) (string, error) {
	if err := value.Validate(); err != nil {
		return "", err
	}
	if value.Null {
		return "NULL", nil
	}
	switch value.Kind {
	case typedSigned, typedUnsigned:
		return value.Number, nil
	case typedDecimal:
		return value.Decimal(), nil
	case typedFloat:
		number := math.Float64frombits(value.Bits)
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return "", errors.New("non-finite float requires an engine constructor")
		}
		if engine == engineClickHouse {
			return "reinterpretAsFloat64(toUInt64('" + strconv.FormatUint(value.Bits, decimalBase) + "'))", nil
		}
		return strconv.FormatFloat(number, 'e', -1, scalarBitWidth), nil
	case typedBoolean:
		if value.Number == "1" {
			return "TRUE", nil
		}
		return "FALSE", nil
	case typedBinary:
		return (Value{Kind: valueBinary, Text: string(value.Bytes), Integer: 0}).literal(engine), nil
	case typedText, typedUUID, typedJSON:
		return (Value{Kind: valueText, Text: value.Text, Integer: 0}).literal(engine), nil
	case typedDate, typedTimestamp, typedTimestamptz:
		temporal, err := value.temporalBinding(engine)
		if err != nil {
			return "", err
		}
		return typedQuote + temporal + typedQuote, nil
	case typedArray:
		return value.arrayLiteral(engine)
	default:
		return "", errors.New("collection literal requires an engine adapter")
	}
}

// Binding supplies a value using an engine's admitted parameter representation.
//
// Takes engine (string) which selects collection and binary adapters.
//
// Returns any which preserves the independently modelled value.
// Returns error when the representation is unsupported or malformed.
func (value TypedValue) Binding(engine string) (any, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	if value.Null {
		return nil, nil
	}
	switch value.Kind {
	case typedArray:
		return value.arrayBinding(engine)
	case typedUnsigned:
		if postgresFamily(engine) || engine == engineDuckDB {
			return value.Number, nil
		}
	case typedBinary:
		if engine == engineClickHouse {
			return clickHouseParameterText(string(value.Bytes)), nil
		}
	case typedText:
		if engine == engineClickHouse {
			return clickHouseParameterText(value.Text), nil
		}
	case typedTimestamptz:
		if engine == engineClickHouse {
			return value.temporalBinding(engine)
		}
	}
	return value.Scalar()
}

// PositionalBinding supplies a value before a driver's SQL literal formatting.
//
// Takes engine (string) which selects native positional conversion.
//
// Returns any which keeps raw text separate from the named parameter wire format.
// Returns error when the value cannot be represented by the driver.
func (value TypedValue) PositionalBinding(engine string) (any, error) {
	if engine != engineClickHouse {
		return value.Binding(engine)
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	if value.Null {
		return nil, nil
	}
	switch value.Kind {
	case typedText:
		return value.Text, nil
	case typedBinary:
		return string(value.Bytes), nil
	case typedFloat:
		return strconv.FormatFloat(math.Float64frombits(value.Bits), 'g', -1, scalarBitWidth), nil
	default:
		return value.Binding(engine)
	}
}

// arrayLiteral preserves element order with independent native notation.
//
// Takes engine (string) which selects native syntax and binding rules.
//
// Returns string which preserves collection order in SQL.
// Returns error when an element cannot render as SQL.
func (value TypedValue) arrayLiteral(engine string) (string, error) {
	literals := make([]string, len(value.Elements))
	for index, element := range value.Elements {
		literal, err := element.Literal(engine)
		if err != nil {
			return "", err
		}
		literals[index] = literal
	}
	prefix := ""
	if postgresFamily(engine) {
		prefix = "ARRAY"
	}
	return prefix + "[" + strings.Join(literals, ", ") + "]", nil
}

// arrayBinding uses the admitted integer collection representation.
//
// Takes engine (string) which selects native syntax and binding rules.
//
// Returns any which holds the admitted native collection representation.
// Returns error when elements cannot bind as signed integers.
func (value TypedValue) arrayBinding(engine string) (any, error) {
	numbers := make([]int64, len(value.Elements))
	for index, element := range value.Elements {
		if element.Kind != typedSigned || element.Null {
			return nil, errors.New("array adapter requires non-null signed integers")
		}
		number, err := strconv.ParseInt(element.Number, decimalBase, scalarBitWidth)
		if err != nil {
			return nil, fmt.Errorf("reading array element: %w", err)
		}
		numbers[index] = number
	}
	if engine == engineClickHouse {
		return numbers, nil
	}
	literal, err := value.arrayLiteral(engine)
	if err != nil {
		return nil, err
	}
	if body, ok := strings.CutPrefix(literal, "ARRAY["); ok {
		return "{" + strings.TrimSuffix(body, "]") + "}", nil
	}
	return literal, nil
}

// temporalBinding retains wall time or converts a zoned value to native UTC notation.
//
// Takes engine (string) which selects native syntax and binding rules.
//
// Returns string which retains wall time or native UTC notation.
// Returns error when a zoned timestamp is malformed.
func (value TypedValue) temporalBinding(engine string) (string, error) {
	if engine != engineClickHouse || value.Kind != typedTimestamptz {
		return value.Number, nil
	}
	instant, err := time.Parse(time.RFC3339Nano, value.Number)
	if err != nil {
		return "", fmt.Errorf("reading zoned timestamp: %w", err)
	}
	return instant.UTC().Format(typedUTCTimestampLayout), nil
}

// TypedNumber constructs an exact numeric value.
//
// Takes kind (string) which selects the numeric family.
// Takes number (string) which holds its coefficient.
// Takes scale (int) which records decimal places.
//
// Returns TypedValue which retains the supplied representation.
func TypedNumber(kind, number string, scale int) TypedValue {
	return TypedValue{Kind: kind, Number: number, Scale: scale, Text: "", Bytes: nil, Elements: nil, Bits: 0, Null: false}
}

// clickHouseParameterText independently escapes the server's text parameter format.
//
// Takes value (string) which retains the unescaped parameter bytes.
//
// Returns string which escapes the native named parameter format.
func clickHouseParameterText(value string) string {
	var escaped strings.Builder
	for _, char := range []byte(value) {
		switch char {
		case '\\':
			escaped.WriteString(`\\`)
		case '\t':
			escaped.WriteString(`\t`)
		case '\n':
			escaped.WriteString(`\n`)
		case '\r':
			escaped.WriteString(`\r`)
		case 0:
			escaped.WriteString(`\0`)
		default:
			escaped.WriteByte(char)
		}
	}
	return escaped.String()
}
