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

package martian_test

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	emit_clickhouse "seagull.sh/seagull/internal/emit/clickhouse"
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/internal/emit/pgx"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	declaredMigration = "CREATE TABLE declared_items (value TEXTUAL);\n"
	declaredQueries   = `-- sgl.query(name: CompareRestated, command: many)
SELECT value FROM declared_items WHERE value = CONVERT(@p1 TO TEXTUAL) OR CONVERT(@p2 TO TEXTUAL) <> value OR value = @p3;
-- sgl.query(name: ListRestated, command: many)
SELECT value FROM declared_items WHERE value IN (CONVERT(@p1 TO TEXTUAL), @p2);
-- sgl.query(name: InsertRestated, command: exec)
INSERT INTO declared_items (value) VALUES (CONVERT(@p1 TO TEXTUAL));
-- sgl.query(name: UpdateRestated, command: exec)
UPDATE declared_items SET value = CONVERT(@p1 TO TEXTUAL) WHERE value = CONVERT(@p2 TO TEXTUAL);
-- sgl.query(name: CompareDiffering, command: many)
SELECT value FROM declared_items WHERE value = CONVERT(@p1 TO WHOLE32) OR CONVERT(@p2 TO WHOLE32) <> value;
-- sgl.query(name: ListDiffering, command: many)
SELECT value FROM declared_items WHERE value IN (CONVERT(@p1 TO WHOLE32));
-- sgl.query(name: InsertDiffering, command: exec)
INSERT INTO declared_items (value) VALUES (CONVERT(@p1 TO WHOLE32));
-- sgl.query(name: UpdateDiffering, command: exec)
UPDATE declared_items SET value = CONVERT(@p1 TO WHOLE32) WHERE value = CONVERT(@p2 TO WHOLE32);
`
)

const projectedQueries = `-- sgl.query(name: ProjectRestated, command: many)
SELECT value, CONVERT(value TO TEXTUAL) AS restated FROM declared_items;

-- sgl.query(name: ProjectDiffering, command: many)
SELECT CONVERT(value TO WHOLE32) AS whole FROM declared_items;
`

type expectation struct {
	holds        func(t *testing.T) (bool, string)
	name         string
	reason       string
	knownFailure bool
	toolchain    bool
}

var builtInEmitters = map[string]func() emit.EmitterPort{
	"database/sql": func() emit.EmitterPort { return databasesql.New() },
	"pgx":          func() emit.EmitterPort { return pgx.New() },
	"clickhouse":   func() emit.EmitterPort { return emit_clickhouse.New() },
}

var martianExpectations = slices.Concat(coreExpectations, emitterExpectations())

var coreExpectations = []expectation{
	{name: "integer columns map to Go integers of the dialect's widths", holds: integerWidthsFollowTheDialect, reason: "", knownFailure: false, toolchain: false},
	{name: "an @pN directive anchor binds the placeholder it names", holds: directiveAnchorsBindByNumber, reason: "", knownFailure: false, toolchain: false},
	{name: "a slice of @pN placeholders is spliced into its list", holds: sliceExpansionRewritesPlaceholders, reason: "", knownFailure: false, toolchain: false},
	{name: "unqualified names resolve in the DBO schema", holds: unqualifiedNamesResolveInDefaultSchema, reason: "", knownFailure: false, toolchain: false},
	{name: "a DBO-qualified name resolves", holds: qualifiedDefaultSchemaResolves, reason: "", knownFailure: false, toolchain: false},
	{name: "no PUBLIC schema is assumed", holds: noPublicSchemaIsAssumed, reason: "", knownFailure: false, toolchain: false},
	{name: "delimited identifiers keep their case", holds: delimitedIdentifiersKeepTheirCase, reason: "", knownFailure: false, toolchain: false},
	{name: "the || operator is a syntax error", holds: concatenationOperatorIsRejected, reason: "", knownFailure: false, toolchain: false},
	{name: "function results are typed by the dialect's catalogue", holds: functionsAreTypedByTheCatalogue, reason: "", knownFailure: false, toolchain: false},
	{name: "arithmetic promotes through the dialect", holds: arithmeticPromotesThroughTheDialect, reason: "", knownFailure: false, toolchain: false},
	{name: "a compared parameter takes the column's type", holds: comparisonParametersTakeTheColumnType, reason: "", knownFailure: false, toolchain: false},
	{name: "a parameter compared with a select-list alias in WHERE takes the alias's type", holds: aliasOperandsTypeComparedParameters, reason: "", knownFailure: false, toolchain: false},
	{name: "a parameter passed to a call compared with a call over a column is named after the column", holds: wrappedParametersAreNamedAfterTheirColumn, reason: "", knownFailure: false, toolchain: false},
	{name: "a parameter compared with an expression over a column takes the expression's type", holds: expressionComparedParametersTakeTheExpressionsType, reason: "", knownFailure: false, toolchain: false},
	{name: "unknown names in WHERE, ORDER BY and SET are reported where they are written", holds: clauseReferencesAreChecked, reason: "", knownFailure: false, toolchain: false},
	{name: "a column override reaches a parameter declaring the column's type and no other", holds: declaredParametersFollowColumnOverrides, reason: "", knownFailure: false, toolchain: false},
	{name: "a column override follows a projection of the column's own type and no other", holds: projectionsFollowColumnOverrides, reason: "", knownFailure: false, toolchain: false},
	{name: "a star is written as its columns in the dialect's delimiters", holds: starsExpandInTheDialectsDelimiters, reason: "", knownFailure: false, toolchain: false},
	{name: "a comparison over a nullable column is nullable and an IS NULL test never is", holds: predicatesOverNullableColumnsAreNullable, reason: "", knownFailure: false, toolchain: false},
	{name: "a column beside an aggregate without GROUP BY is nullable", holds: ungroupedColumnsAreNullable, reason: "", knownFailure: false, toolchain: false},
	{name: "a scalar call in FROM is one column named after the function or its alias", holds: scalarCallsInFromAreOneColumn, reason: "", knownFailure: false, toolchain: false},
}

