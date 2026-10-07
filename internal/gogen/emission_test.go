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
	"go/ast"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

type envelopeBinder struct{}

func (envelopeBinder) Bind(access ast.Expr, _ *sqlmodel.QueryParameter, _ *goast.ImportTracker) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("envelope"), access)
}

type positionalEnvelopeBinder struct {
	envelopeBinder
}

func (positionalEnvelopeBinder) BindPositional(access ast.Expr, _ *sqlmodel.QueryParameter) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("positional"), access)
}

type fixedReader struct {
	reading ColumnReading
}

func (reader fixedReader) ColumnReading(sqlmodel.SQLType) ColumnReading {
	return reader.reading
}

func emissionWith(driver *Driver, queries ...*sqlmodel.AnalysedQuery) *Emission {
	request := testRequest()
	request.Queries = queries
	return NewEmission(request, driver)
}

func TestEmissionBindsAParameterThroughTheDriversEnvelope(t *testing.T) {
	t.Parallel()

	parameter := &sqlmodel.QueryParameter{Name: "id", Number: 1}
	testCases := []struct {
		binder     ParameterBinder
		name       string
		bound      string
		positional string
	}{
		{name: "a driver binding values as they are", binder: nil, bound: "id", positional: "id"},
		{name: "a driver with an envelope", binder: envelopeBinder{}, bound: "envelope(id)", positional: "id"},
		{name: "a driver with a positional envelope", binder: positionalEnvelopeBinder{}, bound: "envelope(id)", positional: "positional(id)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := emissionWith(&Driver{Binder: testCase.binder})

			require.Equal(t, testCase.bound, renderExpression(t, emission.Bind(goastutil.CachedIdent("id"), parameter, goast.NewImportTracker())))
			require.Equal(t, testCase.positional, renderExpression(t, emission.BindPositional(goastutil.CachedIdent("id"), parameter)))
		})
	}
}

func TestEmissionReadsAColumnAsTheDriversReaderSays(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		reader ColumnReader
		name   string
		want   ColumnReading
	}{
		{name: "a driver with no reader", reader: nil, want: ColumnReadAsIs},
		{name: "a driver reading JSON", reader: fixedReader{reading: ColumnReadAsJSON}, want: ColumnReadAsJSON},
		{name: "a driver converting", reader: fixedReader{reading: ColumnReadConverted}, want: ColumnReadConverted},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := emissionWith(&Driver{Reader: testCase.reader})

			require.Equal(t, testCase.want, emission.Reading(sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}))
		})
	}
}

func TestEmissionRunsAReadOnlyQueryOnTheReaderAndAnyOtherOnTheWriter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		want     string
		readOnly bool
	}{
		{name: "a read-only query", readOnly: true, want: "reader"},
		{name: "a query that writes", readOnly: false, want: "writer"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := emissionWith(&Driver{Connection: Connection{Reader: "reader", Writer: "writer"}})

			require.Equal(t, testCase.want, emission.ConnectionField(&sqlmodel.AnalysedQuery{ReadOnly: testCase.readOnly}))
		})
	}
}

func TestEmissionNamingPlansAQueryAddedAfterItStarted(t *testing.T) {
	t.Parallel()

	first := &sqlmodel.AnalysedQuery{Name: "GetBoat", Filename: "boats.sql", Command: sqlmodel.QueryCommandExec}
	added := &sqlmodel.AnalysedQuery{Name: "ListBoats", Filename: "boats.sql", Command: sqlmodel.QueryCommandExec}
	foreign := &sqlmodel.AnalysedQuery{Name: "Elsewhere", Filename: "other.sql", Command: sqlmodel.QueryCommandExec}
	emission := emissionWith(&Driver{}, first)

	testCases := []struct {
		query *sqlmodel.AnalysedQuery
		name  string
		want  string
	}{
		{name: "a query the plan names", query: first, want: "GetBoat"},
		{name: "a query added to the request since", query: added, want: "ListBoats"},
		{name: "a query the request does not hold", query: foreign, want: ""},
	}
	emission.Request.Queries = append(emission.Request.Queries, added)

	for _, testCase := range testCases {
		naming := emission.Naming(testCase.query)
		if testCase.want == "" {
			require.Nil(t, naming, testCase.name)
			continue
		}
		require.NotNil(t, naming, testCase.name)
		require.Equal(t, testCase.want, naming.Method, testCase.name)
	}
}

func TestEmissionRecordsTheDeclarationsTheirReferencesAndTheirFilesImports(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		emission *Emission
		name     string
	}{
		{name: "an emission that started", emission: emissionWith(&Driver{})},
		{name: "an emission with nothing recorded yet", emission: &Emission{}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tracker := goast.NewImportTracker()
			tracker.AddImport("database/sql")
			declaration := &ast.FuncDecl{
				Name: goastutil.CachedIdent("GetBoat"),
				Type: &ast.FuncType{Params: &ast.FieldList{}},
				Body: goastutil.BlockStmt(goastutil.ExprStmt(goastutil.CallExpr(goastutil.CachedIdent("sglSplice")))),
			}

			testCase.emission.RecordDeclarations([]ast.Decl{declaration}, tracker)

			require.Equal(t, []ast.Decl{declaration}, testCase.emission.Declarations)
			require.Contains(t, testCase.emission.Referenced, "sglSplice")
			require.Equal(t, "database/sql", testCase.emission.Qualifiers["sql"])
		})
	}
}
