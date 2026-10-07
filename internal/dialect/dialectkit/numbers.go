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

package dialectkit

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	// MaxTypeModifier bounds the magnitude of a numeric type modifier, such as the length of
	// VARCHAR(10) or the precision and scale of DECIMAL(10, 2), so a modifier written out of
	// range is held at a sane upper bound rather than wrapping round.
	MaxTypeModifier = 1 << 30

	// decimalBase is the base of an integer literal written without a base prefix.
	decimalBase = 10

	// hexadecimalBase is the base of a "0x" literal.
	hexadecimalBase = 16

	// octalBase is the base of a "0o" literal.
	octalBase = 8

	// binaryBase is the base of a "0b" literal.
	binaryBase = 2

	// basePrefixLength is the length of a base prefix such as "0x".
	basePrefixLength = len("0x")
)

// NumberClass is the shape of a numeric literal, which decides its type: integer, exact
// decimal or approximate. A dialect maps each class to its own type names.
type NumberClass uint8

const (
	// NumberInvalid marks text that is not a numeric literal under the rules.
	NumberInvalid NumberClass = iota

	// NumberInteger marks an integer literal: decimal digits only, or a base-prefixed
	// literal such as 0x1E.
	NumberInteger

	// NumberExact marks an exact decimal literal with a fractional part and no exponent,
	// such as 1.5 or .5, which engines with an exact numeric type keep exact.
	NumberExact

	// NumberApproximate marks a literal with an exponent, such as 1e3 or 2.5E-2, which SQL
	// treats as approximate (floating point).
	NumberApproximate
)

// String returns the class's name.
//
// Returns string which is "integer", "exact", "approximate" or "invalid".
func (c NumberClass) String() string {
	switch c {
	case NumberInteger:
		return "integer"
	case NumberExact:
		return "exact"
	case NumberApproximate:
		return "approximate"
	default:
		return "invalid"
	}
}

// NumberRules captures which base-prefixed integer literals a dialect accepts. The shared
// number scanner reads these flags so one scanner serves dialects that differ only in
// their recognised bases.
type NumberRules struct {
	// HexPrefix accepts "0x" / "0X" hexadecimal integer literals.
	HexPrefix bool

	// OctalPrefix accepts "0o" / "0O" octal integer literals.
	OctalPrefix bool

	// BinaryPrefix accepts "0b" / "0B" binary integer literals.
	BinaryPrefix bool

	// RequireDigitsAfterPrefix rejects a base prefix that is followed by no digits of its
	// base (for example "0x" with nothing after it) as a lexical error, rather than
	// accepting the bare prefix as a literal. ClickHouse is strict; the other dialects
	// accept the bare prefix.
	RequireDigitsAfterPrefix bool
}

// PrefixValidator returns the digit predicate for a base-prefix byte (the byte following
// the leading "0"), or nil when the dialect does not accept that prefix.
//
// Takes prefix (byte) which is the byte that follows the leading "0".
//
// Returns func(byte) bool which validates digits for the matched base, or nil when the
// prefix is not an enabled base.
func (rules NumberRules) PrefixValidator(prefix byte) func(byte) bool {
	switch prefix {
	case 'x', 'X':
		if rules.HexPrefix {
			return IsHexDigit
		}
	case 'o', 'O':
		if rules.OctalPrefix {
			return IsOctalDigit
		}
	case 'b', 'B':
		if rules.BinaryPrefix {
			return IsBinaryDigit
		}
	}
	return nil
}

// TryReadPrefixedNumber consumes a base-prefixed integer literal ("0x", "0o", or "0b")
// that begins at position when the dialect enables that prefix.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which indexes the candidate leading "0".
//
// Returns int which is the offset after the literal, or position unchanged when no
// enabled prefix matched.
// Returns bool which is true when a prefixed literal was consumed.
// Returns error when RequireDigitsAfterPrefix is set and the prefix lacks digits.
func (rules NumberRules) TryReadPrefixedNumber(input string, position int) (int, bool, error) {
	if position+1 >= len(input) || input[position] != '0' {
		return position, false, nil
	}

	prefix := input[position+1]
	validator := rules.PrefixValidator(prefix)
	if validator == nil {
		return position, false, nil
	}

	startPosition := position
	position += basePrefixLength
	digitsStart := position
	for position < len(input) && validator(input[position]) {
		position++
	}

	if rules.RequireDigitsAfterPrefix && position == digitsStart {
		return startPosition, false, ParseErrorAt(input, startPosition, position, fmt.Sprintf("base-prefixed number has no digits after the \"0%c\" prefix", prefix))
	}

	return position, true, nil
}

// ReadNumber consumes a numeric literal under the configured rules.
//
// It accepts enabled base prefixes, fractions and complete exponents. An incomplete
// exponent remains outside the token.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which indexes the literal's first byte, as StartsNumber reports.
//
// Returns int which is the offset after the literal, or position when none begins there.
// Returns error when RequireDigitsAfterPrefix is set and a base prefix has no digits.
func (rules NumberRules) ReadNumber(input string, position int) (int, error) {
	return rules.ReadSeparatedNumber(input, position, 0)
}

