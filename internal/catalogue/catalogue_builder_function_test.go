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

package catalogue

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type capturedRecords struct {
	records []slog.Record
	mu      sync.Mutex
}

func (captured *capturedRecords) Enabled(context.Context, slog.Level) bool {
	return true
}

func (captured *capturedRecords) Handle(_ context.Context, record slog.Record) error {
	captured.mu.Lock()
	defer captured.mu.Unlock()
	captured.records = append(captured.records, record)
	return nil
}

func (captured *capturedRecords) WithAttrs([]slog.Attr) slog.Handler {
	return captured
}

func (captured *capturedRecords) WithGroup(string) slog.Handler {
	return captured
}

func (captured *capturedRecords) levelOf(message string) (slog.Level, bool) {
	captured.mu.Lock()
	defer captured.mu.Unlock()
	for index := range captured.records {
		if captured.records[index].Message == message {
			return captured.records[index].Level, true
		}
	}
	return 0, false
}

func replayPostgresFunctions(t *testing.T, migrations ...string) (*catalogueBuilder, []diag.SourceError) {
	t.Helper()

	builder := newCatalogueBuilder(postgres.New(), namespace.Default())
	diagnostics := make([]diag.SourceError, 0, len(migrations))
	for index, migration := range migrations {
		diagnostics = append(diagnostics, applyMigration(t, builder, "migration.sql", migration, index)...)
	}
	return builder, diagnostics
}

func sqlFunctionEngine(bodies map[string][]*ast.RawQueryAnalysis, panicsIn string) *fakedialect.Engine {
	return &fakedialect.Engine{
		SchemaSearchPathFunc: func() []string { return []string{"public"} },
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			switch name {
			case "integer":
				return sqlmodel.NewIntegerType("integer", 32, false)
			case "text":
				return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
			default:
				return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, name)
			}
		},
		CoreTypesFunc: func() dialect.CoreTypes {
			core := fakedialect.DefaultCoreTypes()
			core.Void = sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "void")
			return core
		},
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			if panicsIn == "parse" && bodies[sql] != nil {
				panic("parsing the body")
			}
			count := max(len(bodies[sql]), 1)
			statements := make([]ast.ParsedStatement, count)
			for index := range statements {
				statements[index] = ast.ParsedStatement{Location: index, Length: 1}
			}
			return statements, nil
		},
		AnalyseQueryFunc: func(_ context.Context, _ *sqlmodel.Catalogue, statement ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			if panicsIn == "analyse" {
				panic("analysing the body")
			}
			for _, analyses := range bodies {
				if statement.Location < len(analyses) {
					return analyses[statement.Location], nil
				}
			}
			return nil, errors.New("no analysis")
		},
	}
}

func literalColumn(typeName string) ast.RawOutputColumn {
	return ast.RawOutputColumn{Expression: &ast.LiteralExpression{TypeName: typeName}, Name: "value"}
}

func TestASQLFunctionBodyIsClassifiedByEveryStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		body       string
		wantAccess sqlmodel.FunctionDataAccess
		wantCalls  []string
	}{
		{name: "a delete before a select", body: "DELETE FROM audit; SELECT 1::bigint;",
			wantAccess: sqlmodel.DataAccessModifiesData, wantCalls: nil},
		{name: "an insert before a select", body: "INSERT INTO audit (note) VALUES ('x'); SELECT 1::bigint;",
			wantAccess: sqlmodel.DataAccessModifiesData, wantCalls: nil},
		{name: "a select before a delete", body: "SELECT 1::bigint; DELETE FROM audit;",
			wantAccess: sqlmodel.DataAccessModifiesData, wantCalls: nil},
		{name: "two selects", body: "SELECT count(*) FROM audit; SELECT 1::bigint;",
			wantAccess: sqlmodel.DataAccessReadOnly, wantCalls: []string{"count"}},
		{name: "calls in each statement", body: "SELECT write_audit(); SELECT read_audit();",
			wantAccess: sqlmodel.DataAccessReadOnly, wantCalls: []string{"read_audit", "write_audit"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, diagnostics := replayPostgresFunctions(t, "CREATE TABLE audit (note text);\n"+
				"CREATE FUNCTION purge_audit() RETURNS bigint LANGUAGE sql AS $$ "+testCase.body+" $$;")

			require.Empty(t, diagnosticMessages(diagnostics))
			function := builder.Catalogue().Schemas["public"].Functions["purge_audit"][0]
			assert.Equal(t, testCase.wantAccess, function.DataAccess)
			assert.Equal(t, testCase.wantCalls, builder.calledFunctions[function])
		})
	}
}

