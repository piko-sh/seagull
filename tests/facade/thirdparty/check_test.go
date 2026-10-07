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

package thirdparty_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/tests/facade/thirdparty"
)

type nameResolver map[string]seagull.GoType

func (resolver nameResolver) Resolve(sqlType dialect.SQLType, _ bool) seagull.GoType {
	if goType, found := resolver[sqlType.EngineName]; found {
		return goType
	}
	return seagull.GoType{Name: "string"}
}

func numberedDialect() dialect.Facts {
	form := dialect.PlaceholderSigil{Prefix: "$", Kind: dialect.PlaceholderNumbered}
	return dialect.Facts{Placeholders: dialect.Placeholders{Execution: form, Sigils: []dialect.PlaceholderSigil{form}}}
}

func anonymousDialect() dialect.Facts {
	form := dialect.PlaceholderSigil{Prefix: "?", Kind: dialect.PlaceholderAnonymous}
	return dialect.Facts{Placeholders: dialect.Placeholders{Execution: form, Sigils: []dialect.PlaceholderSigil{form}}}
}

func namedDialect() dialect.Facts {
	form := dialect.PlaceholderSigil{Prefix: ":", Kind: dialect.PlaceholderNamed}
	return dialect.Facts{Placeholders: dialect.Placeholders{Execution: form, Sigils: []dialect.PlaceholderSigil{form}}}
}

func textOf(engineName string) dialect.SQLType {
	return dialect.NewSQLType(dialect.TypeCategoryText, engineName)
}

func parameterOf(name, engineName string) emit.QueryParameter {
	return emit.QueryParameter{Name: name, SQLType: textOf(engineName)}
}

func columnOf(name, engineName string) emit.OutputColumn {
	return emit.OutputColumn{Name: name, SQLType: textOf(engineName)}
}

func queryOf(command emit.QueryCommand, parameters []emit.QueryParameter, columns []emit.OutputColumn) *emit.AnalysedQuery {
	return &emit.AnalysedQuery{Name: "Probe", Filename: "probe.sql", Command: command, Parameters: parameters, OutputColumns: columns}
}

func batchOutsideValues() *emit.AnalysedQuery {
	query := queryOf(emit.QueryCommandBatch, []emit.QueryParameter{parameterOf("id", "text"), parameterOf("replacement", "text")}, nil)
	query.Template = emit.SQLTemplate{Segments: []emit.SQLSegment{
		{Kind: emit.SegmentText, Text: "INSERT INTO t (id) VALUES "},
		{Kind: emit.SegmentValuesRow, Children: []emit.SQLSegment{
			{Kind: emit.SegmentText, Text: "("},
			{Kind: emit.SegmentParameter, Parameter: 0},
			{Kind: emit.SegmentText, Text: ")"},
		}},
		{Kind: emit.SegmentText, Text: " ON CONFLICT (id) DO UPDATE SET id = "},
		{Kind: emit.SegmentParameter, Parameter: 1},
	}}
	return query
}

func TestCheckRefusesWhatTheEmitterDoesNotGenerate(t *testing.T) {
	t.Parallel()

	limited := parameterOf("page", "text")
	limited.DefaultLimit = new(10)
	outer := columnOf("id", "text")
	outer.IsEmbedded, outer.EmbedTable, outer.EmbedIsOuter = true, "items", true
	grouped := queryOf(emit.QueryCommandStream, nil, []emit.OutputColumn{columnOf("id", "text"), outer})
	grouped.GroupByKey = []string{"orders.id"}
	inner := columnOf("id", "text")
	inner.IsEmbedded, inner.EmbedTable = true, "items"
	groupedStream := queryOf(emit.QueryCommandStream, nil, []emit.OutputColumn{columnOf("id", "text"), inner})
	groupedStream.GroupByKey = []string{"orders.id"}
	builder := queryOf(emit.QueryCommandMany, []emit.QueryParameter{parameterOf("id", "text")}, []emit.OutputColumn{columnOf("id", "text")})
	builder.DynamicRuntime = true

	tests := []struct {
		name     string
		info     dialect.Facts
		query    *emit.AnalysedQuery
		code     string
		contains string
	}{
		{name: "a dialect binding by name", info: namedDialect(), query: queryOf(emit.QueryCommandExec, nil, nil), code: seagull.CodeEmitterUnsupported, contains: "binds parameters by name"},
		{name: "copyfrom", info: numberedDialect(), query: queryOf(emit.QueryCommandCopyFrom, nil, nil), code: seagull.CodeEmitterUnsupported, contains: "command: copyfrom"},
		{name: "asyncexec", info: numberedDialect(), query: queryOf(emit.QueryCommandAsyncExec, nil, nil), code: seagull.CodeEmitterUnsupported, contains: "command: asyncexec"},
		{name: "a batch returning rows", info: numberedDialect(), query: queryOf(emit.QueryCommandBatch, nil, []emit.OutputColumn{columnOf("id", "text")}), code: seagull.CodeEmitterUnsupported, contains: "RETURNING"},
		{name: "a batch binding a parameter outside its row", info: numberedDialect(), query: batchOutsideValues(), code: seagull.CodeBatchShape, contains: "outside its VALUES row"},
		{name: "a LIMIT default", info: numberedDialect(), query: queryOf(emit.QueryCommandExec, []emit.QueryParameter{limited}, nil), code: seagull.CodeEmitterUnsupported, contains: "default: or max:"},
		{name: "a column of a slice type", info: numberedDialect(), query: queryOf(emit.QueryCommandMany, nil, []emit.OutputColumn{columnOf("tags", "text[]")}), code: seagull.CodeEmitterUnsupported, contains: "cannot scan"},
		{name: "a column of the inline JSON type", info: numberedDialect(), query: queryOf(emit.QueryCommandMany, nil, []emit.OutputColumn{columnOf("body", "json")}), code: seagull.CodeEmitterUnsupported, contains: "cannot scan"},
		{name: "an outer embed in a grouped stream", info: numberedDialect(), query: grouped, code: seagull.CodeEmitterUnsupported, contains: "outer embed"},
		{name: "a grouped stream", info: numberedDialect(), query: groupedStream, code: seagull.CodeEmitterUnsupported, contains: "group_by needs command: many"},
		{name: "a runtime builder on an anonymous form", info: anonymousDialect(), query: builder, code: seagull.CodeEmitterUnsupported, contains: "dynamic: runtime"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := &emit.Request{
				Dialect: test.info,
				Types:   nameResolver{"text[]": {Name: "[]string"}, "json": {Name: emit.InlineJSONTypeName}},
				Queries: []*emit.AnalysedQuery{test.query},
			}

			problems := thirdparty.New().Check(request)

			require.NotEmpty(t, problems)
			require.Equal(t, test.code, problems[0].Code)
			require.Contains(t, problems[0].Message, test.contains)
			require.Equal(t, seagull.SeverityError, problems[0].Severity)
		})
	}
}

func TestCheckAcceptsWhatTheEmitterGenerates(t *testing.T) {
	t.Parallel()

	request := &emit.Request{
		Dialect: numberedDialect(),
		Types:   nameResolver{"bytea": {Name: "[]byte"}},
		Queries: []*emit.AnalysedQuery{
			queryOf(emit.QueryCommandOne, []emit.QueryParameter{parameterOf("id", "text")}, []emit.OutputColumn{columnOf("body", "bytea")}),
			queryOf(emit.QueryCommandExecRows, nil, nil),
		},
	}

	require.Empty(t, thirdparty.New().Check(request))
}
