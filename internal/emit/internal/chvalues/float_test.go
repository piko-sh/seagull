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

package chvalues

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

var testTransport = FloatTransport{Syntax: dialect.FloatBitsSyntax{Single: "UInt32", Double: "UInt64", Arrow: "->"}, Local: "sgl_float"}

func TestFloatTransportPreservesNamesAndQuotedTypeDetails(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ original, expected string }{
		{original: "Float64", expected: "UInt64"},
		{original: "Nullable(Float32)", expected: "Nullable(UInt32)"},
		{original: "Array(Array(Nullable(Float64)))", expected: "Array(Array(Nullable(UInt64)))"},
		{original: "Map(String, Float64)", expected: "Map(String, UInt64)"},
		{original: "Tuple(Float64 Float64, Float32 Float32)", expected: "Tuple(Float64 UInt64, Float32 UInt32)"},
		{original: "Tuple(`Float64` Float64, label Enum8('Float32'=1, 'f''Float64'=2))", expected: "Tuple(`Float64` UInt64, label Enum8('Float32'=1, 'f''Float64'=2))"},
		{original: "Tuple(a REAL, b single, c DOUBLE PRECISION, d FLOAT)", expected: "Tuple(a UInt32, b UInt32, c UInt64, d UInt32)"},
	} {
		require.Equal(t, test.expected, floatTransportType(test.original, testTransport))
	}
}

func TestFloatTemplatesCopyQueriesAndKeepNativeColumnarBatches(t *testing.T) {
	t.Parallel()
	scalar := sqlmodel.NewFloatType("Float64", 64)
	query := &sqlmodel.AnalysedQuery{
		Parameters: []sqlmodel.QueryParameter{{Name: "value", SQLType: scalar}},
		Template:   sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{{Kind: sqlmodel.SegmentParameter, Parameter: 0, TypeText: "Float64"}}},
	}
	count := query.Template
	query.CountTemplate = &count
	result := FloatParameters([]*sqlmodel.AnalysedQuery{query}, testTransport)
	require.NotSame(t, query, result[0])
	require.Len(t, query.Template.Segments, 1)
	require.Len(t, query.CountTemplate.Segments, 1)
	require.Len(t, result[0].Template.Segments, 3)
	require.Equal(t, "String", result[0].Template.Segments[1].TypeText)
	require.Equal(t, "Float64", query.Template.Segments[0].TypeText)
	require.NotSame(t, query.CountTemplate, result[0].CountTemplate)
	query.Command = sqlmodel.QueryCommandCopyFrom
	require.Same(t, query, NativeFloatParameters([]*sqlmodel.AnalysedQuery{query}, testTransport)[0])
}

func TestFloatDetectionBoundsRecursiveTypes(t *testing.T) {
	t.Parallel()
	array := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "Array")
	array.ElementType = &array
	require.False(t, holdsFloatValue(&array))
	require.False(t, holdsFloatValue(nil))
	require.False(t, holdsFloatValue(new(sqlmodel.NewFloatType("BFloat16", 16))))
}

func TestFloatTransportLeavesQueriesAloneWithoutDialectSyntax(t *testing.T) {
	t.Parallel()
	query := &sqlmodel.AnalysedQuery{
		Parameters: []sqlmodel.QueryParameter{{Name: "value", SQLType: sqlmodel.NewFloatType("Float64", 64)}},
		Template:   sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{{Kind: sqlmodel.SegmentParameter, Parameter: 0, TypeText: "Float64"}}},
	}
	require.Same(t, query, FloatParameters([]*sqlmodel.AnalysedQuery{query}, FloatTransport{})[0])
}

func TestFloatTransportNamesLocalsWithTheHelperPrefix(t *testing.T) {
	t.Parallel()
	facts := dialect.Facts{FloatBits: testTransport.Syntax}
	parameter := &sqlmodel.QueryParameter{Name: "value", SQLType: sqlmodel.NewFloatType("Float64", 64)}
	_, wrap := floatCast(parameter, "Float64", NewFloatTransport(&facts, "acme"))
	require.Contains(t, wrap.Before, "acme_float0 -> ")
	require.NotContains(t, wrap.Before+wrap.After, "sgl", "the default vocabulary leaked into a preset")
}
