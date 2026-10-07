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

package gogen

import (
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestEnumStringsConvertOnlyTheShapesTheDriverNeeds(t *testing.T) {
	t.Parallel()

	vocabulary := testVocabulary()
	slices := EnumStrings{Scan: EnumSlice, Bind: EnumSlice}
	values := EnumStrings{Scan: EnumValue, Bind: EnumValue}
	value := sqlmodel.QueryParameter{Name: "mood", Number: 1, SQLType: enumType, Kind: sqlmodel.ParameterDirectiveParam}
	slice := value
	slice.IsSlice = true
	limit := sqlmodel.QueryParameter{Name: "limit", Number: 2, SQLType: enumType, Context: sqlmodel.ParameterContextLimit}
	testCases := []struct {
		name      string
		enums     EnumStrings
		want      string
		element   string
		parameter sqlmodel.QueryParameter
	}{
		{name: "a slice bound by a driver binding slices as strings", enums: slices, parameter: slice, want: "sglConvertStrings[string](x)", element: "x"},
		{name: "a value bound by a driver binding slices as strings", enums: slices, parameter: value, want: "x", element: "x"},
		{name: "a slice bound element by element by a driver binding values as strings", enums: values, parameter: slice, want: "x", element: "string(x)"},
		{name: "a value bound by a driver binding values as strings", enums: values, parameter: value, want: "string(x)", element: "string(x)"},
		{name: "a pagination bound", enums: values, parameter: limit, want: "x", element: "x"},
		{name: "a driver converting nothing", enums: EnumStrings{}, parameter: slice, want: "x", element: "x"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			access := goastutil.CachedIdent("x")
			require.Equal(t, testCase.want, renderExpression(t, testCase.enums.Bound(access, &sqlmodel.AnalysedQuery{}, &testCase.parameter, namedEnums(), vocabulary)))
			require.Equal(t, testCase.element, renderExpression(t, testCase.enums.BoundElement(access, &testCase.parameter, namedEnums(), vocabulary)))
		})
	}
}

func TestEnumStringsReadAnOptionalSliceOrMapThroughItsPointerBeforeConverting(t *testing.T) {
	t.Parallel()

	everything := EnumStrings{Scan: 0, Bind: EnumValue | EnumSlice | EnumMap}
	dynamic := &sqlmodel.AnalysedQuery{Name: "Filter", IsDynamic: true}
	runtime := &sqlmodel.AnalysedQuery{Name: "Search", IsDynamic: true, DynamicRuntime: true}
	optional := func(sqlType sqlmodel.SQLType) sqlmodel.QueryParameter {
		return sqlmodel.QueryParameter{Name: "wanted", Number: 1, SQLType: sqlType, Nullable: true, IsOptional: true, Kind: sqlmodel.ParameterDirectiveParam}
	}
	required := sqlmodel.QueryParameter{Name: "wanted", Number: 1, SQLType: enumArray(false), Kind: sqlmodel.ParameterDirectiveParam}
	testCases := []struct {
		name      string
		query     *sqlmodel.AnalysedQuery
		want      string
		parameter sqlmodel.QueryParameter
	}{
		{name: "an optional enum array of a dynamic query", query: dynamic, parameter: optional(enumArray(false)), want: "sglConvertStrings[string](sglDereference(x))"},
		{name: "an optional array of nullable enums", query: dynamic, parameter: optional(enumArray(true)), want: "sglConvertStringPointers[string](sglDereference(x))"},
		{name: "an optional map of enum values", query: dynamic, parameter: optional(mapOf(textType, enumType)), want: "sglConvertStringMap[string](sglDereference(x))"},
		{name: "an optional enum value, a pointer already", query: dynamic, parameter: optional(enumType), want: "(*string)(x)"},
		{name: "an optional enum array of a runtime builder, held as it is", query: runtime, parameter: optional(enumArray(false)), want: "sglConvertStrings[string](x)"},
		{name: "a required enum array of a dynamic query", query: dynamic, parameter: required, want: "sglConvertStrings[string](x)"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			bound := everything.Bound(goastutil.CachedIdent("x"), testCase.query, &testCase.parameter, namedEnums(), testVocabulary())

			require.Equal(t, testCase.want, renderExpression(t, bound))
		})
	}
}