func emitterGenerates(newEmitter func() emit.EmitterPort) func(t *testing.T) (bool, string) {
	return func(t *testing.T) (bool, string) {
		t.Helper()

		generated := generate(t, &recordingEmitter{inner: newEmitter(), queries: nil}, martianQueries)
		if generated.result.Err() != nil || len(generated.result.Files) == 0 {
			return false, "generation reported " + diagnosticSummary(generated.result.Diagnostics)
		}
		return len(generated.result.Diagnostics) == 0, "diagnostics: " + diagnosticSummary(generated.result.Diagnostics)
	}
}

func emitterOutputTypeChecks(newEmitter func() emit.EmitterPort) func(t *testing.T) (bool, string) {
	return func(t *testing.T) (bool, string) {
		t.Helper()

		generated := generate(t, &recordingEmitter{inner: newEmitter(), queries: nil}, martianQueries)
		problems := typeCheck(t, generated.result.Files)
		return len(problems) == 0, strings.Join(problems, "; ")
	}
}

func diagnosticSummary(diagnostics []diag.SourceError) string {
	summaries := make([]string, 0, len(diagnostics))
	for index := range diagnostics {
		summaries = append(summaries, diagnostics[index].Code+" "+diagnostics[index].Message)
	}
	return "[" + strings.Join(summaries, "; ") + "]"
}

func integerWidthsFollowTheDialect(t *testing.T) (bool, string) {
	t.Helper()

	generated := generate(t, &recordingEmitter{inner: databasesql.New(), queries: nil}, martianQueries)
	fields := structFieldTypes(t, generated.file(t, "models.go"), "Widths")
	expected := map[string]string{"Small": "int16", "Medium": "int32", "Large": "int64"}
	return len(fields) == len(expected) && fields["Small"] == expected["Small"] &&
		fields["Medium"] == expected["Medium"] && fields["Large"] == expected["Large"], formatFields(fields)
}

func formatFields(fields map[string]string) string {
	return "Small " + fields["Small"] + ", Medium " + fields["Medium"] + ", Large " + fields["Large"]
}

func directiveAnchorsBindByNumber(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, `-- sgl.query(name: RenameReversed, command: exec)
-- @p2 as sgl.param(new_name)
-- @p1 as sgl.param(item_id)
UPDATE items SET name = @p2 WHERE id = @p1;
`)
	query := generated.query(t, "RenameReversed")
	names := make(map[int]string, len(query.Parameters))
	for _, parameter := range query.Parameters {
		names[parameter.Number] = parameter.Name
	}
	holds := !generated.hasCode(diag.CodeUnreferencedParameter) && names[1] == "item_id" && names[2] == "new_name"
	return holds, "parameter names by number: @p1 " + names[1] + ", @p2 " + names[2] + "; diagnostics " + diagnosticSummary(generated.result.Diagnostics)
}