// ReadSeparatedNumber consumes a numeric literal as ReadNumber does, except that in its
// decimal digits, fraction and exponent a single separator byte may stand between two
// digits, as in DuckDB's 1_000_000. The separator stays part of the literal's text.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which indexes the literal's first byte, as StartsNumber reports.
// Takes separator (byte) which may stand between two digits; zero allows none, as
// ReadNumber does.
//
// Returns int which is the offset after the literal, or position when none begins there.
// Returns error when RequireDigitsAfterPrefix is set and a base prefix has no digits.
func (rules NumberRules) ReadSeparatedNumber(input string, position int, separator byte) (int, error) {
	if !StartsNumber(input, position) {
		return position, nil
	}
	end, matched, err := rules.TryReadPrefixedNumber(input, position)
	if err != nil {
		return position, err
	}
	if matched {
		return end, nil
	}
	end = consumeSeparatedDigits(input, position, separator)
	if end < len(input) && input[end] == '.' {
		end = consumeSeparatedDigits(input, end+1, separator)
	}
	return consumeExponent(input, end, separator), nil
}

// ClassifyNumber classifies a numeric literal by its scanned shape rather than by
// searching its text, so a hexadecimal literal holding the digit E stays an integer.
//
// Takes text (string) which is the literal as ReadNumber consumed it.
//
// Returns NumberClass which is the literal's class, or NumberInvalid when text is not one
// whole literal under the rules.
func (rules NumberRules) ClassifyNumber(text string) NumberClass {
	end, err := rules.ReadNumber(text, 0)
	if err != nil || end == 0 || end != len(text) {
		return NumberInvalid
	}
	if _, prefixed, _ := rules.TryReadPrefixedNumber(text, 0); prefixed {
		return NumberInteger
	}
	switch {
	case strings.ContainsAny(text, "eE"):
		return NumberApproximate
	case strings.Contains(text, "."):
		return NumberExact
	default:
		return NumberInteger
	}
}

// TypeModifierMagnitude reads the magnitude of a numeric type modifier, such as the 10 of
// VARCHAR(10), from its literal: an integer, decimal or base-prefixed as the rules
// accept, or a fraction or exponent, which is truncated. A magnitude past MaxTypeModifier
// is held at it.
//
// Takes literal (string) which is the modifier's number as written, without its sign.
//
// Returns int which is the magnitude, at most MaxTypeModifier.
// Returns bool which is false when literal is not one whole number under the rules, or is
// a base prefix with no digits after it.
func (rules NumberRules) TypeModifierMagnitude(literal string) (int, bool) {
	switch rules.ClassifyNumber(literal) {
	case NumberInteger:
		return rules.integerMagnitude(literal)
	case NumberExact, NumberApproximate:
		value, err := strconv.ParseFloat(literal, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return 0, false
		}
		return int(min(math.Trunc(value), MaxTypeModifier)), true
	default:
		return 0, false
	}
}

// integerMagnitude reads an integer literal, decimal or base-prefixed, holding its value
// at MaxTypeModifier rather than letting it overflow.
//
// Takes literal (string) which is the integer as written, which ClassifyNumber accepts.
//
// Returns int which is the value, at most MaxTypeModifier.
// Returns bool which is false when the literal holds no digits after its prefix, or a
// digit its base does not have.
func (rules NumberRules) integerMagnitude(literal string) (int, bool) {
	base, digits := decimalBase, literal
	if len(literal) >= basePrefixLength && literal[0] == '0' && rules.PrefixValidator(literal[1]) != nil {
		base, digits = prefixBase(literal[1]), literal[basePrefixLength:]
	}
	value, err := strconv.ParseInt(digits, base, 64)
	switch {
	case errors.Is(err, strconv.ErrRange):
		return MaxTypeModifier, true
	case err != nil:
		return 0, false
	default:
		return int(min(value, MaxTypeModifier)), true
	}
}

// StartsNumber reports whether a numeric literal begins at position: a decimal digit, or
// a '.' followed by a digit such as the leading-dot literal .5.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which is the candidate start offset.
//
// Returns bool which is true when ReadNumber would consume a literal there.
func StartsNumber(input string, position int) bool {
	if position >= len(input) {
		return false
	}
	if IsDigit(input[position]) {
		return true
	}
	return input[position] == '.' && position+1 < len(input) && IsDigit(input[position+1])
}

// prefixBase returns the base a base-prefix byte selects.
//
// Takes prefix (byte) which is the byte that follows the leading "0".
//
// Returns int which is 16, 8 or 2, or 10 for a byte that selects no base.
func prefixBase(prefix byte) int {
	switch prefix {
	case 'x', 'X':
		return hexadecimalBase
	case 'o', 'O':
		return octalBase
	case 'b', 'B':
		return binaryBase
	default:
		return decimalBase
	}
}

// consumeSeparatedDigits advances past a run of decimal digits, the run going on over a
// single separator byte that stands between two digits.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which is where the run may begin.
// Takes separator (byte) which may stand between two digits; zero allows none.
//
// Returns int which is the offset after the run.
func consumeSeparatedDigits(input string, position int, separator byte) int {
	for position < len(input) {
		switch {
		case IsDigit(input[position]):
			position++
		case separator != 0 && input[position] == separator && position > 0 && IsDigit(input[position-1]) &&
			position+1 < len(input) && IsDigit(input[position+1]):
			position++
		default:
			return position
		}
	}
	return position
}

// consumeExponent advances past an exponent, an 'e' or 'E' with an optional sign and at
// least one digit, leaving position unchanged when no complete exponent begins there.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which is where the exponent may begin.
// Takes separator (byte) which may stand between two of its digits; zero allows none.
//
// Returns int which is the offset after the exponent, or position.
func consumeExponent(input string, position int, separator byte) int {
	if position >= len(input) || (input[position] != 'e' && input[position] != 'E') {
		return position
	}
	digits := position + 1
	if digits < len(input) && (input[digits] == '+' || input[digits] == '-') {
		digits++
	}
	if digits >= len(input) || !IsDigit(input[digits]) {
		return position
	}
	return consumeSeparatedDigits(input, digits, separator)
}