func TestACallerOfAFunctionWritingInAnEarlierStatementChangesData(t *testing.T) {
	t.Parallel()

	builder, diagnostics := replayPostgresFunctions(t, "CREATE TABLE audit (note text);",
		"CREATE FUNCTION write_audit() RETURNS void LANGUAGE plpgsql AS $$ BEGIN DELETE FROM audit; END $$;",
		"CREATE FUNCTION tidy() RETURNS bigint LANGUAGE sql AS $$ SELECT write_audit(); SELECT 1::bigint; $$;",
		"CREATE FUNCTION reader() RETURNS bigint LANGUAGE sql AS $$ SELECT tidy(); $$;")
	require.Empty(t, diagnosticMessages(diagnostics))

	catalogue := builder.Catalogue()
	propagateDataAccess(catalogue, builder.calledFunctions, builder.calledTableFunctions, postgres.New().BuiltinFunctions())

	for _, name := range []string{"write_audit", "tidy", "reader"} {
		assert.Equal(t, sqlmodel.DataAccessModifiesData, catalogue.Schemas["public"].Functions[name][0].DataAccess, name)
	}
}

func TestABodyTypesItsFunctionFromItsLastStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		returnType   sqlmodel.SQLType
		statements   []*ast.RawQueryAnalysis
		wantCategory sqlmodel.SQLTypeCategory
		wantName     string
	}{
		{
			name:       "an unknown return type takes the last statement's column",
			returnType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
			statements: []*ast.RawQueryAnalysis{
				{OutputColumns: []ast.RawOutputColumn{literalColumn("text")}, ReadOnly: true},
				{OutputColumns: []ast.RawOutputColumn{literalColumn("integer")}, ReadOnly: true},
			},
			wantCategory: sqlmodel.TypeCategoryInteger,
			wantName:     "integer",
		},
		{
			name:       "a last statement of two columns leaves the return type unknown",
			returnType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
			statements: []*ast.RawQueryAnalysis{
				{OutputColumns: []ast.RawOutputColumn{literalColumn("text"), literalColumn("integer")}, ReadOnly: true},
			},
			wantCategory: sqlmodel.TypeCategoryUnknown,
			wantName:     "",
		},
		{
			name:       "a function returning the dialect's void keeps it",
			returnType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "void"),
			statements: []*ast.RawQueryAnalysis{
				{OutputColumns: []ast.RawOutputColumn{literalColumn("integer")}, ReadOnly: true},
			},
			wantCategory: sqlmodel.TypeCategoryText,
			wantName:     "void",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const body = "SELECT body"
			builder := newCatalogueBuilder(sqlFunctionEngine(map[string][]*ast.RawQueryAnalysis{body: testCase.statements}, ""),
				namespace.Default())

			require.NoError(t, builder.applyMutation(t.Context(), ast.NewCatalogueMutation(ast.MutationCreateFunction, "public", "",
				ast.WithFunction(ast.NewFunctionDefinition(&sqlmodel.Function{
					Name: "typed", Language: "sql", BodySQL: body, ReturnType: testCase.returnType,
				})))))

			function := builder.Catalogue().Schemas["public"].Functions["typed"][0]
			assert.Equal(t, testCase.wantCategory, function.ReturnType.Category)
			assert.Equal(t, testCase.wantName, function.ReturnType.EngineName)
			assert.Equal(t, sqlmodel.DataAccessReadOnly, function.DataAccess)
		})
	}
}

