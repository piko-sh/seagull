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

package dialectkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

var (
	everyPrefix = dialectkit.NumberRules{HexPrefix: true, OctalPrefix: true, BinaryPrefix: true}
	strictHex   = dialectkit.NumberRules{HexPrefix: true, RequireDigitsAfterPrefix: true}
	noPrefixes  = dialectkit.NumberRules{}
)

func TestReadSeparatedNumberAllowsOneSeparatorBetweenTwoDigits(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		input     string
		separator byte
		want      int
	}{
		{name: "separated thousands", input: "1_000_000 ", separator: '_', want: 9},
		{name: "separated fraction", input: "1_0.2_5)", separator: '_', want: 7},
		{name: "separated exponent", input: "1e1_0,", separator: '_', want: 5},
		{name: "a separator ending the digits", input: "1_ ", separator: '_', want: 1},
		{name: "two separators in a row", input: "1__0", separator: '_', want: 1},
		{name: "a separator after the dot", input: "1._5", separator: '_', want: 2},
		{name: "no separator allowed", input: "1_000", separator: 0, want: 1},
		{name: "a separator ending the exponent", input: "1e1_", separator: '_', want: 3},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			end, err := noPrefixes.ReadSeparatedNumber(testCase.input, 0, testCase.separator)

			require.NoError(t, err)
			require.Equal(t, testCase.want, end)
		})
	}
}

func TestReadNumber(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		rules    dialectkit.NumberRules
		input    string
		position int
		want     int
		wantErr  bool
	}{
		{name: "integer", rules: everyPrefix, input: "42 ", want: 2},
		{name: "fraction", rules: everyPrefix, input: "4.25)", want: 4},
		{name: "leading dot", rules: everyPrefix, input: ".5 AS half", want: 2},
		{name: "trailing dot", rules: everyPrefix, input: "5. ", want: 2},
		{name: "exponent", rules: everyPrefix, input: "1e3,", want: 3},
		{name: "signed exponent", rules: everyPrefix, input: "2.5E-2 ", want: 6},
		{name: "exponent needs digits", rules: everyPrefix, input: "1e", want: 1},
		{name: "exponent sign needs digits", rules: everyPrefix, input: "1e+x", want: 1},
		{name: "exponent marker starts an identifier", rules: everyPrefix, input: "1ex", want: 1},
		{name: "hexadecimal", rules: everyPrefix, input: "0x1E+1", want: 4},
		{name: "octal", rules: everyPrefix, input: "0o17 ", want: 4},
		{name: "binary", rules: everyPrefix, input: "0b101 ", want: 5},
		{name: "prefix disabled", rules: noPrefixes, input: "0x1E", want: 1},
		{name: "bare prefix accepted", rules: everyPrefix, input: "0x ", want: 2},
		{name: "bare prefix rejected", rules: strictHex, input: "0x ", want: 0, wantErr: true},
		{name: "offset start", rules: everyPrefix, input: "a + .75", position: 4, want: 7},
		{name: "dot alone is not a number", rules: everyPrefix, input: ". 5", want: 0},
		{name: "letter is not a number", rules: everyPrefix, input: "x1", want: 0},
		{name: "past the end", rules: everyPrefix, input: "1", position: 1, want: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			end, err := testCase.rules.ReadNumber(testCase.input, testCase.position)
			if testCase.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.want, end)
		})
	}
}

func TestStartsNumber(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  bool
	}{
		{input: "7", want: true},
		{input: ".7", want: true},
		{input: ".x", want: false},
		{input: ".", want: false},
		{input: "x", want: false},
		{input: "", want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, dialectkit.StartsNumber(testCase.input, 0))
		})
	}
}

func TestClassifyNumber(t *testing.T) {
	t.Parallel()

	cases := []struct {
		text  string
		rules dialectkit.NumberRules
		want  dialectkit.NumberClass
	}{
		{text: "42", rules: everyPrefix, want: dialectkit.NumberInteger},
		{text: "0x1E", rules: everyPrefix, want: dialectkit.NumberInteger},
		{text: "0b1", rules: everyPrefix, want: dialectkit.NumberInteger},
		{text: "1.1", rules: everyPrefix, want: dialectkit.NumberExact},
		{text: ".5", rules: everyPrefix, want: dialectkit.NumberExact},
		{text: "5.", rules: everyPrefix, want: dialectkit.NumberExact},
		{text: "1e3", rules: everyPrefix, want: dialectkit.NumberApproximate},
		{text: "2.5E-2", rules: everyPrefix, want: dialectkit.NumberApproximate},
		{text: "0x1E", rules: noPrefixes, want: dialectkit.NumberInvalid},
		{text: "1e", rules: everyPrefix, want: dialectkit.NumberInvalid},
		{text: "", rules: everyPrefix, want: dialectkit.NumberInvalid},
		{text: "abc", rules: everyPrefix, want: dialectkit.NumberInvalid},
	}
	for _, testCase := range cases {
		t.Run(testCase.text, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, testCase.rules.ClassifyNumber(testCase.text))
		})
	}
}

func TestNumberClassString(t *testing.T) {
	t.Parallel()

	require.Equal(t, "integer", dialectkit.NumberInteger.String())
	require.Equal(t, "exact", dialectkit.NumberExact.String())
	require.Equal(t, "approximate", dialectkit.NumberApproximate.String())
	require.Equal(t, "invalid", dialectkit.NumberInvalid.String())
}