func sliceExpansionRewritesPlaceholders(t *testing.T) (bool, string) {
	t.Helper()

	generated := generate(t, &recordingEmitter{inner: databasesql.New(), queries: nil}, martianQueries)
	content := generated.file(t, "items.sql.go")
	splices := strings.Contains(content, "sglSplice(") && strings.Contains(content, "sglList(sglListMark{")
	return splices, "the ItemsByID method does not splice its @pN list with sglList:\n" + content
}

func unqualifiedNamesResolveInDefaultSchema(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, martianQueries)
	query := generated.query(t, "GetItem")
	return len(generated.result.Diagnostics) == 0 && len(query.OutputColumns) == 4, diagnosticSummary(generated.result.Diagnostics)
}

func qualifiedDefaultSchemaResolves(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, "-- sgl.query(name: Qualified, command: many)\nSELECT id FROM dbo.items;\n")
	return len(generated.result.Diagnostics) == 0, diagnosticSummary(generated.result.Diagnostics)
}

func noPublicSchemaIsAssumed(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, "-- sgl.query(name: Public, command: many)\nSELECT id FROM public.items;\n")
	return generated.hasCode(diag.CodeUnknownTable), diagnosticSummary(generated.result.Diagnostics)
}

func delimitedIdentifiersKeepTheirCase(t *testing.T) (bool, string) {
	t.Helper()

	folded := probe(t, "-- sgl.query(name: Folded, command: many)\nSELECT LABEL FROM MIXED;\n")
	delimited := probe(t, "-- sgl.query(name: Delimited, command: many)\nSELECT [Label] FROM [Mixed];\n")
	holds := folded.hasCode(diag.CodeUnknownTable) && len(delimited.result.Diagnostics) == 0
	return holds, "folded " + diagnosticSummary(folded.result.Diagnostics) + ", delimited " + diagnosticSummary(delimited.result.Diagnostics)
}

func concatenationOperatorIsRejected(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, "-- sgl.query(name: Doubled, command: many)\nSELECT name || name AS doubled FROM items;\n")
	return generated.hasCode(diag.CodeParseError), diagnosticSummary(generated.result.Diagnostics)
}

func outputTypes(t *testing.T, queryName string) (string, sqlmodel.SQLTypeCategory) {
	t.Helper()

	query := probe(t, martianQueries).query(t, queryName)
	column := query.OutputColumns[len(query.OutputColumns)-1]
	return column.SQLType.EngineName, column.SQLType.Category
}

func functionsAreTypedByTheCatalogue(t *testing.T) (bool, string) {
	t.Helper()

	engineName, _ := outputTypes(t, "ListActiveItems")
	return engineName == "TEXTUAL", "CONCAT is typed " + engineName
}

func arithmeticPromotesThroughTheDialect(t *testing.T) (bool, string) {
	t.Helper()

	engineName, _ := outputTypes(t, "NextQuantity")
	return engineName == "WHOLE64", "WHOLE32 + 1 is typed " + engineName
}

func comparisonParametersTakeTheColumnType(t *testing.T) (bool, string) {
	t.Helper()

	query := probe(t, martianQueries).query(t, "GetItem")
	engineName := query.Parameters[0].SQLType.EngineName
	return engineName == "WHOLE64", "@p1 compared with ID is typed " + engineName
}

func wrappedParametersAreNamedAfterTheirColumn(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, "-- sgl.query(name: ByName, command: many)\nSELECT id FROM items WHERE UPPER(name) = UPPER(@p1);\n")
	query := generated.query(t, "ByName")
	name := query.Parameters[0].Name
	return len(generated.result.Diagnostics) == 0 && strings.EqualFold(name, "name"),
		"@p1 passed to UPPER and compared with UPPER(NAME) is named " + name + "; diagnostics " + diagnosticSummary(generated.result.Diagnostics)
}

func expressionComparedParametersTakeTheExpressionsType(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, "-- sgl.query(name: ByLength, command: many)\nSELECT id FROM items WHERE LENGTH(name) = @p1 AND (quantity + 1) = @p2;\n")
	query := generated.query(t, "ByLength")
	measured, bumped := query.Parameters[0], query.Parameters[1]
	return len(generated.result.Diagnostics) == 0 && measured.SQLType.EngineName == "WHOLE64" && !bumped.Nullable,
		"@p1 compared with LENGTH(NAME) is typed " + measured.SQLType.EngineName + ", @p2 compared with (QUANTITY + 1) nullable " +
			strconv.FormatBool(bumped.Nullable) + "; diagnostics " + diagnosticSummary(generated.result.Diagnostics)
}