func TestAPanicReadingAFunctionBodyIsReportedAndTheFunctionKept(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		panicsIn string
	}{
		{name: "parsing the body", panicsIn: "parse"},
		{name: "analysing the body", panicsIn: "analyse"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const body = "DELETE FROM audit"
			engine := sqlFunctionEngine(map[string][]*ast.RawQueryAnalysis{body: {{ReadOnly: false}}}, testCase.panicsIn)
			engine.ApplyDDLFunc = func(context.Context, ast.ParsedStatement) (*ast.CatalogueMutation, error) {
				return ast.NewCatalogueMutation(ast.MutationCreateFunction, "public", "", ast.WithFunction(ast.NewFunctionDefinition(
					&sqlmodel.Function{Name: "purge", Language: "sql", BodySQL: body,
						ReturnType: sqlmodel.NewIntegerType("integer", 32, false)}))), nil
			}
			builder := newCatalogueBuilder(engine, namespace.Default())

			var diagnostics []diag.SourceError
			require.NotPanics(t, func() {
				diagnostics = applyMigration(t, builder, "001.up.sql", "\nCREATE FUNCTION purge() ...;", 0)
			})

			require.Len(t, diagnostics, 1)
			assert.Equal(t, diag.CodeRecoveredPanic, diagnostics[0].Code)
			assert.Contains(t, diagnostics[0].Message, "the body of function purge")
			overloads := builder.Catalogue().Schemas["public"].Functions["purge"]
			require.Len(t, overloads, 1)
			assert.Equal(t, sqlmodel.DataAccessModifiesData, overloads[0].DataAccess, "the scan of the body still sees the write")
		})
	}
}

func TestAFunctionBodyThatDoesNotParseIsLoggedAtTheInternalLevel(t *testing.T) {
	t.Parallel()

	captured := &capturedRecords{records: nil, mu: sync.Mutex{}}
	ctx := logging.ContextWithLogger(t.Context(), slog.New(captured))
	engine := sqlFunctionEngine(nil, "")
	engine.ParseStatementsFunc = func(context.Context, string) ([]ast.ParsedStatement, error) {
		return nil, errors.New("unexpected token")
	}
	builder := newCatalogueBuilder(engine, namespace.Default())

	require.NoError(t, builder.applyMutation(ctx, ast.NewCatalogueMutation(ast.MutationCreateFunction, "public", "",
		ast.WithFunction(ast.NewFunctionDefinition(&sqlmodel.Function{Name: "broken", Language: "sql", BodySQL: "SELEKT 1"})))))

	level, logged := captured.levelOf("function body parse degraded; scanning the body instead")
	require.True(t, logged)
	assert.Equal(t, logging.LevelInternal, level)
}

func TestAnExpressionBodyTypesItsFunction(t *testing.T) {
	t.Parallel()

	builder := newCatalogueBuilder(clickhouse.New(), namespace.Default())

	diagnostics := applyMigration(t, builder, "001.sql", "CREATE FUNCTION answer AS () -> 42;", 0)

	require.Empty(t, diagnosticMessages(diagnostics))
	overloads := builder.Catalogue().LookupSchema("").Functions["answer"]
	require.Len(t, overloads, 1)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, overloads[0].ReturnType.Category)
}

func TestQuotedAndUnquotedFunctionNamesStayDistinct(t *testing.T) {
	t.Parallel()

	const setup = `CREATE FUNCTION "Foo"() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;
CREATE FUNCTION foo() RETURNS int LANGUAGE sql AS $$ SELECT 2 $$;
`
	testCases := []struct {
		wantNames map[string][]string
		name      string
		statement string
	}{
		{name: "both are kept, each under its own key", statement: "", wantNames: map[string][]string{"Foo": {"Foo"}, "foo": {"foo"}}},
		{name: "replacing one keeps the other", statement: "CREATE OR REPLACE FUNCTION foo() RETURNS int LANGUAGE sql AS $$ SELECT 3 $$;",
			wantNames: map[string][]string{"Foo": {"Foo"}, "foo": {"foo"}}},
		{name: "dropping the quoted one keeps the other", statement: `DROP FUNCTION "Foo"();`, wantNames: map[string][]string{"foo": {"foo"}}},
		{name: "dropping the unquoted one keeps the other", statement: "DROP FUNCTION foo;", wantNames: map[string][]string{"Foo": {"Foo"}}},
		{name: "an unquoted mixed-case name folds onto the unquoted function", statement: "CREATE OR REPLACE FUNCTION FOO() RETURNS int LANGUAGE sql AS $$ SELECT 4 $$;",
			wantNames: map[string][]string{"Foo": {"Foo"}, "foo": {"foo"}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, diagnostics := replayPostgresFunctions(t, setup+testCase.statement)

			require.Empty(t, diagnosticMessages(diagnostics))
			names := make(map[string][]string)
			for key, overloads := range builder.Catalogue().Schemas["public"].Functions {
				for _, overload := range overloads {
					names[key] = append(names[key], overload.Name)
				}
			}
			assert.Equal(t, testCase.wantNames, names)
		})
	}
}

