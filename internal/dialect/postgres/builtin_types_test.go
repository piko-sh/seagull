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

package postgres

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestTextTypesSentInAnotherBinaryFormSaySo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		typeName       string
		wantBinaryForm bool
	}{
		{name: "a bit string", typeName: "bit", wantBinaryForm: true},
		{name: "a varying bit string", typeName: "varbit", wantBinaryForm: true},
		{name: "a text search document", typeName: "tsvector", wantBinaryForm: true},
		{name: "a text search query", typeName: "tsquery", wantBinaryForm: false},
		{name: "text", typeName: "text", wantBinaryForm: false},
		{name: "an XML document", typeName: "xml", wantBinaryForm: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.wantBinaryForm, New().NormaliseTypeName(testCase.typeName).BinaryForm)
		})
	}
}

func TestNormaliseTypeNameKeepsAQuotedWordAsWrittenAndFoldsAnUnquotedOne(t *testing.T) {
	t.Parallel()

	tooLong := `"` + strings.Repeat("Long", maxWrittenTypeRunes) + `"`
	tooManyTokens := "numeric(" + strings.Repeat("1,", maxWrittenTypeTokens) + "1)"
	testCases := []struct {
		name            string
		typeName        string
		wantCategory    sqlmodel.SQLTypeCategory
		wantSchema      string
		wantEngineName  string
		wantElementName string
	}{
		{name: "a quoted mixed-case name keeps its letters", typeName: `"Pair"`,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: "Pair"},
		{name: "an unquoted name of the same letters folds", typeName: "Pair",
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: "pair"},
		{name: "a quoted schema and name keep their letters", typeName: `"Shapes"."Pair"`,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantSchema: "Shapes", wantEngineName: "Pair"},
		{name: "an unquoted name under a quoted schema folds", typeName: `"Shapes".Pair`,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantSchema: "Shapes", wantEngineName: "pair"},
		{name: "an array of a quoted name keeps its letters", typeName: `"Mood"[]`,
			wantCategory: sqlmodel.TypeCategoryArray, wantEngineName: "Mood[]", wantElementName: "Mood"},
		{name: "a doubled quote stands for one quote", typeName: `"Say ""Hi"""`,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: `Say "Hi"`},
		{name: "a quoted built-in name in lower case keeps its canonical spelling", typeName: `"integer"`,
			wantCategory: sqlmodel.TypeCategoryInteger, wantEngineName: "int4"},
		{name: "a quoted built-in name in upper case is not the built-in", typeName: `"INTEGER"`,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: "INTEGER"},
		{name: "a quoted mixed-case built-in name is not the built-in", typeName: `"Point"`,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: "Point"},
		{name: "an unquoted mixed-case built-in name is the built-in", typeName: "Point",
			wantCategory: sqlmodel.TypeCategoryGeometric, wantEngineName: "point"},
		{name: "a quoted name past the length bound is read as a bare name", typeName: tooLong,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: strings.ToLower(tooLong)},
		{name: "a name of more tokens than the bound is read as a bare name", typeName: tooManyTokens,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: tooManyTokens},
		{name: "an unterminated quote is read as a bare name", typeName: `"Mood`,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: `"mood`},
		{name: "words after the type are read as a bare name", typeName: `"Mood" NOT NULL`,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: `"mood" not null`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolved := New().NormaliseTypeName(testCase.typeName)
			assert.Equal(t, testCase.wantCategory, resolved.Category)
			assert.Equal(t, testCase.wantSchema, resolved.Schema)
			assert.Equal(t, testCase.wantEngineName, resolved.EngineName)
			if testCase.wantElementName == "" {
				assert.Nil(t, resolved.ElementType)
				return
			}
			require.NotNil(t, resolved.ElementType)
			assert.Equal(t, testCase.wantElementName, resolved.ElementType.EngineName)
		})
	}
}

func TestNormaliseTypeNameReadsAModifierListWrittenInTheName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		typeName       string
		wantEngineName string
		wantPrecision  *int
		wantScale      *int
		wantLength     *int
		wantArray      bool
	}{
		{name: "a numeric precision and scale", typeName: "numeric(10,2)", wantEngineName: "numeric", wantPrecision: new(10), wantScale: new(2)},
		{name: "spaces in the list", typeName: " NUMERIC( 10 , 2 ) ", wantEngineName: "numeric", wantPrecision: new(10), wantScale: new(2)},
		{name: "a varchar length", typeName: "varchar(255)", wantEngineName: "varchar", wantLength: new(255)},
		{name: "a multi-word name's length", typeName: "character varying(20)", wantEngineName: "varchar", wantLength: new(20)},
		{name: "a precision before a time zone", typeName: "timestamp(3) with time zone", wantEngineName: "timestamptz", wantPrecision: new(3)},
		{name: "a fraction is truncated", typeName: "numeric(10.5, 2)", wantEngineName: "numeric", wantPrecision: new(10), wantScale: new(2)},
		{name: "an array keeps its element's modifiers", typeName: "numeric(10, 2)[]", wantEngineName: "numeric", wantPrecision: new(10),
			wantScale: new(2), wantArray: true},
		{name: "a name without a list has no modifiers", typeName: "numeric", wantEngineName: "numeric"},
		{name: "a negative scale keeps its sign", typeName: "numeric(5,-2)", wantEngineName: "numeric", wantPrecision: new(5), wantScale: new(-2)},
		{name: "a precision before no time zone", typeName: "time(0) without time zone", wantEngineName: "time", wantPrecision: new(0)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolved := New().NormaliseTypeName(testCase.typeName)
			if testCase.wantArray {
				require.Equal(t, sqlmodel.TypeCategoryArray, resolved.Category)
				require.NotNil(t, resolved.ElementType)
				resolved = *resolved.ElementType
			}
			assert.Equal(t, testCase.wantEngineName, resolved.EngineName)
			assert.Equal(t, testCase.wantPrecision, resolved.Precision)
			assert.Equal(t, testCase.wantScale, resolved.Scale)
			assert.Equal(t, testCase.wantLength, resolved.Length)
		})
	}
}

func TestNormaliseTypeNameAsksTheHooksForTheNameAndModifiersAWrittenTypeHolds(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		typeName       string
		wantModifiers  []int
		wantEngineName string
		wantAsked      bool
	}{
		{name: "a modifier list", typeName: "widget(7)", wantAsked: true, wantModifiers: []int{7}, wantEngineName: "answered"},
		{name: "a quoted name in lower case", typeName: `"widget"`, wantAsked: true, wantModifiers: nil, wantEngineName: "answered"},
		{name: "a quoted qualified name with a modifier list", typeName: `"Parts"."widget"(3, 1)`, wantAsked: true,
			wantModifiers: []int{3, 1}, wantEngineName: "answered"},
		{name: "a quoted name in another case is not asked", typeName: `"Widget"`, wantAsked: false, wantEngineName: "Widget"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			answer := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "answered")
			asked := false
			var askedModifiers []int
			engine := New(WithTypeNormaliserHook(func(name string, modifiers []int) *sqlmodel.SQLType {
				if name != "widget" {
					return nil
				}
				asked, askedModifiers = true, modifiers
				return &answer
			}))

			resolved := engine.NormaliseTypeName(testCase.typeName)
			assert.Equal(t, testCase.wantAsked, asked)
			assert.Equal(t, testCase.wantModifiers, askedModifiers)
			assert.Equal(t, testCase.wantEngineName, resolved.EngineName)
		})
	}
}
