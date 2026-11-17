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
	"strconv"
	"strings"
)

// Value records an exact scalar without depending on a driver or SQL type parser.
type Value struct {
	// Text retains UTF-8 text or arbitrary binary bytes without JSON conversion.
	Text string

	// Kind distinguishes text, binary and NULL from the default integer representation.
	Kind string

	// Integer contains an exact signed numeric value.
	Integer int64
}

// valueText identifies a textual scalar.
const valueText = "text"

// valueBinary identifies an arbitrary byte sequence.
const valueBinary = "binary"

// valueNull identifies SQL NULL independently of string contents.
const valueNull = "null"

// valueQuote delimits text and binary literals.
const valueQuote = "'"

// DriverValue supplies the model's exact value at the driver boundary.
//
// Takes engine (string) which selects the binary representation of ClickHouse String.
//
// Returns any which holds an integer, string, byte slice or nil.
func (value Value) DriverValue(engine string) any {
	switch value.Kind {
	case valueNull:
		return nil
	case valueText:
		return value.Text
	case valueBinary:
		if engine == engineClickHouse {
			return value.Text
		}
		return []byte(value.Text)
	default:
		return value.Integer
	}
}

// literal writes an independent SQL representation of one exact scalar.
//
// Takes engine (string) which selects literal escaping and binary syntax.
//
// Returns string which can appear as an expression in reference SQL.
func (value Value) literal(engine string) string {
	switch value.Kind {
	case valueNull:
		return "NULL"
	case valueText:
		text := strings.ReplaceAll(value.Text, valueQuote, "''")
		if mysqlFamily(engine) || engine == engineClickHouse {
			text = strings.ReplaceAll(text, `\`, `\\`)
		}
		return valueQuote + text + valueQuote
	case valueBinary:
		encoded := hex.EncodeToString([]byte(value.Text))
		switch engine {
		case enginePostgres, engineCockroach, engineTimescale:
			return "decode('" + encoded + "', 'hex')"
		case engineClickHouse:
			return "unhex('" + encoded + "')"
		case engineDuckDB:
			return "from_hex('" + encoded + "')"
		default:
			return "X'" + encoded + valueQuote
		}
	default:
		return strconv.FormatInt(value.Integer, decimalBase)
	}
}

// integerValues retains the numeric interpretation of earlier decoder versions.
//
// Takes numbers ([]int64) which contains original binding or occurrence values.
//
// Returns []Value which contains the same exact integers in the same order.
func integerValues(numbers []int64) []Value {
	if numbers == nil {
		return nil
	}
	values := make([]Value, len(numbers))
	for index, number := range numbers {
		values[index] = Value{Integer: number, Text: "", Kind: ""}
	}
	return values
}