func TestEnumStringsScanOnlyTheShapesTheDriverNeeds(t *testing.T) {
	t.Parallel()

	nullable := sqlmodel.OutputColumn{Name: "maybe", SQLType: enumType, Nullable: true}
	array := sqlmodel.OutputColumn{Name: "moods", SQLType: enumArray(false)}
	overridden := sqlmodel.OutputColumn{Name: "mood", SQLType: enumType, GoTypeOverride: &sqlmodel.GoType{Composite: nil, Package: "", Name: "Feeling"}}

	require.Equal(t, EnumHolding{Name: "Mood", Shape: EnumValue, Pointer: true}, EnumStrings{Scan: EnumValue, Bind: 0}.Scanned(&nullable, namedEnums()))
	require.Equal(t, EnumHolding{}, EnumStrings{Scan: EnumValue, Bind: 0}.Scanned(&array, namedEnums()))
	require.Equal(t, EnumHolding{Name: "Mood", Shape: EnumSlice, Pointer: false}, EnumStrings{Scan: EnumSlice, Bind: 0}.Scanned(&array, namedEnums()))
	require.Equal(t, EnumHolding{}, EnumStrings{Scan: EnumSlice, Bind: 0}.Scanned(&nullable, namedEnums()))
	require.Equal(t, EnumHolding{}, EnumStrings{Scan: EnumValue | EnumSlice, Bind: 0}.Scanned(&overridden, namedEnums()))
}

func TestEnumStringsLeaveANestedShapeAsItIs(t *testing.T) {
	t.Parallel()

	everything := EnumStrings{Scan: EnumValue | EnumSlice | EnumMap | EnumNested | EnumNestedInMap, Bind: EnumValue | EnumSlice | EnumMap | EnumNested | EnumNestedInMap}
	grid := sqlmodel.OutputColumn{Name: "grid", SQLType: arrayOf(enumArray(false))}
	keyed := sqlmodel.QueryParameter{Name: "keyed", Number: 1, SQLType: mapOf(enumType, textType), Kind: sqlmodel.ParameterDirectiveParam}

	require.Equal(t, EnumHolding{}, everything.Scanned(&grid, namedEnums()))
	require.Equal(t, "x", renderExpression(t, everything.Bound(goastutil.CachedIdent("x"), &sqlmodel.AnalysedQuery{}, &keyed, namedEnums(), testVocabulary())))
}

