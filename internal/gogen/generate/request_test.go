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

package generate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

func usersCatalogue() *sqlmodel.Catalogue {
	return &sqlmodel.Catalogue{
		DefaultSchema: "main",
		Schemas: map[string]*sqlmodel.Schema{"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"users": {
			Name:   "users",
			Schema: "main",
			Columns: []sqlmodel.Column{
				sqlmodel.NewColumn("id", sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, false),
				sqlmodel.NewColumn("name", sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, false),
			},
		}}}},
	}
}

func plannedRequest(t *testing.T) *emit.Request {
	t.Helper()
	emission := indexedEmission(true)
	request := emission.Request
	request.Catalogue = usersCatalogue()
	request.Queries = []*sqlmodel.AnalysedQuery{
		withTemplate(&sqlmodel.AnalysedQuery{
			Name: "GetUser", Filename: "users.sql", SQL: "SELECT id, name FROM users WHERE id = ?1", Command: sqlmodel.QueryCommandOne, ReadOnly: true,
			Parameters:    []sqlmodel.QueryParameter{textParam("id", 1)},
			OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), textColumn("name", false)},
		}),
		withTemplate(dynamicSortableQuery(sqlmodel.QueryCommandMany)),
	}
	planned, problems := gogen.WithNamingPlan(request)
	require.Empty(t, problems)
	planned.Naming.Types = []emit.NamedType{{
		Name:      "Mood",
		Constants: []string{"MoodCalm", "MoodStormy"},
		SQLType:   sqlmodel.SQLType{Category: sqlmodel.TypeCategoryEnum, EnumValues: []string{"calm", "stormy"}},
	}}
	return planned
}

func TestPackageEmitsARequestWhosePlanFitsIt(t *testing.T) {
	t.Parallel()

	request := plannedRequest(t)

	files, err := Package(t.Context(), request, testDriver())

	require.NoError(t, err)
	require.NotEmpty(t, files)
}

func TestPackageRefusesARequestItCannotEmitAsItIs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		change func(request *emit.Request)
		name   string
		want   string
	}{
		{name: "a nil query", want: "query 1 is nil", change: func(request *emit.Request) { request.Queries[1] = nil }},
		{name: "a package name that is no identifier", want: "package name", change: func(request *emit.Request) { request.Package = "db\nfunc init() {}" }},
		{name: "the blank package name", want: "package name", change: func(request *emit.Request) { request.Package = "_" }},
		{name: "a header of two lines", want: "header", change: func(request *emit.Request) { request.Style.Header = "// Code generated.\nfunc init() {}" }},
		{name: "a header that is no comment", want: "header", change: func(request *emit.Request) { request.Style.Header = "package other" }},
		{name: "a header holding a byte order mark", want: "header", change: func(request *emit.Request) { request.Style.Header = "// a\uFEFFb" }},
		{name: "a helper prefix holding a quote", want: "helper prefix", change: func(request *emit.Request) { request.Style.HelperPrefix = `sgl"` }},
		{name: "an empty error prefix", want: "error prefix", change: func(request *emit.Request) { request.Style.ErrorPrefix = "" }},
		{name: "an error prefix holding a quote", want: "error prefix", change: func(request *emit.Request) { request.Style.ErrorPrefix = `sgl", "x` }},
		{name: "a query the plan names no method for", want: "names no method", change: func(request *emit.Request) { request.Naming.Queries[0].Method = "" }},
		{name: "a method that is no identifier", want: "not a Go identifier", change: func(request *emit.Request) { request.Naming.Queries[0].Method = "Get User" }},
		{name: "a column field that is no identifier", want: "not a Go identifier", change: func(request *emit.Request) { request.Naming.Queries[0].ColumnFields[1] = "Name`" }},
		{name: "too few parameter fields", want: "0 parameter fields for 1 parameters", change: func(request *emit.Request) { request.Naming.Queries[0].ParameterFields = nil }},
		{name: "too few column fields", want: "1 column fields for 2 columns", change: func(request *emit.Request) { request.Naming.Queries[0].ColumnFields = []string{"ID"} }},
		{name: "column locals for no grouping that do not fit", want: "1 column locals for 2 columns", change: func(request *emit.Request) { request.Naming.Queries[0].ColumnLocals = []string{"rowID"} }},
		{name: "too few order constants", want: "1 order constants for 2 sortable columns", change: func(request *emit.Request) {
			request.Naming.Queries[1].OrderByConstants = request.Naming.Queries[1].OrderByConstants[:1]
		}},
		{name: "a model with too few fields", want: "names 1 fields for the 2 columns", change: func(request *emit.Request) { request.Naming.Models[0].Fields = []string{"ID"} }},
		{name: "a model of a table the catalogue does not hold", want: "which the catalogue does not hold", change: func(request *emit.Request) { request.Naming.Models[0].Relation = "posts" }},
		{name: "a model field that is no identifier", want: "not a Go identifier", change: func(request *emit.Request) { request.Naming.Models[0].Fields[0] = "1D" }},
		{name: "an enum with too few constants", want: "names 1 of its 2 values", change: func(request *emit.Request) { request.Naming.Types[0].Constants = []string{"MoodCalm"} }},
		{name: "an interface that is no identifier", want: "interface", change: func(request *emit.Request) { request.Naming.Interface = "Querier {}\nfunc init() {" }},
		{name: "a qualifier that is no identifier", want: "qualifier", change: func(request *emit.Request) { request.Naming.Imports["context"] = "con text" }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := plannedRequest(t)
			testCase.change(request)

			files, err := Package(t.Context(), request, testDriver())

			require.ErrorIs(t, err, ErrInvalidRequest)
			require.ErrorContains(t, err, testCase.want)
			require.Nil(t, files)
		})
	}
}