func TestMixedCaseLambdaFunctionsAreKeptApartByTheirCase(t *testing.T) {
	t.Parallel()

	const setup = `CREATE FUNCTION MyFunc AS x -> x * 2;
CREATE FUNCTION myfunc AS x -> toString(x);
`
	testCases := []struct {
		wantNames map[string][]string
		name      string
		statement string
	}{
		{name: "both are kept, each under its own key", statement: "", wantNames: map[string][]string{"MyFunc": {"MyFunc"}, "myfunc": {"myfunc"}}},
		{name: "dropping one keeps the other", statement: "DROP FUNCTION MyFunc;", wantNames: map[string][]string{"myfunc": {"myfunc"}}},
		{name: "dropping a name in another case leaves both", statement: "DROP FUNCTION IF EXISTS MYFUNC;",
			wantNames: map[string][]string{"MyFunc": {"MyFunc"}, "myfunc": {"myfunc"}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := newCatalogueBuilder(clickhouse.New(), namespace.Default())
			diagnostics := applyMigration(t, builder, "001.sql", setup+testCase.statement, 0)

			require.Empty(t, diagnosticMessages(diagnostics))
			names := make(map[string][]string)
			for key, overloads := range builder.Catalogue().LookupSchema("").Functions {
				for _, overload := range overloads {
					names[key] = append(names[key], overload.Name)
				}
			}
			assert.Equal(t, testCase.wantNames, names)
		})
	}
}

func TestALoadedExtensionFunctionIsFiledUnderTheKeyOfItsName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		identifiers sqlmodel.IdentifierCase
		wantKey     string
	}{
		{name: "a case-sensitive dialect files it as named",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: false}, wantKey: "Gen_Random"},
		{name: "a case-insensitive dialect files it under its lower-case key",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true}, wantKey: "gen_random"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &mockExtensionLoaderEngine{
				InfoFunc: func() dialect.Info {
					return dialect.Info{SchemaSearchPath: []string{"public"}, Identifiers: testCase.identifiers}
				},
				loadExtensionFunctionsHook: func(string) []*sqlmodel.Function {
					return []*sqlmodel.Function{{Name: "Gen_Random"}}
				},
			}
			builder := newCatalogueBuilder(engine, namespace.Default())

			require.NoError(t, builder.applyMutation(t.Context(), &ast.CatalogueMutation{Kind: ast.MutationCreateExtension, TableName: "random"}))

			functions := builder.Catalogue().Schemas["public"].Functions
			require.Len(t, functions, 1)
			require.Len(t, functions[testCase.wantKey], 1)
			assert.Equal(t, "Gen_Random", functions[testCase.wantKey][0].Name)
		})
	}
}

