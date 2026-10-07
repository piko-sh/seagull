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

package multirow_test

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/emit/internal/multirow"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func batchQuery(t *testing.T, info *dialect.Facts, typeText string) *gogen.Query {
	t.Helper()
	analysed := &sqlmodel.AnalysedQuery{
		Name:    "InsertBirds",
		Command: sqlmodel.QueryCommandBatch,
		Parameters: []sqlmodel.QueryParameter{
			{Number: 1, Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			{Number: 2, Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
		},
		Template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
			{Kind: sqlmodel.SegmentText, Text: "INSERT INTO birds (id, name) VALUES "},
			{Kind: sqlmodel.SegmentValuesRow, Children: []sqlmodel.SQLSegment{
				{Kind: sqlmodel.SegmentText, Text: "("},
				{Kind: sqlmodel.SegmentParameter, Parameter: 0, TypeText: typeText},
				{Kind: sqlmodel.SegmentText, Text: ", lower("},
				{Kind: sqlmodel.SegmentParameter, Parameter: 1, TypeText: typeText},
				{Kind: sqlmodel.SegmentText, Text: "))"},
			}},
			{Kind: sqlmodel.SegmentText, Text: " ON CONFLICT DO NOTHING"},
		}},
	}
	renderer := &emitkit.Renderer{Query: analysed, Form: info.Placeholders.Execution, Placeholders: info.Placeholders, Quote: info.IdentifierQuote}
	rendered, err := emitkit.Render(analysed.Template, renderer)
	require.NoError(t, err)
	return &gogen.Query{
		Analysed: analysed,
		Naming: &emit.QueryNaming{
			Method: "InsertBirds", SQLConstant: "insertBirdsSQL", Params: "InsertBirdsParams", ParamsArgument: "params",
			ParameterFields: []string{"ID", "Name"},
		},
		Imports:  goast.NewImportTracker(),
		Renderer: renderer,
		Rendered: rendered,
	}
}

func printed(t *testing.T, declaration ast.Decl) string {
	t.Helper()
	var buffer bytes.Buffer
	require.NoError(t, printer.Fprint(&buffer, token.NewFileSet(), declaration))
	return buffer.String()
}

func TestMethodWritesEachRowOfAChunkAndBindsItsArguments(t *testing.T) {
	t.Parallel()

	postgresInfo, clickHouseInfo := dialect.Wrap(postgres.New()).Info(), dialect.Wrap(clickhouse.New()).Info()
	positional := func(access ast.Expr, _ *sqlmodel.QueryParameter, name ast.Expr) ast.Expr {
		require.Nil(t, name)
		return access
	}
	named := func(access ast.Expr, _ *sqlmodel.QueryParameter, name ast.Expr) ast.Expr {
		return goastutil.CallExpr(goastutil.CachedIdent("named"), name, access)
	}
	testCases := []struct {
		info       *dialect.Facts
		bind       multirow.Binder
		connection gogen.Connection
		name       string
		typeText   string
		wants      []string
	}{
		{
			name: "a numbered form, an Exec returning a result", info: &postgresInfo, bind: positional,
			connection: gogen.Connection{Writer: "writer", Exec: "ExecContext", ExecResult: &sqlmodel.GoType{Name: "Result"}},
			wants: []string{
				"for offset := 0; offset < len(params); offset += 5 {",
				`values.Write(strconv.AppendInt(digits[:0], int64(i*2+1), 10))`,
				"args = append(args, item.ID, item.Name)",
				"if _, err := queries.writer.ExecContext(ctx, values.String(), args...)",
			},
		},
		{
			name: "a named form, an Exec returning only the error", info: &clickHouseInfo, bind: named, typeText: "UInt64",
			connection: gogen.Connection{Writer: "db", Exec: "Exec"},
			wants: []string{
				"row := strconv.Itoa(i)",
				`values.WriteString("({p_id_")`,
				`args = append(args, named("p_id_"+row, item.ID), named("p_name_"+row, item.Name))`,
				"if err := queries.db.Exec(ctx, values.String(), args...)",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec := &multirow.Spec{Bind: testCase.bind, Vocabulary: gogen.Vocabulary{}, Connection: testCase.connection, Limit: 10}
			method, err := multirow.Method(batchQuery(t, testCase.info, testCase.typeText), spec)
			require.NoError(t, err)
			source := printed(t, method)
			for _, want := range testCase.wants {
				require.Contains(t, source, want)
			}
		})
	}
}

func TestMethodRefusesARowWiderThanAStatement(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(postgres.New()).Info()
	spec := &multirow.Spec{Bind: func(access ast.Expr, _ *sqlmodel.QueryParameter, _ ast.Expr) ast.Expr { return access },
		Vocabulary: gogen.Vocabulary{}, Connection: gogen.Connection{Writer: "writer", Exec: "ExecContext"}, Limit: 1}

	method, err := multirow.Method(batchQuery(t, &info, ""), spec)

	require.Nil(t, method, "no method is written for a row no statement could hold")
	require.ErrorContains(t, err, "a batch row binds more arguments than one statement binds: query InsertBirds binds 2 arguments in each row, more than the 1 one statement binds")
}

func TestMethodRefusesAQueryWithoutAValuesRow(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(postgres.New()).Info()
	query := batchQuery(t, &info, "")
	query.Analysed.Template.Segments = query.Analysed.Template.Segments[:1]
	spec := &multirow.Spec{Bind: func(access ast.Expr, _ *sqlmodel.QueryParameter, _ ast.Expr) ast.Expr { return access },
		Vocabulary: gogen.Vocabulary{}, Connection: gogen.Connection{Writer: "writer", Exec: "ExecContext"}, Limit: 10}

	_, err := multirow.Method(query, spec)

	require.ErrorContains(t, err, "needs an INSERT with exactly one VALUES row")
}

func TestMethodLabelsAFailedStatementWithTheRowItStartsAtWhenTheStyleAsks(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(postgres.New()).Info()
	testCases := []struct {
		name  string
		want  string
		wraps bool
	}{
		{name: "labelled errors", wraps: true,
			want: `return sglWrapError("test: InsertBirds: the statement starting at row "+strconv.Itoa(offset), err)`},
		{name: "errors as they are", wraps: false, want: "\t\treturn err\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := &emit.Request{Style: emit.Style{HelperPrefix: "sgl", ErrorPrefix: "test", WrapErrors: testCase.wraps}}
			spec := &multirow.Spec{Bind: func(access ast.Expr, _ *sqlmodel.QueryParameter, _ ast.Expr) ast.Expr { return access },
				Vocabulary: gogen.NewVocabulary(request), Connection: gogen.Connection{Writer: "writer", Exec: "ExecContext"}, Limit: 10}

			method, err := multirow.Method(batchQuery(t, &info, ""), spec)

			require.NoError(t, err)
			require.Contains(t, printed(t, method), testCase.want)
		})
	}
}
