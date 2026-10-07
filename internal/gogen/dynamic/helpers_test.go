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

package dynamic

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/preset"
	"seagull.sh/seagull/internal/sqlmodel"
)

const importSQL = "database/sql"

var dollar = dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}

type namedBinder struct{}

func (namedBinder) Bind(access ast.Expr, parameter *sqlmodel.QueryParameter, _ *goast.ImportTracker) ast.Expr {
	return goastutil.CallExpr(goastutil.SelectorExpr("driver", "Named"), goastutil.StrLit(parameter.Name), access)
}

func testRequest() *emit.Request {
	settings := preset.DefaultSettings()
	return &emit.Request{
		Catalogue:         nil,
		Types:             nil,
		Package:           "db",
		Style:             settings.Style,
		Queries:           nil,
		DirectivePrefixes: settings.Prefixes.Accepted(),
		Naming:            emit.Naming{},
		Dialect:           dialect.Facts{Name: "test", Family: "test"},
	}
}

func testVocabulary() gogen.Vocabulary {
	return gogen.NewVocabulary(testRequest())
}

func question(numbered bool) dialect.PlaceholderSigil {
	kind := dialect.PlaceholderAnonymous
	if numbered {
		kind = dialect.PlaceholderNumbered
	}
	return dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: kind}
}

func testDialectInfo(execution, positional dialect.PlaceholderSigil) dialect.Facts {
	info := dialect.Facts{}
	info.CommentPrefix = "--"
	info.Name = "test"
	info.Family = "test"
	info.Placeholders = dialect.Placeholders{
		Sigils: []dialect.PlaceholderSigil{
			{Prefix: ":", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
			{Prefix: "@", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
			{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
			execution,
		},
		Execution:  execution,
		Positional: positional,
		NamePrefix: "",
	}
	info.IdentifierQuote = dialect.IdentifierQuote{Open: `"`, Close: `"`}
	info.MaxParameters = 999
	info.Identifiers = sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true}
	return info
}

func testDriver() *gogen.Driver {
	return &gogen.Driver{
		Binder:  nil,
		Reader:  nil,
		Helpers: nil,
		Batch:   nil,
		Querier: emitkit.QuerierSpec{
			Transactions: nil,
			Connections:  emitkit.Connections{Reader: goast.IdentReader, Writer: goast.IdentWriter},
			DBTX:         nil,
			Replica:      false,
			WrapDBTX:     false,
		},
		Connection: gogen.Connection{
			ExecResult:         &sqlmodel.GoType{Composite: nil, Package: importSQL, Name: "Result"},
			Query:              "QueryContext",
			QueryRow:           "QueryRowContext",
			Exec:               "ExecContext",
			Reader:             goast.IdentReader,
			Writer:             goast.IdentWriter,
			CountScan:          "",
			NoRows:             sqlmodel.GoType{Composite: nil, Package: importSQL, Name: "ErrNoRows"},
			RowsAffectedErrors: true,
		},
		Lists: emitkit.ListExpand,
	}
}

func testEmission(info dialect.Facts, mappings sqlmodel.GoTypeResolver, driver *gogen.Driver) *gogen.Emission {
	request := testRequest()
	request.Package = "mypkg"
	request.Dialect = info
	request.Types = mappings
	return gogen.NewEmission(request, driver)
}

func indexedEmission(numbered bool) *gogen.Emission {
	return testEmission(testDialectInfo(question(numbered), dialect.PlaceholderSigil{}), textMappings(), testDriver())
}

func textMappings() sqlmodel.GoTypeResolver {
	return &gotypes.MappingTable{Mappings: []gotypes.Mapping{
		{
			SQLCategory: sqlmodel.TypeCategoryInteger,
			NotNull:     sqlmodel.GoType{Composite: nil, Package: "", Name: "int64"},
			Nullable:    sqlmodel.GoType{Composite: nil, Package: "", Name: "*int64"},
		},
		{
			SQLCategory: sqlmodel.TypeCategoryText,
			NotNull:     sqlmodel.GoType{Composite: nil, Package: "", Name: "string"},
			Nullable:    sqlmodel.GoType{Composite: nil, Package: "", Name: "*string"},
		},
	}}
}

func textColumn(name string, nullable bool) sqlmodel.OutputColumn {
	return sqlmodel.OutputColumn{
		Name:     name,
		SQLType:  sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
		Nullable: nullable,
	}
}

func textParam(name string, number int) sqlmodel.QueryParameter {
	return sqlmodel.QueryParameter{
		Name:    name,
		Number:  number,
		SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
		Kind:    sqlmodel.ParameterDirectiveParam,
	}
}

func runtimeEmission() *gogen.Emission {
	return testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), textMappings(), testDriver())
}

func text(value string) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentText, Text: value}
}

func parameter(index int) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentParameter, Parameter: index}
}

func clause(kind sqlmodel.SQLSegmentKind, children ...sqlmodel.SQLSegment) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: kind, Present: len(children) > 0, Children: children}
}

func template(segments ...sqlmodel.SQLSegment) sqlmodel.SQLTemplate {
	return sqlmodel.SQLTemplate{Segments: segments}
}

func queryFor(t *testing.T, emission *gogen.Emission, analysed *sqlmodel.AnalysedQuery) *gogen.Query {
	t.Helper()
	emission.Request.Queries = append(emission.Request.Queries, analysed)
	query, err := emission.Query(analysed, goast.NewImportTracker())
	require.NoError(t, err)
	return query
}

func renderDecl(t *testing.T, declaration ast.Decl) string {
	t.Helper()
	var buffer bytes.Buffer
	require.NoError(t, printer.Fprint(&buffer, token.NewFileSet(), declaration))
	return buffer.String()
}

func renderDecls(t *testing.T, declarations []ast.Decl) string {
	t.Helper()
	var builder strings.Builder
	for _, declaration := range declarations {
		builder.WriteString(renderDecl(t, declaration))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func renderStatements(t *testing.T, statements []ast.Stmt) string {
	t.Helper()
	var buffer bytes.Buffer
	for _, statement := range statements {
		require.NoError(t, printer.Fprint(&buffer, token.NewFileSet(), statement))
		buffer.WriteByte('\n')
	}
	return buffer.String()
}