func aliasOperandsTypeComparedParameters(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, "-- sgl.query(name: Bumped, command: many)\nSELECT quantity + 1 AS next FROM items WHERE next > @p1;\n")
	query := generated.query(t, "Bumped")
	engineName := query.Parameters[0].SQLType.EngineName
	return len(generated.result.Diagnostics) == 0 && engineName == "WHOLE64",
		"@p1 compared with the alias NEXT is typed " + engineName + "; diagnostics " + diagnosticSummary(generated.result.Diagnostics)
}

func clauseReferencesAreChecked(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, `-- sgl.query(name: Misspelt, command: many)
SELECT id FROM items WHERE quantty > @p1 AND MISSING_FN(name) = 'x' ORDER BY nam;

-- sgl.query(name: MisspeltSet, command: exec)
UPDATE items SET nme = @p1 WHERE id = @p2;
`)
	located := make([]string, 0, len(generated.result.Diagnostics))
	for _, diagnostic := range generated.result.Diagnostics {
		located = append(located, fmt.Sprintf("%s %d:%d-%d", diagnostic.Code, diagnostic.Line, diagnostic.Column, diagnostic.EndColumn))
	}
	expected := []string{
		diag.CodeUnknownColumn + " 2:28-35",
		diag.CodeUnknownFunction + " 2:46-56",
		diag.CodeUnknownColumn + " 2:78-81",
		diag.CodeUnknownColumn + " 5:18-21",
	}
	return slices.Equal(located, expected), strings.Join(located, ", ") + " " + diagnosticSummary(generated.result.Diagnostics)
}

func declaredParametersFollowColumnOverrides(t *testing.T) (bool, string) {
	t.Helper()

	override := app.TypeOverride{SQLTypeName: "", Column: "declared_items.value", GoPackage: "example.com/declared", GoName: "Value"}
	generated := generateWith(t, &recordingEmitter{inner: nil, queries: nil}, declaredMigration, declaredQueries,
		app.WithTypeOverrides(override))
	var wrong []string
	for _, query := range generated.queries {
		restating := strings.HasSuffix(query.Name, "Restated")
		for _, parameter := range query.Parameters {
			overridden := parameter.GoTypeOverride != nil && parameter.GoTypeOverride.Name == override.GoName
			if overridden != restating {
				wrong = append(wrong, fmt.Sprintf("%s @p%d overridden %t", query.Name, parameter.Number, overridden))
			}
		}
	}
	holds := len(generated.queries) == 8 && len(wrong) == 0 && len(generated.result.Diagnostics) == 0
	return holds, fmt.Sprintf("%d queries; wrong: %s; diagnostics %s", len(generated.queries), strings.Join(wrong, ", "),
		diagnosticSummary(generated.result.Diagnostics))
}

func projectionsFollowColumnOverrides(t *testing.T) (bool, string) {
	t.Helper()

	override := app.TypeOverride{SQLTypeName: "", Column: "declared_items.value", GoPackage: "example.com/declared", GoName: "Value"}
	generated := generateWith(t, &recordingEmitter{inner: nil, queries: nil}, declaredMigration, projectedQueries,
		app.WithTypeOverrides(override))
	var wrong []string
	for _, query := range generated.queries {
		restating := strings.HasSuffix(query.Name, "Restated")
		for _, column := range query.OutputColumns {
			overridden := column.GoTypeOverride != nil && column.GoTypeOverride.Name == override.GoName
			if overridden != restating {
				wrong = append(wrong, fmt.Sprintf("%s %s overridden %t", query.Name, column.Name, overridden))
			}
		}
	}
	holds := len(generated.queries) == 2 && len(wrong) == 0 && len(generated.result.Diagnostics) == 0
	return holds, fmt.Sprintf("%d queries; wrong: %s; diagnostics %s", len(generated.queries), strings.Join(wrong, ", "),
		diagnosticSummary(generated.result.Diagnostics))
}

func starsExpandInTheDialectsDelimiters(t *testing.T) (bool, string) {
	t.Helper()

	generated := generate(t, &recordingEmitter{inner: databasesql.New(), queries: nil},
		"-- sgl.query(name: ListMixed, command: many)\nSELECT * FROM [Mixed];\n-- sgl.query(name: ListItems, command: many)\nSELECT * FROM items i;\n")
	content := generated.file(t, "items.sql.go")
	holds := strings.Contains(content, "SELECT [Mixed].[Label] FROM [Mixed];") &&
		strings.Contains(content, "SELECT [I].[ID], [I].[NAME], [I].[QUANTITY], [I].[PRICE], [I].[ACTIVE], [I].[CREATED], [I].[display label] FROM items i;")
	return holds, content
}