func TestEnumStringsProblemsRefuseTheShapesTheGeneratedCodeCannotConvert(t *testing.T) {
	t.Parallel()

	query := &sqlmodel.AnalysedQuery{
		Name: "GetItems",
		OutputColumns: []sqlmodel.OutputColumn{
			{Name: "moods", SQLType: enumArray(true)},
			{Name: "grid", SQLType: arrayOf(enumArray(false))},
			{Name: "keyed", SQLType: mapOf(enumType, textType)},
			{Name: "labelled", SQLType: mapOf(textType, enumType)},
		},
		Parameters: []sqlmodel.QueryParameter{
			{Name: "grid", Number: 1, SQLType: arrayOf(enumArray(false)), Kind: sqlmodel.ParameterDirectiveParam},
			{Name: "keyed", Number: 2, SQLType: mapOf(enumType, textType), Kind: sqlmodel.ParameterDirectiveParam},
			{Name: "limit", Number: 3, SQLType: arrayOf(enumArray(false)), Context: sqlmodel.ParameterContextLimit},
		},
	}
	columnProblem := func(subject, where string) emit.Problem {
		return emit.Problem{
			Query: query,
			Code:  diag.CodeEmitterUnsupported,
			Message: subject + " holds the generated enum type Mood " + where + ", which the driver cannot read, and the generated " +
				"code converts an enum only as a value, a pointer, a slice or a map's values; cast the column to a type that " +
				"holds no enum in the query, or keep enums as Go strings (types.enum: string)",
			Severity: diag.SeverityError,
		}
	}
	parameterProblem := func(subject, where string) emit.Problem {
		return emit.Problem{
			Query: query,
			Code:  diag.CodeEmitterUnsupported,
			Message: subject + " holds the generated enum type Mood " + where + ", which the driver cannot bind, and the generated " +
				"code converts an enum only as a value, a pointer, a slice or a map's values; give the parameter a type that " +
				"holds no enum (sgl.param type:), or keep enums as Go strings (types.enum: string)",
			Severity: diag.SeverityError,
		}
	}
	nested, inMap := "nested in a slice or a struct", "inside a map, as a key or within a value"
	testCases := []struct {
		name  string
		enums EnumStrings
		want  []emit.Problem
	}{
		{
			name:  "a driver reading and binding every shape only as strings",
			enums: EnumStrings{Scan: EnumSlice | EnumMap | EnumNested | EnumNestedInMap, Bind: EnumNested | EnumNestedInMap},
			want: []emit.Problem{
				columnProblem("column grid", nested), columnProblem("column keyed", inMap),
				parameterProblem("parameter grid", nested), parameterProblem("parameter keyed", inMap),
			},
		},
		{
			name:  "a driver reading only maps as strings",
			enums: EnumStrings{Scan: EnumMap | EnumNestedInMap, Bind: 0},
			want:  []emit.Problem{columnProblem("column keyed", inMap)},
		},
		{
			name:  "a driver converting only the shapes the generated code converts",
			enums: EnumStrings{Scan: EnumValue | EnumSlice | EnumMap, Bind: EnumValue | EnumSlice | EnumMap},
			want:  nil,
		},
		{name: "a driver converting nothing", enums: EnumStrings{}, want: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := &emit.Request{Dialect: renderingInfo(dollarForm, dollarForm), Types: namedEnums()}

			require.Equal(t, testCase.want, testCase.enums.Problems(request, query, emitkit.ListArray, "the driver"))
		})
	}
}

func TestEnumStringsProblemsJudgeASliceByWhatTheDriverBinds(t *testing.T) {
	t.Parallel()

	query := &sqlmodel.AnalysedQuery{
		Name: "ItemsWithMoodSets",
		Parameters: []sqlmodel.QueryParameter{
			{Name: "sets", Number: 1, SQLType: enumArray(false), IsSlice: true, Kind: sqlmodel.ParameterDirectiveParam},
		},
	}
	arrays := renderingInfo(dollarForm, dollarForm)
	arrays.Lists.ArrayMembership = dialect.Affix{Before: "= ANY(", After: ")"}
	enums := EnumStrings{Scan: 0, Bind: EnumSlice | EnumNested}
	testCases := []struct {
		name    string
		info    dialect.Facts
		lists   emitkit.ListMode
		refused bool
	}{
		{name: "a list expanded element by element binds each enum array, which the driver converts", info: renderingInfo(dollarForm, dollarForm), lists: emitkit.ListExpand, refused: false},
		{name: "a list a driver binding arrays expands for a dialect without an array form", info: renderingInfo(dollarForm, dollarForm), lists: emitkit.ListArray, refused: false},
		{name: "a list bound as one array binds a slice of enum arrays", info: arrays, lists: emitkit.ListArray, refused: true},
		{name: "a list in a form binding by name is bound whole", info: renderingInfo(namedForm, dollarForm), lists: emitkit.ListExpand, refused: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := &emit.Request{Dialect: testCase.info, Types: namedEnums()}

			problems := enums.Problems(request, query, testCase.lists, "the driver")

			require.Equal(t, testCase.refused, len(problems) == 1, "problems: %v", problems)
		})
	}
}