func TestABodyScanFindsStatementsThatWriteWithoutTakingColumnsForThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
		want sqlmodel.FunctionDataAccess
	}{
		{name: "dynamic SQL", body: "BEGIN EXECUTE 'DELETE FROM t'; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "dynamic SQL built by a call", body: "BEGIN EXECUTE format('TRUNCATE %I', name); END;", want: sqlmodel.DataAccessModifiesData},
		{name: "rows returned from dynamic SQL", body: "BEGIN RETURN QUERY EXECUTE 'SELECT 1'; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a loop over dynamic SQL", body: "BEGIN FOR r IN EXECUTE 'SELECT 1' LOOP NULL; END LOOP; END;",
			want: sqlmodel.DataAccessModifiesData},
		{name: "a materialised view refreshed", body: "BEGIN REFRESH MATERIALIZED VIEW mv; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a copy into a table", body: "BEGIN COPY t FROM '/tmp/t.csv'; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a procedure called", body: "BEGIN CALL archive_rows(); END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a table locked", body: "BEGIN LOCK TABLE t IN EXCLUSIVE MODE; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a channel notified", body: "BEGIN NOTIFY changes; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a privilege granted", body: "BEGIN GRANT SELECT ON t TO reader; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a privilege revoked", body: "BEGIN REVOKE ALL ON t FROM reader; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a vacuum", body: "VACUUM;", want: sqlmodel.DataAccessModifiesData},
		{name: "a reindex", body: "BEGIN REINDEX TABLE t; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a cluster", body: "BEGIN CLUSTER t; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a foreign schema imported", body: "BEGIN IMPORT FOREIGN SCHEMA remote FROM SERVER s INTO local; END;",
			want: sqlmodel.DataAccessModifiesData},
		{name: "a comment set", body: "BEGIN COMMENT ON TABLE t IS 'x'; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a security label set", body: "BEGIN SECURITY LABEL ON TABLE t IS 'secret'; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a statement after a branch", body: "BEGIN IF x THEN CALL p(); ELSE COPY t TO STDOUT; END IF; END;",
			want: sqlmodel.DataAccessModifiesData},
		{name: "a statement after a label", body: "BEGIN <<outer>> EXECUTE 'DELETE FROM t'; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "columns named like statement keywords", body: "SELECT comment, lock, call, copy FROM notes WHERE lock = false",
			want: sqlmodel.DataAccessReadOnly},
		{name: "columns in a CASE", body: "BEGIN RETURN CASE WHEN flag THEN comment ELSE lock END; END;", want: sqlmodel.DataAccessReadOnly},
		{name: "columns assigned", body: "BEGIN lock := true; comment := 'x'; copy.total := 1; END;", want: sqlmodel.DataAccessReadOnly},
		{name: "columns a branch returns", body: "BEGIN IF x THEN RETURN call; ELSE RETURN copy; END IF; END;",
			want: sqlmodel.DataAccessReadOnly},
		{name: "keywords inside strings and comments", body: "BEGIN -- EXECUTE 'DELETE'\nRETURN 'CALL p(); COPY t'; END;",
			want: sqlmodel.DataAccessReadOnly},
		{name: "a column named like a keyword after a branch", body: "SELECT CASE WHEN a THEN execute ELSE grant END FROM t",
			want: sqlmodel.DataAccessReadOnly},
	}

	scanner := postgresBodyScanner()
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, scanner.dataAccess(testCase.body))
		})
	}
}

func TestAProceduralFunctionWritingThroughDynamicSQLChangesData(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
		want sqlmodel.FunctionDataAccess
	}{
		{name: "EXECUTE", body: "BEGIN EXECUTE 'DELETE FROM t'; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "REFRESH", body: "BEGIN REFRESH MATERIALIZED VIEW mv; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "COPY", body: "BEGIN COPY t FROM '/tmp/t.csv'; END;", want: sqlmodel.DataAccessModifiesData},
		{name: "a read of columns named comment and lock", body: "BEGIN RETURN (SELECT count(*) FROM t WHERE comment = lock); END;",
			want: sqlmodel.DataAccessReadOnly},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, diagnostics := replayPostgresFunctions(t, "CREATE FUNCTION job() RETURNS bigint LANGUAGE plpgsql AS $$ "+
				testCase.body+" $$;")

			require.Empty(t, diagnosticMessages(diagnostics))
			assert.Equal(t, testCase.want, builder.Catalogue().Schemas["public"].Functions["job"][0].DataAccess)
		})
	}
}

func TestAnSQLFunctionBodyOfTooManyStatementsIsScanned(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("SELECT 1; ", maxBodyStatements) + "DELETE FROM audit;"
	builder, diagnostics := replayPostgresFunctions(t, "CREATE TABLE audit (note text);\n"+
		"CREATE FUNCTION long_job() RETURNS bigint LANGUAGE sql AS $$ "+body+" $$;")

	require.Empty(t, diagnosticMessages(diagnostics))
	function := builder.Catalogue().Schemas["public"].Functions["long_job"][0]
	assert.Equal(t, sqlmodel.DataAccessModifiesData, function.DataAccess)
}

func TestWordOperatorsOfKeepsOnlyTheOperatorsSpeltAsAWord(t *testing.T) {
	testCases := []struct {
		name      string
		operators []string
		want      map[string]struct{}
	}{
		{name: "no operators", operators: nil, want: map[string]struct{}{}},
		{name: "symbols are left out", operators: []string{"?", "?|", "@>"}, want: map[string]struct{}{}},
		{name: "a word is kept in lower case", operators: []string{"ILIKE", "?", "SIMILAR_TO"},
			want: map[string]struct{}{"ilike": {}, "similar_to": {}}},
		{name: "an empty spelling is left out", operators: []string{""}, want: map[string]struct{}{}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, wordOperatorsOf(testCase.operators))
		})
	}
}

