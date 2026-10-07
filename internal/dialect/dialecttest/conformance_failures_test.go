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

package dialecttest_test

import (
	"context"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialecttest"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	deepNestingParentheses = 1000
	lenientQuery           = "SELECT id FROM items;"
)

var smallSpanStatements = []string{
	"SELECT id, name AS label FROM items WHERE id IN ($1) AND name = $2 ORDER BY id DESC;",
	"INSERT INTO items (id, name) VALUES ($1, $2), ($3, lower($4));",
}

type perturbedPostgres struct {
	*postgres.Dialect
	info                func(dialect.Info) dialect.Info
	parse               func(ctx context.Context, base *postgres.Dialect, sql string) ([]ast.ParsedStatement, error)
	analyse             func(analysis *ast.RawQueryAnalysis)
	apply               func(ctx context.Context, base *postgres.Dialect, parsed ast.ParsedStatement) (*ast.CatalogueMutation, error)
	builtinTypes        func(types *ast.TypeCatalogue) *ast.TypeCatalogue
	ignoresCancellation bool
}

func (p perturbedPostgres) Info() dialect.Info {
	if p.info == nil {
		return p.Dialect.Info()
	}
	return p.info(p.Dialect.Info())
}

func (p perturbedPostgres) ParseStatements(ctx context.Context, sql string) ([]ast.ParsedStatement, error) {
	if p.ignoresCancellation {
		ctx = context.WithoutCancel(ctx)
	}
	if p.parse == nil {
		return p.Dialect.ParseStatements(ctx, sql)
	}
	return p.parse(ctx, p.Dialect, sql)
}

func (p perturbedPostgres) AnalyseQuery(ctx context.Context, catalogue *sqlmodel.Catalogue, parsed ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
	if p.ignoresCancellation {
		ctx = context.WithoutCancel(ctx)
	}
	analysis, err := p.Dialect.AnalyseQuery(ctx, catalogue, parsed)
	if p.analyse != nil && err == nil && analysis != nil {
		p.analyse(analysis)
	}
	return analysis, err
}

func (p perturbedPostgres) ApplyDDL(ctx context.Context, parsed ast.ParsedStatement) (*ast.CatalogueMutation, error) {
	if p.apply == nil {
		return p.Dialect.ApplyDDL(ctx, parsed)
	}
	return p.apply(ctx, p.Dialect, parsed)
}

func (p perturbedPostgres) BuiltinTypes() *ast.TypeCatalogue {
	if p.builtinTypes == nil {
		return p.Dialect.BuiltinTypes()
	}
	return p.builtinTypes(p.Dialect.BuiltinTypes())
}

func postgresFixture(edit func(fixture *dialecttest.Fixture)) func() dialecttest.Fixture {
	return func() dialecttest.Fixture {
		fixture := postgresFamilyFixture(postgres.New)
		fixture.Spans = &dialecttest.SpanFixture{Statements: smallSpanStatements, Corpora: []fs.FS{smallCorpus()}}
		edit(&fixture)
		return fixture
	}
}

func perturbedFixture(perturbation perturbedPostgres) func() dialecttest.Fixture {
	return postgresFixture(func(fixture *dialecttest.Fixture) {
		fixture.NewDialect = func() dialect.DialectPort {
			fresh := perturbation
			fresh.Dialect = postgres.New()
			return fresh
		}
	})
}

func smallCorpus() fs.FS {
	return fstest.MapFS{
		"001_star/testspec.json":               {Data: []byte(`{"description": "a star over items"}`)},
		"001_star/migrations/001_items.up.sql": {Data: []byte("CREATE TABLE items (id integer PRIMARY KEY, name text);")},
		"001_star/queries/get_user.sql":        {Data: []byte("-- sgl.query(name: GetUser, command: one)\nSELECT * FROM items WHERE id = $1;")},
	}
}

func silentParser() perturbedPostgres {
	return perturbedPostgres{parse: func(context.Context, *postgres.Dialect, string) ([]ast.ParsedStatement, error) {
		return nil, nil
	}}
}