func emitterExpectations() []expectation {
	expectations := make([]expectation, 0, 2*len(builtInEmitters))
	for name, newEmitter := range builtInEmitters {
		expectations = append(expectations,
			expectation{name: "the " + name + " emitter generates every query", holds: emitterGenerates(newEmitter), reason: "", knownFailure: false, toolchain: false},
			expectation{name: "the " + name + " output type-checks", holds: emitterOutputTypeChecks(newEmitter), reason: "", knownFailure: false, toolchain: true},
		)
	}
	return expectations
}

func TestMartianThroughThePipeline(t *testing.T) {
	t.Parallel()

	for _, current := range martianExpectations {
		t.Run(current.name, func(t *testing.T) {
			t.Parallel()
			if current.toolchain && testing.Short() {
				t.Skip("type-checking loads the standard library's export data through the go command; skipped under -short")
			}

			holds, detail := current.holds(t)
			if current.knownFailure {
				require.False(t, holds, "a known failure now holds; set knownFailure to false and drop its reason: %s", current.reason)
				t.Logf("known failure: %s (observed: %s)", current.reason, detail)
				return
			}
			require.True(t, holds, "the Martian dialect expectation failed: %s", detail)
		})
	}
}

func TestTypeCheckReportsRealErrorsOnly(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("type-checking loads the standard library's export data through the go command; skipped under -short")
	}

	testCases := []struct {
		name           string
		source         string
		expectProblems bool
	}{
		{
			name:           "a stubbed third-party selector is not a problem",
			source:         "package store\n\nimport \"github.com/example/thing/v2\"\n\nvar Value thing.Kind\n",
			expectProblems: false,
		},
		{
			name:           "a standard-library misuse is a problem",
			source:         "package store\n\nimport \"strings\"\n\nvar Value int = strings.ToUpper(\"x\")\n",
			expectProblems: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problems := typeCheck(t, []sqlmodel.GeneratedFile{
				{Name: "value.go", Content: []byte(testCase.source)},
				{Name: "schema.sql", Content: []byte("not go")},
			})
			require.Equal(t, testCase.expectProblems, len(problems) > 0, "problems: %v", problems)
		})
	}
}

func TestMartianKnownFailuresStayDocumented(t *testing.T) {
	t.Parallel()

	for _, current := range martianExpectations {
		require.Equal(t, current.knownFailure, current.reason != "", "%s: a known failure carries a reason and a passing expectation carries none", current.name)
	}
}

func predicatesOverNullableColumnsAreNullable(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, "-- sgl.query(name: Flags, command: many)\nSELECT quantity > 1 AS big, active = 1 AS on_sale, quantity IS NULL AS unknown FROM items;\n")
	query := generated.query(t, "Flags")
	nullability := make([]bool, len(query.OutputColumns))
	for index := range query.OutputColumns {
		nullability[index] = query.OutputColumns[index].Nullable
	}
	return slices.Equal(nullability, []bool{true, false, false}), fmt.Sprintf("nullability %v; diagnostics %s", nullability,
		diagnosticSummary(generated.result.Diagnostics))
}

func ungroupedColumnsAreNullable(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, "-- sgl.query(name: Tally, command: one)\nSELECT name, COUNT(*) AS total FROM items;\n")
	query := generated.query(t, "Tally")
	nullability := make([]bool, len(query.OutputColumns))
	for index := range query.OutputColumns {
		nullability[index] = query.OutputColumns[index].Nullable
	}
	return slices.Equal(nullability, []bool{true, false}) && len(generated.result.Diagnostics) == 0,
		fmt.Sprintf("nullability %v; diagnostics %s", nullability, diagnosticSummary(generated.result.Diagnostics))
}

func scalarCallsInFromAreOneColumn(t *testing.T) (bool, string) {
	t.Helper()

	generated := probe(t, "-- sgl.query(name: Shouted, command: one)\nSELECT * FROM UPPER('a');\n"+
		"-- sgl.query(name: ShoutedAs, command: one)\nSELECT S FROM UPPER('b') AS S;\n")
	var columns []string
	for _, name := range []string{"Shouted", "ShoutedAs"} {
		for _, column := range generated.query(t, name).OutputColumns {
			columns = append(columns, column.Name+" "+column.SQLType.EngineName)
		}
	}
	return slices.Equal(columns, []string{"UPPER TEXTUAL", "S TEXTUAL"}) && len(generated.result.Diagnostics) == 0,
		fmt.Sprintf("columns %v; diagnostics %s", columns, diagnosticSummary(generated.result.Diagnostics))
}
