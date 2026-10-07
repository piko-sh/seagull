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

package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestFormatTypeModifiersWritesTheListSplitTypeModifiersReads(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		modifiers []int
		want      string
	}{
		{name: "no modifiers", modifiers: nil, want: ""},
		{name: "a length", modifiers: []int{20}, want: "(20)"},
		{name: "a precision and a scale", modifiers: []int{10, 2}, want: "(10,2)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			written := formatTypeModifiers(testCase.modifiers)

			assert.Equal(t, testCase.want, written)
			if len(testCase.modifiers) == 0 {
				return
			}
			base, modifiers, found := splitTypeModifiers("decimal" + written)
			assert.True(t, found)
			assert.Equal(t, "decimal", base)
			assert.Equal(t, testCase.modifiers, modifiers)
		})
	}
}

func TestSplitTypeModifiersSeparatesANumericListFromTheName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		input         string
		wantBase      string
		wantModifiers []int
		wantFound     bool
	}{
		{name: "a precision and a scale", input: "decimal(10,2)", wantBase: "decimal", wantModifiers: []int{10, 2},
			wantFound: true},
		{name: "spaces around every part", input: "  CHARACTER VARYING ( 5 )  ", wantBase: "CHARACTER VARYING",
			wantModifiers: []int{5}, wantFound: true},
		{name: "a modifier past the largest one", input: "varchar(4294967296)", wantBase: "varchar",
			wantModifiers: []int{dialectkit.MaxTypeModifier}, wantFound: true},
		{name: "a name with no list", input: "decimal"},
		{name: "a list with no name", input: " (10)"},
		{name: "a list left open", input: "decimal(10"},
		{name: "a closing parenthesis with no opening one", input: "decimal10)"},
		{name: "three modifiers", input: "decimal(1,2,3)"},
		{name: "an empty list", input: "decimal()"},
		{name: "an empty modifier", input: "decimal(,2)"},
		{name: "a fraction is truncated", input: "decimal(10.5,2)", wantBase: "decimal", wantModifiers: []int{10, 2},
			wantFound: true},
		{name: "an exponent", input: "varchar(1e3)", wantBase: "varchar", wantModifiers: []int{1000}, wantFound: true},
		{name: "a hexadecimal modifier", input: "varchar(0x1F)", wantBase: "varchar", wantModifiers: []int{31},
			wantFound: true},
		{name: "a signed modifier", input: "decimal(-1)"},
		{name: "a word", input: "decimal(x)"},
		{name: "a nested list", input: "decimal((10))"},
		{name: "values rather than numbers", input: "enum('a','b')"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			base, modifiers, found := splitTypeModifiers(testCase.input)

			assert.Equal(t, testCase.wantFound, found)
			assert.Equal(t, testCase.wantBase, base)
			assert.Equal(t, testCase.wantModifiers, modifiers)
		})
	}
}

func TestNormaliseWrittenResolvesAWrittenListAsTheModifiersItHolds(t *testing.T) {
	t.Parallel()

	hooked := newTypeResolver([]func(string, []int) *sqlmodel.SQLType{
		func(name string, modifiers []int) *sqlmodel.SQLType {
			if name != "money" || len(modifiers) != 2 {
				return nil
			}
			money := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "money")
			money.Precision, money.Scale = new(modifiers[0]), new(modifiers[1])
			return &money
		},
	}, nil)

	testCases := []struct {
		name          string
		resolver      *typeResolver
		input         string
		wantName      string
		wantModifiers []int
	}{
		{name: "a precision and a scale", resolver: newTypeResolver(nil, nil), input: "DECIMAL(10,2)",
			wantName: "decimal", wantModifiers: []int{10, 2}},
		{name: "a length through the built-in types alone", resolver: nil, input: "char(20)",
			wantName: "char", wantModifiers: []int{20}},
		{name: "a hook is asked with the modifiers apart from the name", resolver: hooked, input: "money(12, 4)",
			wantName: "money", wantModifiers: []int{12, 4}},
		{name: "a name with no list", resolver: newTypeResolver(nil, nil), input: "datetime", wantName: "datetime"},
		{name: "a list that holds no modifiers is part of the name", resolver: newTypeResolver(nil, nil),
			input: "decimal(1,2,3)", wantName: "decimal(1,2,3)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.resolver.normalise(testCase.wantName, testCase.wantModifiers...),
				testCase.resolver.normaliseWritten(testCase.input))
		})
	}
}

func TestAFloatGivenAPrecisionPastTwentyFourBitsIsADouble(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		typeName       string
		modifiers      []int
		wantEngineName string
		wantWidth      int
	}{
		{name: "a float with no precision", typeName: "float", wantEngineName: "float", wantWidth: widthFloat},
		{name: "a float of no bits of precision", typeName: "float", modifiers: []int{0}, wantEngineName: "float",
			wantWidth: widthFloat},
		{name: "a float of twenty-four bits of precision", typeName: "float", modifiers: []int{maxFloatPrecision},
			wantEngineName: "float", wantWidth: widthFloat},
		{name: "a float of twenty-five bits of precision", typeName: "float", modifiers: []int{maxFloatPrecision + 1},
			wantEngineName: "double", wantWidth: widthDouble},
		{name: "a float of fifty-three bits of precision", typeName: "FLOAT", modifiers: []int{53},
			wantEngineName: "double", wantWidth: widthDouble},
		{name: "an unsigned float of thirty bits of precision", typeName: "float unsigned", modifiers: []int{30},
			wantEngineName: "double", wantWidth: widthDouble},
		{name: "a float with a scale stays a float", typeName: "float", modifiers: []int{30, 4},
			wantEngineName: "float", wantWidth: widthFloat},
		{name: "FLOAT4 is a float", typeName: "float4", wantEngineName: "float", wantWidth: widthFloat},
		{name: "an unsigned FLOAT4 is a float", typeName: "float4 unsigned", wantEngineName: "float",
			wantWidth: widthFloat},
		{name: "FLOAT4 of twenty-five bits of precision", typeName: "FLOAT4", modifiers: []int{maxFloatPrecision + 1},
			wantEngineName: "double", wantWidth: widthDouble},
		{name: "FLOAT8 is a double", typeName: "float8", wantEngineName: "double", wantWidth: widthDouble},
		{name: "FLOAT8 with a precision and a scale is a double", typeName: "float8", modifiers: []int{10, 2},
			wantEngineName: "double", wantWidth: widthDouble},
		{name: "another type given a precision", typeName: "decimal", modifiers: []int{30}, wantEngineName: "decimal"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sqlType := newTypeResolver(nil, nil).normalise(testCase.typeName, testCase.modifiers...)

			assert.Equal(t, testCase.wantEngineName, sqlType.EngineName)
			assert.Equal(t, testCase.wantWidth, sqlType.Width)
		})
	}
}