func TestADialectsWordOperatorAfterAKeywordShowsTheKeywordIsAnOperand(t *testing.T) {
	scanner := newBodyScanner(postgres.New())

	assert.Equal(t, sqlmodel.DataAccessReadOnly,
		scanner.dataAccess("SELECT id FROM notes WHERE comment ILIKE '%draft%';"))
	assert.Equal(t, sqlmodel.DataAccessModifiesData,
		scanner.dataAccess("COMMENT ON TABLE notes IS 'audited';"))
}

func TestAFunctionBodyWhoseAnalysisHoldsANilNodeIsReportedAndTheFunctionKept(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		analysis *ast.RawQueryAnalysis
	}{
		{name: "a nil call in the select list", analysis: &ast.RawQueryAnalysis{
			OutputColumns: []ast.RawOutputColumn{{Name: "v", Expression: (*ast.FunctionCallExpression)(nil)}}, ReadOnly: true}},
		{name: "a nil column in a nested query", analysis: &ast.RawQueryAnalysis{ReadOnly: true,
			PredicateSubqueries: []*ast.RawQueryAnalysis{{ClauseExpressions: []ast.ClauseExpression{
				{Expression: (*ast.ColumnRefExpression)(nil), Clause: ast.ClauseWhere}}}}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const body = "DELETE FROM audit"
			engine := sqlFunctionEngine(map[string][]*ast.RawQueryAnalysis{body: {testCase.analysis}}, "")
			engine.ApplyDDLFunc = func(context.Context, ast.ParsedStatement) (*ast.CatalogueMutation, error) {
				return ast.NewCatalogueMutation(ast.MutationCreateFunction, "public", "", ast.WithFunction(ast.NewFunctionDefinition(
					&sqlmodel.Function{Name: "purge", Language: "sql", BodySQL: body,
						ReturnType: sqlmodel.NewIntegerType("integer", 32, false)}))), nil
			}
			builder := newCatalogueBuilder(engine, namespace.Default())

			var diagnostics []diag.SourceError
			require.NotPanics(t, func() {
				diagnostics = applyMigration(t, builder, "001.up.sql", "\nCREATE FUNCTION purge() ...;", 0)
			})

			require.Len(t, diagnostics, 1)
			assert.Equal(t, diag.CodeInternalNilGuard, diagnostics[0].Code)
			assert.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			assert.Contains(t, diagnostics[0].Message, "the body of function purge")
			overloads := builder.Catalogue().Schemas["public"].Functions["purge"]
			require.Len(t, overloads, 1)
			assert.Equal(t, sqlmodel.DataAccessModifiesData, overloads[0].DataAccess, "the scan of the body still sees the write")
		})
	}
}

func TestATableFunctionABodyReadsDecidesWhetherItsFunctionWrites(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		body       string
		wantAccess sqlmodel.FunctionDataAccess
	}{
		{name: "a catalogue function that writes", body: "SELECT note FROM write_audit()", wantAccess: sqlmodel.DataAccessModifiesData},
		{name: "a catalogue function that reads", body: "SELECT note FROM read_audit()", wantAccess: sqlmodel.DataAccessReadOnly},
		{name: "a built-in table function", body: "SELECT n::text FROM generate_series(1, 3) AS n", wantAccess: sqlmodel.DataAccessReadOnly},
		{name: "a table function neither holds", body: "SELECT x FROM external_rows() AS r(x text)", wantAccess: sqlmodel.DataAccessReadOnly},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, diagnostics := replayPostgresFunctions(t, "CREATE TABLE audit (note text);",
				"CREATE FUNCTION write_audit() RETURNS TABLE (note text) LANGUAGE plpgsql AS $$ BEGIN DELETE FROM audit; RETURN; END $$;",
				"CREATE FUNCTION read_audit() RETURNS TABLE (note text) LANGUAGE sql STABLE AS $$ SELECT note FROM audit $$;",
				"CREATE FUNCTION reader() RETURNS SETOF text LANGUAGE sql AS $$ "+testCase.body+" $$;")
			require.Empty(t, diagnosticMessages(diagnostics))

			catalogue := builder.Catalogue()
			propagateDataAccess(catalogue, builder.calledFunctions, builder.calledTableFunctions, postgres.New().BuiltinFunctions())

			reader := catalogue.Schemas["public"].Functions["reader"][0]
			assert.Equal(t, testCase.wantAccess, reader.DataAccess)
		})
	}
}

