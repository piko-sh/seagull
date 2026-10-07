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
	"context"
	"go/ast"
	"regexp"
	"strconv"
	"strings"

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

var statementParameters = []emitkit.DBTXParameter{
	{Name: "ctx", Type: sqlmodel.GoType{Composite: nil, Package: "context", Name: "Context"}, Variadic: false},
	{Name: "query", Type: sqlmodel.GoType{Composite: nil, Package: "", Name: "string"}, Variadic: false},
	{Name: "args", Type: sqlmodel.GoType{Composite: nil, Package: "", Name: "any"}, Variadic: true},
}

var placeholderPattern = regexp.MustCompile(`\$[0-9]+|\?[0-9]*|:[a-z_]+|\{[a-z_]+:[A-Za-z0-9]+\}`)

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
			DBTX: []emitkit.DBTXMethod{
				{Name: "ExecContext", Parameters: statementParameters, Results: []sqlmodel.GoType{{Composite: nil, Package: importSQL, Name: "Result"}, {Composite: nil, Package: "", Name: "error"}}},
				{Name: "QueryContext", Parameters: statementParameters, Results: []sqlmodel.GoType{{Composite: nil, Package: importSQL, Name: "*Rows"}, {Composite: nil, Package: "", Name: "error"}}},
				{Name: "QueryRowContext", Parameters: statementParameters, Results: []sqlmodel.GoType{{Composite: nil, Package: importSQL, Name: "*Row"}}},
			},
			Replica:  false,
			WrapDBTX: false,
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

func emitFile(ctx context.Context, emission *gogen.Emission, mappings sqlmodel.GoTypeResolver, filename string, queries []*sqlmodel.AnalysedQuery) (sqlmodel.GeneratedFile, error) {
	emission.Request.Types = mappings
	for _, query := range queries {
		withTemplate(query)
	}
	emission.Request.Queries = queries
	return emitQueryFile(ctx, emission, filename, queries)
}

func dollarEmission() *gogen.Emission {
	return testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), textMappings(), testDriver())
}

func bracedEmission() *gogen.Emission {
	braced := dialect.PlaceholderSigil{Prefix: "{", Suffix: "}", TypeSeparator: ":", Kind: dialect.PlaceholderNamed}
	return testEmission(testDialectInfo(braced, dollar), textMappings(), testDriver())
}

func namedEmission(numbered bool) *gogen.Emission {
	driver := testDriver()
	driver.Binder = namedBinder{}
	return testEmission(testDialectInfo(question(numbered), dialect.PlaceholderSigil{}), textMappings(), driver)
}

func emitQueriesFor(ctx context.Context, emission *gogen.Emission, queries []*sqlmodel.AnalysedQuery) ([]sqlmodel.GeneratedFile, error) {
	for _, query := range queries {
		withTemplate(query)
	}
	emission.Request.Queries = queries
	return emitQueries(ctx, emission)
}

func queryNamesFor(ctx context.Context, queries []*sqlmodel.AnalysedQuery) (sqlmodel.GeneratedFile, error) {
	emission := testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), textMappings(), testDriver())
	emission.Request.Package = "generated"
	for _, query := range queries {
		withTemplate(query)
	}
	emission.Request.Queries = queries
	emission.Request.Style.QueryNames = emit.QueryNames{Enabled: true, File: "query_names.go", Function: "QueryName"}
	return queryNamesFile(ctx, emission)
}

func sqlText(value string) sqlmodel.SQLSegment {
	return text(value)
}

func text(value string) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentText, Text: value}
}

func parameter(index int) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentParameter, Parameter: index}
}

func list(index int) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentList, Parameter: index, Text: "IN "}
}

func clause(kind sqlmodel.SQLSegmentKind, children ...sqlmodel.SQLSegment) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: kind, Present: len(children) > 0, Children: children}
}

func template(segments ...sqlmodel.SQLSegment) sqlmodel.SQLTemplate {
	return sqlmodel.SQLTemplate{Segments: segments}
}

func withTemplate(query *sqlmodel.AnalysedQuery) *sqlmodel.AnalysedQuery {
	if len(query.Template.Segments) > 0 || query.SQL == "" {
		return query
	}
	var segments []sqlmodel.SQLSegment
	cursor, ordinal := 0, 0
	for _, found := range placeholderPattern.FindAllStringIndex(query.SQL, -1) {
		token := query.SQL[found[0]:found[1]]
		index := placeholderParameter(query, token, &ordinal)
		if index < 0 {
			continue
		}
		start := found[0]
		segment := sqlmodel.SQLSegment{Kind: sqlmodel.SegmentParameter, Parameter: index}
		if name, typeText, found := strings.Cut(token, ":"); strings.HasPrefix(token, "{") && found && name != "" {
			segment.TypeText = strings.TrimSuffix(typeText, "}")
		}
		if query.Parameters[index].IsSlice && strings.HasSuffix(strings.ToUpper(strings.TrimRight(query.SQL[:start], " (")), "IN") {
			operator := strings.LastIndex(strings.ToUpper(query.SQL[:start]), "IN")
			if strings.HasSuffix(strings.ToUpper(strings.TrimRight(query.SQL[:operator], " ")), "NOT") {
				operator = strings.LastIndex(strings.ToUpper(query.SQL[:operator]), "NOT")
				segment.Negated = true
			}
			segment.Kind = sqlmodel.SegmentList
			segment.Text = query.SQL[operator:strings.LastIndex(query.SQL[:start], "(")]
			start = operator
			found[1] = strings.Index(query.SQL[found[1]:], ")") + found[1] + 1
		}
		segments = appendText(segments, query.SQL[cursor:start])
		segments = append(segments, segment)
		cursor = found[1]
	}
	query.Template = sqlmodel.SQLTemplate{Segments: appendText(segments, query.SQL[cursor:])}
	return query
}

func appendText(segments []sqlmodel.SQLSegment, value string) []sqlmodel.SQLSegment {
	if value == "" {
		return segments
	}
	return append(segments, text(value))
}

func placeholderParameter(query *sqlmodel.AnalysedQuery, token string, ordinal *int) int {
	switch {
	case token == "?":
		*ordinal++
		return parameterNumbered(query, *ordinal)
	case strings.HasPrefix(token, "?") || strings.HasPrefix(token, "$"):
		number, err := strconv.Atoi(token[1:])
		if err != nil {
			return -1
		}
		return parameterNumbered(query, number)
	default:
		name := strings.Trim(token, ":{}")
		name, _, _ = strings.Cut(name, ":")
		for index := range query.Parameters {
			if query.Parameters[index].Name == name {
				return index
			}
		}
		return -1
	}
}

func parameterNumbered(query *sqlmodel.AnalysedQuery, number int) int {
	for index := range query.Parameters {
		if query.Parameters[index].Number == number {
			return index
		}
	}
	return -1
}