func TestPackageRefusesANilRequest(t *testing.T) {
	t.Parallel()

	files, err := Package(t.Context(), nil, testDriver())

	require.ErrorIs(t, err, ErrInvalidRequest)
	require.Nil(t, files)
}

func TestPackageRefusesARequestWhoseNamesCannotBePlanned(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		change func(request *emit.Request)
		name   string
		want   string
	}{
		{name: "a query name that cannot be exported", want: `query 1st: Q`, change: func(request *emit.Request) { request.Queries[0].Name = "1st" }},
		{name: "a query-names function the querier declares", want: `the query-names function "New"`, change: func(request *emit.Request) {
			request.Style.QueryNames = emit.QueryNames{Enabled: true, File: "query_names.go", Function: "New"}
		}},
		{name: "a query-names file the emitter writes", want: `the query-names file "querier.go"`, change: func(request *emit.Request) {
			request.Style.QueryNames = emit.QueryNames{Enabled: true, File: "querier.go", Function: "QueryName"}
		}},
		{name: "an interface named like the querier type", want: `the interface name "Queries"`, change: func(request *emit.Request) {
			request.Style.Interface = emit.Interface{Enabled: true, Name: "Queries"}
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := plannedRequest(t)
			request.Naming = emit.Naming{}
			testCase.change(request)

			files, err := Package(t.Context(), request, testDriver())

			require.ErrorIs(t, err, ErrInvalidRequest)
			require.ErrorContains(t, err, testCase.want)
			require.Nil(t, files)
		})
	}
}

func TestRefusePlanNamesTheFirstErrorProblemsAndCountsTheRest(t *testing.T) {
	t.Parallel()

	problems := make([]emit.Problem, 0, maxReportedProblems+5)
	for index := range maxReportedProblems + 4 {
		query := &sqlmodel.AnalysedQuery{Name: fmt.Sprintf("Query%d", index)}
		problems = append(problems, emit.Problem{Query: query, Code: "Q001", Message: "cannot be named", Severity: diag.SeverityError})
	}
	problems = append(problems, emit.Problem{Query: nil, Code: "Q002", Message: "only a warning", Severity: diag.SeverityWarning})

	testCases := []struct {
		name     string
		problems []emit.Problem
		wantNil  bool
		want     []string
		unwanted []string
	}{
		{name: "no problems", problems: nil, wantNil: true},
		{name: "warnings alone", problems: problems[len(problems)-1:], wantNil: true},
		{
			name:     "more errors than the cap",
			problems: problems,
			want:     []string{"query Query0: Q001 cannot be named", fmt.Sprintf("query Query%d: Q001", maxReportedProblems-1), "and 4 more"},
			unwanted: []string{fmt.Sprintf("query Query%d:", maxReportedProblems), "only a warning"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := refusePlan(testCase.problems)

			if testCase.wantNil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrInvalidRequest)
			for _, want := range testCase.want {
				require.ErrorContains(t, err, want)
			}
			for _, unwanted := range testCase.unwanted {
				require.False(t, strings.Contains(err.Error(), unwanted), "unexpected %q in %q", unwanted, err.Error())
			}
		})
	}
}