func TestAQuotedCalleeReadsEachDoubledQuoteAsOne(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		engine   dialect.DialectPort
		body     string
		expected []string
	}{
		{
			name:     "a doubled double quote is one quote",
			engine:   postgres.New(),
			body:     `BEGIN PERFORM "a""b"(1); END;`,
			expected: []string{`a"b`},
		},
		{
			name:     "each part of a qualified callee reads its doubled quotes",
			engine:   postgres.New(),
			body:     `BEGIN PERFORM "Audit""Log"."Write""Event"(1); END;`,
			expected: []string{`Audit"Log.Write"Event`},
		},
		{
			name:     "a run of doubled quotes reads as a run of quotes",
			engine:   postgres.New(),
			body:     `BEGIN PERFORM "a""""b"(1); END;`,
			expected: []string{`a""b`},
		},
		{
			name:     "a doubled backtick is one backtick",
			engine:   clickhouse.New(),
			body:     "SELECT `a``b`(1)",
			expected: []string{"a`b"},
		},
		{
			name:     "a quoted callee without doubled quotes is kept as written",
			engine:   postgres.New(),
			body:     `BEGIN PERFORM "Write"(1); END;`,
			expected: []string{"Write"},
		},
		{
			name:     "a bracketed callee is kept as written",
			engine:   sqlite.New(),
			body:     "SELECT [Write](1)",
			expected: []string{"Write"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, newBodyScanner(testCase.engine).calledFunctions(testCase.body))
		})
	}
}

func TestScanBodyForDML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		expected sqlmodel.FunctionDataAccess
	}{
		{
			name:     "body containing INSERT returns data-modifying",
			body:     "BEGIN INSERT INTO users (name) VALUES ('test'); END;",
			expected: sqlmodel.DataAccessModifiesData,
		},
		{
			name:     "body containing UPDATE returns data-modifying",
			body:     "BEGIN UPDATE users SET name = 'changed'; END;",
			expected: sqlmodel.DataAccessModifiesData,
		},
		{
			name:     "body containing DELETE returns data-modifying",
			body:     "BEGIN DELETE FROM users WHERE id = 1; END;",
			expected: sqlmodel.DataAccessModifiesData,
		},
		{
			name:     "body containing TRUNCATE returns data-modifying",
			body:     "BEGIN TRUNCATE TABLE users; END;",
			expected: sqlmodel.DataAccessModifiesData,
		},
		{
			name:     "body containing only SELECT returns read-only",
			body:     "SELECT id, name FROM users WHERE active = true;",
			expected: sqlmodel.DataAccessReadOnly,
		},
		{
			name:     "empty body returns read-only",
			body:     "",
			expected: sqlmodel.DataAccessReadOnly,
		},
		{
			name:     "case-insensitive detection of insert",
			body:     "insert into logs (msg) values ('hello');",
			expected: sqlmodel.DataAccessModifiesData,
		},
		{
			name:     "keyword without trailing space is not matched",
			body:     "SELECT INSERTING FROM flags;",
			expected: sqlmodel.DataAccessReadOnly,
		},
		{
			name:     "a keyword at the end of a line is matched",
			body:     "BEGIN\n  UPDATE\n    users SET active = false;\nEND;",
			expected: sqlmodel.DataAccessModifiesData,
		},
		{
			name:     "a keyword inside a string literal is not matched",
			body:     "BEGIN RAISE NOTICE 'UPDATE users later'; END;",
			expected: sqlmodel.DataAccessReadOnly,
		},
		{
			name:     "a keyword inside a comment is not matched",
			body:     "BEGIN -- DELETE FROM users\n RETURN 1; /* INSERT INTO x */ END;",
			expected: sqlmodel.DataAccessReadOnly,
		},
		{
			name:     "a quoted identifier is not a keyword",
			body:     "SELECT \"delete\" FROM flags;",
			expected: sqlmodel.DataAccessReadOnly,
		},
		{
			name:     "keyword substring in identifier is not matched",
			body:     "SELECT updated_at FROM records;",
			expected: sqlmodel.DataAccessReadOnly,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := postgresBodyScanner().dataAccess(testCase.body)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestScanBodyForCalledFunctions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		expected []string
	}{
		{
			name:     "PERFORM of a writing helper records the callee",
			body:     "BEGIN PERFORM write_audit(NEW.id); RETURN NEW; END;",
			expected: []string{"write_audit"},
		},
		{
			name:     "schema-qualified call recorded under its dotted form",
			body:     "BEGIN PERFORM audit.write_event(NEW.id); END;",
			expected: []string{"audit.write_event"},
		},
		{
			name:     "control-flow keywords before a paren are not callees",
			body:     "BEGIN IF (x > 0) THEN PERFORM touch(x); END IF; END;",
			expected: []string{"touch"},
		},
		{
			name:     "whitespace between name and paren still matches",
			body:     "BEGIN PERFORM do_thing  (1); END;",
			expected: []string{"do_thing"},
		},
		{
			name:     "body with no calls returns nil",
			body:     "BEGIN x := 1; END;",
			expected: nil,
		},
		{
			name:     "calls inside strings and comments are not callees",
			body:     "BEGIN RAISE NOTICE 'fake(1)'; -- other(2)\n PERFORM real_work(3); END;",
			expected: []string{"real_work"},
		},
		{
			name:     "a type with a modifier is not a callee",
			body:     "DECLARE total numeric(10, 2); label varchar(20); BEGIN RETURN round(total); END;",
			expected: []string{"round"},
		},
		{
			name:     "an insert column list is not a callee",
			body:     "BEGIN INSERT INTO audit (note) VALUES (lower(name)); END;",
			expected: []string{"lower"},
		},
		{
			name:     "a quoted callee is recorded without its quotes, in the case it is written",
			body:     "BEGIN PERFORM \"Audit\".\"Write\"(1); END;",
			expected: []string{"Audit.Write"},
		},
		{
			name:     "an unquoted callee is folded as the dialect folds an unquoted name",
			body:     "BEGIN PERFORM Audit.Write_Event(1); END;",
			expected: []string{"audit.write_event"},
		},
		{
			name:     "a set-returning call in FROM is a callee",
			body:     "BEGIN RETURN QUERY SELECT * FROM generate_series(1, 3); END;",
			expected: []string{"generate_series"},
		},
		{
			name:     "empty body returns nil",
			body:     "",
			expected: nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := postgresBodyScanner().calledFunctions(testCase.body)
			assert.ElementsMatch(t, testCase.expected, result)
		})
	}
}