func TestTypeModifierMagnitudeReadsAModifierHeldAtTheBound(t *testing.T) {
	t.Parallel()

	hexOnly := dialectkit.NumberRules{HexPrefix: true}
	testCases := []struct {
		name          string
		literal       string
		rules         dialectkit.NumberRules
		wantMagnitude int
		wantOK        bool
	}{
		{name: "a decimal integer", literal: "255", rules: hexOnly, wantMagnitude: 255, wantOK: true},
		{name: "a hexadecimal integer", literal: "0x1F", rules: hexOnly, wantMagnitude: 31, wantOK: true},
		{name: "an upper-case prefix", literal: "0X1f", rules: hexOnly, wantMagnitude: 31, wantOK: true},
		{name: "an octal integer", literal: "0o17", rules: everyPrefix, wantMagnitude: 15, wantOK: true},
		{name: "a binary integer", literal: "0b101", rules: everyPrefix, wantMagnitude: 5, wantOK: true},
		{name: "a fraction is truncated", literal: "10.9", rules: hexOnly, wantMagnitude: 10, wantOK: true},
		{name: "an exponent", literal: "1e3", rules: hexOnly, wantMagnitude: 1000, wantOK: true},
		{name: "a magnitude past the bound", literal: "99999999999", rules: hexOnly, wantMagnitude: dialectkit.MaxTypeModifier, wantOK: true},
		{name: "a magnitude past int64", literal: "999999999999999999999999", rules: hexOnly, wantMagnitude: dialectkit.MaxTypeModifier, wantOK: true},
		{name: "a hexadecimal magnitude past the bound", literal: "0x7FFFFFFF", rules: hexOnly, wantMagnitude: dialectkit.MaxTypeModifier, wantOK: true},
		{name: "an exponent past the bound", literal: "1e400", rules: hexOnly, wantMagnitude: dialectkit.MaxTypeModifier, wantOK: true},
		{name: "the bound itself", literal: "1073741824", rules: hexOnly, wantMagnitude: dialectkit.MaxTypeModifier, wantOK: true},
		{name: "a bare prefix", literal: "0x", rules: hexOnly, wantMagnitude: 0, wantOK: false},
		{name: "a prefix the rules refuse", literal: "0b101", rules: hexOnly, wantMagnitude: 0, wantOK: false},
		{name: "a sign", literal: "-5", rules: hexOnly, wantMagnitude: 0, wantOK: false},
		{name: "a word", literal: "max", rules: hexOnly, wantMagnitude: 0, wantOK: false},
		{name: "nothing", literal: "", rules: hexOnly, wantMagnitude: 0, wantOK: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			magnitude, ok := testCase.rules.TypeModifierMagnitude(testCase.literal)

			require.Equal(t, testCase.wantOK, ok)
			require.Equal(t, testCase.wantMagnitude, magnitude)
		})
	}
}

func TestTryReadPrefixedNumber(t *testing.T) {
	t.Parallel()

	allBases := dialectkit.NumberRules{HexPrefix: true, OctalPrefix: true, BinaryPrefix: true}
	hexOnly := dialectkit.NumberRules{HexPrefix: true}

	cases := []struct {
		name        string
		rules       dialectkit.NumberRules
		input       string
		wantEnd     int
		wantMatched bool
	}{
		{name: "hex", rules: allBases, input: "0xDEAD", wantEnd: 6, wantMatched: true},
		{name: "octal", rules: allBases, input: "0o777", wantEnd: 5, wantMatched: true},
		{name: "binary", rules: allBases, input: "0b1010", wantEnd: 6, wantMatched: true},
		{name: "octal disabled", rules: hexOnly, input: "0o777", wantEnd: 0, wantMatched: false},
		{name: "binary disabled", rules: hexOnly, input: "0b10", wantEnd: 0, wantMatched: false},
		{name: "plain decimal not prefixed", rules: allBases, input: "123", wantEnd: 0, wantMatched: false},
		{name: "zero alone", rules: allBases, input: "0", wantEnd: 0, wantMatched: false},
		{name: "hex stops at non-hex", rules: allBases, input: "0xFFg", wantEnd: 4, wantMatched: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			end, matched, err := testCase.rules.TryReadPrefixedNumber(testCase.input, 0)
			require.NoError(t, err)
			require.Equal(t, testCase.wantMatched, matched)
			require.Equal(t, testCase.wantEnd, end)
		})
	}
}

func TestTryReadPrefixedNumberRequireDigits(t *testing.T) {
	t.Parallel()

	lenient := dialectkit.NumberRules{HexPrefix: true}
	_, matched, err := lenient.TryReadPrefixedNumber("0x", 0)
	require.NoError(t, err, "lenient bare prefix")
	require.True(t, matched, "lenient bare prefix")

	strict := dialectkit.NumberRules{HexPrefix: true, RequireDigitsAfterPrefix: true}
	end, matched, err := strict.TryReadPrefixedNumber("0x", 0)
	require.Error(t, err, "strict bare prefix")
	require.False(t, matched, "strict bare prefix: matched should be false on error")
	require.Zero(t, end, "strict bare prefix")

	_, matched, err = strict.TryReadPrefixedNumber("0xAB", 0)
	require.NoError(t, err, "strict with digits")
	require.True(t, matched, "strict with digits")
}

func TestPrefixValidatorDisabledBases(t *testing.T) {
	t.Parallel()

	hexOnly := dialectkit.NumberRules{HexPrefix: true}
	require.NotNil(t, hexOnly.PrefixValidator('x'), "hex validator should be present when HexPrefix is set")
	require.Nil(t, hexOnly.PrefixValidator('o'), "octal validator should be nil when OctalPrefix is unset")
	require.Nil(t, hexOnly.PrefixValidator('b'), "binary validator should be nil when BinaryPrefix is unset")
	require.Nil(t, hexOnly.PrefixValidator('z'), "unknown prefix should yield a nil validator")
}