func lenientParser() perturbedPostgres {
	return perturbedPostgres{parse: func(ctx context.Context, base *postgres.Dialect, sql string) ([]ast.ParsedStatement, error) {
		if strings.Contains(sql, "CREATE TABLE") {
			return base.ParseStatements(ctx, sql)
		}
		return base.ParseStatements(ctx, lenientQuery)
	}}
}

func flatteningParser() perturbedPostgres {
	return perturbedPostgres{parse: func(ctx context.Context, base *postgres.Dialect, sql string) ([]ast.ParsedStatement, error) {
		if strings.Count(sql, "(") > deepNestingParentheses {
			return base.ParseStatements(ctx, "SELECT 1;")
		}
		return base.ParseStatements(ctx, sql)
	}}
}

func ignoringCancellation() perturbedPostgres {
	return perturbedPostgres{ignoresCancellation: true}
}

func analysisEdit(edit func(analysis *ast.RawQueryAnalysis)) perturbedPostgres {
	return perturbedPostgres{analyse: edit}
}

func infoEdit(edit func(info *dialect.Info)) perturbedPostgres {
	return perturbedPostgres{info: func(info dialect.Info) dialect.Info {
		edit(&info)
		return info
	}}
}

func appliesNothing() perturbedPostgres {
	return perturbedPostgres{apply: func(context.Context, *postgres.Dialect, ast.ParsedStatement) (*ast.CatalogueMutation, error) {
		return nil, nil
	}}
}

func dropsOutputColumns(analysis *ast.RawQueryAnalysis) {
	analysis.OutputColumns = nil
}

func dropsSpans(analysis *ast.RawQueryAnalysis) {
	analysis.Spans = ast.StatementSpans{}
}

func dropsClauseExpressions(analysis *ast.RawQueryAnalysis) {
	analysis.ClauseExpressions = nil
}

func dropsParameters(analysis *ast.RawQueryAnalysis) {
	analysis.ParameterReferences = nil
}

func minimalExpressions(info *dialect.Info) {
	info.Expressions = ast.SQLFeatureBinaryComparison
}

func renamesColumnsOfStarlessQueries(analysis *ast.RawQueryAnalysis) {
	if slices.ContainsFunc(analysis.OutputColumns, func(column ast.RawOutputColumn) bool { return column.IsStar }) {
		return
	}
	for index := range analysis.OutputColumns {
		analysis.OutputColumns[index].Name += "_renamed"
	}
}

func recordsAListForEveryPlaceholder(analysis *ast.RawQueryAnalysis) {
	for index := range analysis.Spans.Placeholders {
		analysis.Spans.Placeholders[index].List = ast.ListSpan{
			Operator: ast.Span{Start: 0, End: 1},
			List:     ast.Span{Start: 0, End: 1},
			Negated:  false,
		}
	}
}

func dropsTypeAttributes(types *ast.TypeCatalogue) *ast.TypeCatalogue {
	stripped := &ast.TypeCatalogue{Types: make(map[string]sqlmodel.SQLType, len(types.Types))}
	for name, sqlType := range types.Types {
		stripped.Types[name] = sqlmodel.NewSQLType(sqlType.Category, sqlType.EngineName)
	}
	return stripped
}

func withMartianNotInQuery() dialecttest.Fixture {
	fixture := martianFixture()
	fixture.NotInQuery = "-- sgl.query(name: ExcludeItems, command: many)\nSELECT id FROM items WHERE id NOT IN (@p1);"
	return fixture
}

func TestEachCheckReportsHowADialectFailsIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		fixture func() dialecttest.Fixture
		check   string
		expect  string
	}{
		{check: "StatementSplitting", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			fixture.SplitterMigration += "\nCREATE TABLE gamma (id integer);"
		}), expect: "the two-statement migration must split into exactly the tables alpha and beta, got map["},
		{check: "BeginAliasSplitsBothStatements", fixture: perturbedFixture(silentParser()),
			expect: `"SELECT 1 AS begin; SELECT 2;" must split into exactly two statements, got 0`},
		{check: "ParameterTracking", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			fixture.ParamQuery = "-- sgl.query(name: GetItem, command: one)\nSELECT id, name FROM items WHERE id = $1 AND name = $2;"
		}), expect: "exactly one parameter expected, got 2"},
		{check: "QuotedIdentifier", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			fixture.QuotedMigration = `CREATE TABLE "other name" (id integer);`
		}), expect: `the delimited table name "weird name" should appear in the catalogue`},
		{check: "CommentStripping", fixture: perturbedFixture(appliesNothing()),
			expect: "the table defined amid comments should appear in the catalogue"},
		{check: "UnterminatedBlockCommentErrors", fixture: perturbedFixture(lenientParser()),
			expect: "an unterminated block comment must produce a Q010 diagnostic, got []"},
		{check: "DiagnosticsIgnoreTheDirectivePrefix", fixture: perturbedFixture(appliesNothing()),
			expect: "a migration with a doubled comma: want a Q010 error on line 2, got []"},
		{check: "DeepNestingTerminates", fixture: perturbedFixture(flatteningParser()),
			expect: "the depth guard truncates the nested expression, so its column must degrade to the unknown category, got integer"},
		{check: "InfoIsValid", fixture: perturbedFixture(infoEdit(func(info *dialect.Info) { *info = dialect.Info{} })),
			expect: "Info.Name must name the dialect"},
		{check: "ReservedWordColumns", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: "GetReserved must output id and group, got []"},
		{check: "DeclaredColumnsReachCatalogue", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			fixture.TypedColumns = append(slices.Clone(fixture.TypedColumns), "missing")
		}), expect: "typed_items declares the columns [id, created_at, updated_at, tags, label, missing] but the catalogue holds"},
		{check: "PlaceholdersBecomeParameters", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			fixture.PlaceholderCount = 3
		}), expect: "the query holds 3 distinct placeholders but reports 2 parameters"},
		{check: "NotInListBindsTheColumn", fixture: perturbedFixture(appliesNothing()),
			expect: "the NOT IN placeholder should take the integer type of the column id, got unknown"},
		{check: "UsingJoinColumnAppearsOnce", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: "SELECT * over a USING join should return 3 columns, got 0"},
		{check: "TrailingTokensRejected", fixture: perturbedFixture(lenientParser()),
			expect: `the tokens " ) )" after a complete statement must produce an error diagnostic, got []`},
		{check: "UnrecognisedStatementRejected", fixture: perturbedFixture(lenientParser()),
			expect: `"SELEC id FROM items;" must be rejected with a Q010 parse error, got []`},
		{check: "UnbalancedConditionRejected", fixture: perturbedFixture(lenientParser()),
			expect: `"SELECT id FROM items WHERE id = (;" must be rejected with a Q010 parse error, got []`},
		{check: "EmptyConditionRejected", fixture: perturbedFixture(lenientParser()),
			expect: `"SELECT id FROM items WHERE;" must be rejected with a Q010 parse error, got []`},
		{check: "DanglingOperatorRejected", fixture: perturbedFixture(lenientParser()),
			expect: `"SELECT id FROM items WHERE id = 1 AND;" must be rejected with a Q010 parse error, got []`},
		{check: "ConditionLeftoversRejected", fixture: perturbedFixture(lenientParser()),
			expect: `"SELECT id FROM items WHERE id = 1 name;" must be rejected with a Q010 parse error, got []`},
		{check: "FromIsNeverASelectedColumn", fixture: perturbedFixture(lenientParser()),
			expect: `"SELECT FROM items;" must not read FROM as a selected column, got the output columns [id]`},
		{check: "ExpressionKindsFollowInfo", fixture: perturbedFixture(infoEdit(minimalExpressions)),
			expect: `"SELECT (SELECT id FROM items)[1] FROM items" holds a scalar subquery, which Info.Expressions does not declare`},
		{check: "LexicalReadsPlaceholderForms", fixture: perturbedFixture(infoEdit(func(info *dialect.Info) {
			info.Placeholders.Sigils = []dialect.PlaceholderSigil{{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderAnonymous}}
		})), expect: `Info.Placeholders.Sigils[0]: Info.Lexical must read "?" and "?" as parameter tokens`},
		{check: "ByteOrderMarkMigration", fixture: perturbedFixture(appliesNothing()),
			expect: "the table created after a byte order mark should appear in the catalogue"},
		{check: "ByteOrderMarkQuery", fixture: perturbedFixture(analysisEdit(dropsSpans)),
			expect: "a query file starting with a byte order mark must read cleanly, got [Q030"},
		{check: "UnknownColumnInWhere", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: `"SELECT id FROM items WHERE missing_column = 1;" must produce a Q001 diagnostic, got []`},
		{check: "UnknownColumnInOrderBy", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: `"SELECT id FROM items ORDER BY missing_column;" must produce a Q001 diagnostic, got []`},
		{check: "UnknownColumnInSet", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: `"UPDATE items SET missing_column = 1 WHERE id = 1;" must produce a Q001 diagnostic, got []`},
		{check: "UnknownNamesInNestedQueriesAreReported", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: `"WITH found AS (SELECT id FROM items) SELECT id FROM items WHERE found.id = 1;" must produce a Q001 diagnostic, got []`},
		{check: "InsertShapesTheEngineRefusesAreReported", fixture: perturbedFixture(analysisEdit(dropsSpans)),
			expect: `"INSERT INTO items (id, name) VALUES (1);" must produce a Q064 diagnostic, got []`},
		{check: "UnknownFunctionInWhere", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: `"SELECT id FROM items WHERE missing_function(id) = 1;" must produce a Q005 diagnostic, got []`},
		{check: "UnknownColumnInSelectListReportedOnce", fixture: perturbedFixture(silentParser()),
			expect: `the unknown column missing_column of "SELECT missing_column = 1 AS compared, coalesce(other_missing, 1) AS fallback FROM items;" must be reported once`},
		{check: "SelectListAliasesFollowInfo", fixture: perturbedFixture(infoEdit(minimalExpressions)),
			expect: "names the alias bumped in a later select item, which Info.SelectAliases does not make visible there"},
		{check: "PredicatesHaveTheStatedType", fixture: perturbedFixture(infoEdit(minimalExpressions)),
			expect: "must analyse without diagnostics, got [Q004"},
		{check: "PredicatesOverNullableOperandsAreNullable", fixture: perturbedFixture(infoEdit(minimalExpressions)),
			expect: "the queries must analyse without diagnostics, got [Q004"},
		{check: "OuterJoinedRelationsAreNullable", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: `"SELECT u.id AS table_id FROM items u RIGHT JOIN items t ON u.id = t.id" must be analysed into one column`},
		{check: "UngroupedColumnsFollowTheDialect", fixture: perturbedFixture(appliesNothing()),
			expect: "the column id read beside count(*) without GROUP BY must be reported once as Q062 error"},
		{check: "AssignedExpressionParametersAreNotTypedAsTheColumn", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: `is integer "int4", the type of the column id it is part of the value of`},
		{check: "ComparedExpressionParametersAreNotTypedAsTheColumn", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: "the parameter compared with coalesce(name, 'none') in"},
		{check: "RowComparisonsTypeTheirParameters", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: "but must take the integer type of the column at its position"},
		{check: "SharedTypeArgumentsTakeTheirSiblingsType", fixture: perturbedFixture(analysisEdit(dropsSpans)),
			expect: `"UPDATE items SET id = greatest(id, $1), name = greatest($2, name)" must analyse without diagnostics, got [Q030`},
		{check: "RowValuesFollowTheDialect", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: "a RowValueTyperPort's queries naming a relation as a value must analyse without diagnostics"},
		{check: "ParameterExpressionsCarryTheirNumbers", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: "the placeholders must be analysed into the parameter expressions [parameter 1 in"},
		{check: "ParametersInStatementsRunWithoutAnalysisAreRefused", fixture: perturbedFixture(silentParser()),
			expect: `the parameter in "SET LOCAL app.user_id = $1" must be refused with a Q010 error saying no parameter can be bound there`},
		{check: "ComparedParametersAreTyped", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: `is unknown "", but it is compared with id + 1, which is integer "int4"`},
		{check: "CoalescedParametersTakeTheirSiblingsType", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: "but must take the integer type of id, which COALESCE resolves it to"},
		{check: "QuantifiedArrayParametersAreArraysOfTheComparedType", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: "but must be an array of id's integer type"},
		{check: "ComparedParametersAreNamedAfterTheirColumns", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: "are named [p1 p2 p3 x], but must be named"},
		{check: "ParametersStandingForConditionsAreBooleans", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: "but stands for a condition, so it must be boolean"},
		{check: "ValuesWithoutAColumnListTypeTheirParametersByPosition", fixture: perturbedFixture(analysisEdit(dropsSpans)),
			expect: `"INSERT INTO items VALUES ($1, $2);" must analyse into two parameters without diagnostics`},
		{check: "SelectAliasOperandsTypeParameters", fixture: perturbedFixture(perturbedPostgres{
			info: func(info dialect.Info) dialect.Info {
				info.SelectAliasOperands = ast.ClauseWhere
				info.SelectAliases |= ast.ClauseWhere
				return info
			},
			analyse: dropsParameters,
		}), expect: "must analyse without diagnostics where Info.SelectAliasOperands holds WHERE"},
		{check: "NestedQueriesSeeEnclosingRelations", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: "the queries must analyse without diagnostics, got [Q009"},
		{check: "RowLocksAnywhereRouteToTheWriter", fixture: perturbedFixture(analysisEdit(func(analysis *ast.RawQueryAnalysis) {
			analysis.ReadOnly = false
		})), expect: `"SELECT id FROM items WHERE id = 1" locks no rows and changes none, so it must be read-only`},
		{check: "ScalarSubqueriesTakeTheirColumnsType", fixture: perturbedFixture(infoEdit(minimalExpressions)),
			expect: "unsupported expression: scalar subquery"},
		{check: "ParametersComparedWithScalarSubqueriesAreTyped", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: `a parameter compared with (SELECT max(i.id) FROM items i) is unknown "", but must take the subquery's integer type`},
		{check: "ColumnListsNameTheColumns", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: `query "DerivedColumnList" uses command "many" but produces no output columns`},
		{check: "AliasedJoinsAreRelations", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: `query "AliasedJoinStar" uses command "many" but produces no output columns`},
		{check: "ValuesListsStandWhereQueriesDo", fixture: perturbedFixture(infoEdit(minimalExpressions)),
			expect: "unsupported expression: binary arithmetic"},
		{check: "ScalarFunctionsInFromFollowTheDialect", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: "calls of clock_timestamp() in a select list and in FROM must analyse without diagnostics"},
		{check: "ColumnOverridesFollowDeclaredTypes", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			declared := *fixture.DeclaredParameters
			declared.Restating, declared.Differing = declared.Differing, declared.Restating
			fixture.DeclaredParameters = &declared
		}), expect: "declares value's own type or none, so it must take the column override of declared_items.value"},
		{check: "ColumnOverridesFollowProjectedTypes", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			declared := *fixture.DeclaredParameters
			declared.Projections, declared.CastProjections = declared.CastProjections, declared.Projections
			fixture.DeclaredParameters = &declared
		}), expect: "returns value as it is or as its own type, so it must take the column override of declared_items.value"},
		{check: "PlaceholderSpansCoverEveryParameterToken", fixture: perturbedFixture(analysisEdit(dropsSpans)),
			expect: `the placeholder "$1" at 49 is covered by 0 spans, want 1`},
		{check: "PlaceholdersAreParametersOfTheStatement", fixture: perturbedFixture(analysisEdit(dropsParameters)),
			expect: `the placeholder "$1" names no parameter of the statement`},
		{check: "ClauseSpansAreOrderedAndInside", fixture: perturbedFixture(analysisEdit(dropsSpans)),
			expect: "the statement writes WHERE but records no Where"},
		{check: "ProjectionItemsMatchOutputColumns", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: "ProjectionItems records 2 items for 0 output columns"},
		{check: "InListSpansOnlyForSoleElements", fixture: perturbedFixture(analysisEdit(recordsAListForEveryPlaceholder)),
			expect: `the placeholder "$2" is not the sole element of an IN list but records one`},
		{check: "ValuesTupleItemsAreCommaSeparated", fixture: perturbedFixture(analysisEdit(dropsSpans)),
			expect: "an INSERT ... VALUES records no VALUES rows"},
		{check: "OrderByPlaceIsRecorded", fixture: perturbedFixture(analysisEdit(dropsSpans)),
			expect: "a query must record OrderByPlace as a zero-width span inside it, got {Start:0 End:0}"},
		{check: "TemplatesRenderTheKeptSQL", fixture: perturbedFixture(analysisEdit(dropsSpans)),
			expect: "001_star query GetUser has no template"},
		{check: "ExpandedStarsReadTheStarsColumns", fixture: perturbedFixture(analysisEdit(renamesColumnsOfStarlessQueries)),
			expect: "reads [id_renamed integer int4, name_renamed text text], but the stars read [id integer int4, name text text]"},
		{check: "ExtraTypesResolve", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			fixture.NewDialectWithExtraType = func(string, sqlmodel.SQLType) dialect.DialectPort { return postgres.New() }
		}), expect: `the extra type "seagull_conformance_type" must resolve to the text category, got unknown`},
		{check: "FlavourOptionsAreAdditive", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			fixture.Flavour = &dialecttest.FlavourFixture{New: func() dialect.DialectPort { return postgres.New(postgresUserOptions...) },
				UserFunction: userFunctionName, FlavourFunctions: []string{"no_such_function"}, FlavourTypes: nil, WithheldFunctions: nil}
		}), expect: "the function no_such_function is missing once the caller's options apply; options must add to the flavour's settings, not replace them"},
		{check: "FlavourWithholdsWhatItsEngineLacks", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			fixture.Flavour = &dialecttest.FlavourFixture{New: func() dialect.DialectPort { return postgres.New(postgresUserOptions...) },
				UserFunction: userFunctionName, FlavourFunctions: nil, FlavourTypes: nil, WithheldFunctions: []string{"lower"}}
		}), expect: "the flavour registers lower, which its engine lacks"},
		{check: "BuiltinTypesCarryStructuralAttributes", fixture: perturbedFixture(perturbedPostgres{builtinTypes: dropsTypeAttributes}),
			expect: `the built-in integer type "bigint" has no structural attributes`},
		{check: "TokeniserSpansMatchSharedScanner", fixture: withMartianNotInQuery,
			expect: "NotInQuery: ParseStatements refuses text sqllex splits"},
		{check: "ReturningFollowsInfo", fixture: perturbedFixture(silentParser()),
			expect: "Info.Returning includes INSERT, so its RETURNING clause must generate without an error"},
		{check: "GroupingModifiersLeaveKeysOut", fixture: perturbedFixture(silentParser()),
			expect: `"SELECT id, count(*) AS total FROM items GROUP BY ROLLUP (id);" must analyse: parsed into 0 statements, want 1`},
		{check: "ElementsTheEngineCanHoldNullInArePointers", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: "reading readings must analyse without diagnostics"},
		{check: "AggregatedNonNullElementsAreNotPointers", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: `"SELECT array_agg(id) AS ids FROM samples" must analyse without diagnostics`},
		{check: "ArrayParametersHoldNullElementsWhereTheEngineCan", fixture: perturbedFixture(analysisEdit(dropsClauseExpressions)),
			expect: "SELECT id FROM samples WHERE id = ANY($1) must be analysed into one parameter of an array, list or map type"},
		{check: "BracketedValuesLeaveLaterParametersTheirColumns", fixture: perturbedFixture(analysisEdit(dropsSpans)),
			expect: `"INSERT INTO samples (readings, id) VALUES (ARRAY[1, 2], $1)" must analyse without diagnostics`},
		{check: "CancelledContextStopsWork", fixture: perturbedFixture(ignoringCancellation()),
			expect: "ParseStatements with a cancelled context must return its cause or error, got <nil>"},
		{check: "CancellationStopsARunningAnalysis", fixture: perturbedFixture(ignoringCancellation()),
			expect: "AnalyseQuery cancelled while it walks a statement must stop with the context's error, got <nil> and an analysis of 4000 columns"},
		{check: "IfExistsFormsReplayAsTheEngineDoes", fixture: perturbedFixture(appliesNothing()),
			expect: "items must keep exactly the columns [id name], got []"},
		{check: "PrimaryKeyColumnsFollowTheEngine", fixture: perturbedFixture(appliesNothing()),
			expect: "KeyedTables must create at least one table with the column left_id"},
		{check: "ColumnPlacementsReplayAsTheEngineDoes", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			replay := *fixture.Replay
			replay.Placement = "CREATE TABLE placed (a integer, b integer);"
			replay.PlacedColumns = []string{"b", "a"}
			fixture.Replay = &replay
		}), expect: "placed must hold the columns [b a] in that order, as the engine keeps them, got [a b]"},
		{check: "RenamesReplayAsTheEngineDoes", fixture: perturbedFixture(appliesNothing()),
			expect: "the relation renamed_items must hold the columns [id name] after the renames, got []"},
		{check: "ViewStarsExpandToTheirColumns", fixture: perturbedFixture(appliesNothing()),
			expect: "the view star_view must hold the columns [id name] its star stands for, got []"},
		{check: "ViewsFollowTheirTablesAsTheDialectSays", fixture: perturbedFixture(appliesNothing()),
			expect: "the view star_view must hold the columns [id name] once items gains extra, as Info.ViewsFollowTheirTables is false, got []"},
		{check: "MissingSemicolonIsReported", fixture: perturbedFixture(appliesNothing()),
			expect: "whose first semicolon is missing, must produce an error diagnostic, got []"},
		{check: "AcceptedDDLReplaysWithoutAnError", fixture: postgresFixture(func(fixture *dialecttest.Fixture) {
			replay := *fixture.Replay
			replay.AcceptedForms = []string{"CREATE TABLE broken (id integer,, name text);"}
			fixture.Replay = &replay
		}), expect: `is valid on the engine and must replay without an error, got [Q010`},
		{check: "DerivedRelationsAreQueryable", fixture: perturbedFixture(analysisEdit(dropsOutputColumns)),
			expect: "every query reading a derived relation must analyse without a diagnostic"},
	}

	covered := make(map[string]bool, len(testCases))
	for _, testCase := range testCases {
		covered[testCase.check] = true
	}
	for _, name := range dialecttest.CheckNamesForTest() {
		require.True(t, covered[name], "no case makes %s fail", name)
	}

	for _, testCase := range testCases {
		t.Run(testCase.check, func(t *testing.T) {
			t.Parallel()

			fixture := testCase.fixture()
			problems, notApplicable := dialecttest.RunCheckForTest(t.Context(), testCase.check, &fixture)
			require.Empty(t, notApplicable)
			require.True(t, slices.ContainsFunc(problems, func(problem string) bool { return strings.Contains(problem, testCase.expect) }),
				"no problem of %s holds %q: %v", testCase.check, testCase.expect, problems)
		})
	}
}