func postgresBodyScanner() bodyScanner {
	engine := postgres.New()
	return bodyScanner{rules: engine.Info().Lexical, builtinTypes: engine.BuiltinTypes(), builtinFunctions: engine.BuiltinFunctions(),
		identifiers: engine.Info().Identifiers}
}

func TestSyntheticTableReturnTypeName_DistinguishesByCategory(t *testing.T) {
	t.Parallel()

	scalar := &sqlmodel.Function{
		Name: "udf_x",
		Arguments: []sqlmodel.FunctionArgument{
			{Name: "input", Type: sqlmodel.SQLType{
				EngineName: "text",
				Category:   sqlmodel.TypeCategoryText,
			}},
		},
	}
	array := &sqlmodel.Function{
		Name: "udf_x",
		Arguments: []sqlmodel.FunctionArgument{
			{Name: "input", Type: sqlmodel.SQLType{
				EngineName: "text",
				Category:   sqlmodel.TypeCategoryArray,
			}},
		},
	}

	require.NotEqual(t, syntheticTableReturnTypeName(scalar), syntheticTableReturnTypeName(array),
		"category must be encoded in synthetic name to disambiguate overloads")
}

func TestSyntheticTableReturnTypeName_DistinguishesByEngineName(t *testing.T) {
	t.Parallel()

	textSignature := &sqlmodel.Function{
		Name: "udf_y",
		Arguments: []sqlmodel.FunctionArgument{
			{Name: "input", Type: sqlmodel.SQLType{
				EngineName: "text",
				Category:   sqlmodel.TypeCategoryText,
			}},
		},
	}
	intSignature := &sqlmodel.Function{
		Name: "udf_y",
		Arguments: []sqlmodel.FunctionArgument{
			{Name: "input", Type: sqlmodel.SQLType{
				EngineName: "integer",
				Category:   sqlmodel.TypeCategoryInteger,
			}},
		},
	}

	require.NotEqual(t, syntheticTableReturnTypeName(textSignature), syntheticTableReturnTypeName(intSignature))
}
