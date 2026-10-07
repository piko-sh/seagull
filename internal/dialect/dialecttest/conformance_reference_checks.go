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

package dialecttest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// queryCommandMany is the command of a query reading rows.
	queryCommandMany = "many"

	// queryCommandExec is the command of a statement reading none.
	queryCommandExec = "exec"

	// reservedWordTable is the table ReservedWordMigration must create.
	reservedWordTable = "reserved_items"

	// typedTable is the table TypedMigration must create.
	typedTable = "typed_items"

	// extraTypeName is the type name the extra-types check registers.
	extraTypeName = "seagull_conformance_type"
)

// reservedWordColumns lists the delimited columns ReservedWordMigration declares after
// id.
var reservedWordColumns = []string{"check", "constraint", "index", "primary", "group", "where"}

// unknownReference is a query naming something the catalogue does not hold, and the
// diagnostic it must produce.
type unknownReference struct {
	// name is the query name.
	name string

	// command is the query command.
	command string

	// body is the query SQL after the directive line.
	body string

	// code is the diagnostic code the query must produce.
	code string
}

// checkReservedWordColumns checks that reserved words delimited as column names stay
// columns: the catalogue holds exactly the declared columns, and a query selecting and
// filtering on them keeps its output columns and its parameter.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkReservedWordColumns(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.ReservedWordMigration == "" || fixture.ReservedWordQuery == "" {
		return notApplicable("the fixture sets no ReservedWordMigration and ReservedWordQuery")
	}
	emitter, result, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_reserved.up.sql": fixture.ReservedWordMigration},
		map[string]string{"get_reserved.sql": fixture.ReservedWordQuery})
	if err != nil {
		return failed(generationFailedFormat, err)
	}
	expectedColumns := append([]string{"id"}, reservedWordColumns...)
	var problems []string
	if columns := catalogueColumnNames(emitter.catalogue, reservedWordTable); !slices.Equal(columns, expectedColumns) {
		problems = append(problems, "the catalogue must hold exactly the columns "+strings.Join(expectedColumns, ", ")+
			" of "+reservedWordTable+", got ["+strings.Join(columns, ", ")+"]")
	}
	query := findQuery(emitter.queries, "GetReserved")
	if query == nil {
		return outcomeOf(append(problems, "the GetReserved query should be analysed, got diagnostics "+
			formatDiagnostics(result.Diagnostics)))
	}
	if outputs := outputColumnNames(query); !slices.Equal(outputs, []string{"id", "group"}) {
		problems = append(problems, "GetReserved must output id and group, got ["+strings.Join(outputs, ", ")+"]")
	}
	if len(query.Parameters) != 1 {
		problems = append(problems, fmt.Sprintf("GetReserved filters on one parameter but reports %d; a delimited "+
			"reserved word read as a keyword drops the parameter", len(query.Parameters)))
	}
	return outcomeOf(problems)
}

// checkDeclaredColumns checks that every column TypedMigration declares reaches the
// catalogue, in order, and that nothing else does.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkDeclaredColumns(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.TypedMigration == "" {
		return notApplicable("the fixture sets no TypedMigration")
	}
	emitter, result, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_typed.up.sql": fixture.TypedMigration}, nil)
	if err != nil {
		return failed(generationFailedFormat, err)
	}
	columns := catalogueColumnNames(emitter.catalogue, typedTable)
	if !slices.Equal(columns, fixture.TypedColumns) {
		return failed("%s declares the columns [%s] but the catalogue holds [%s]; diagnostics %s",
			typedTable, strings.Join(fixture.TypedColumns, ", "), strings.Join(columns, ", "),
			formatDiagnostics(result.Diagnostics))
	}
	return passed
}

// checkNotInList checks that the placeholder in a NOT IN list takes its name and type
// from the column on the left, exactly as it does in an IN list, rather than from the NOT
// keyword.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkNotInList(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.NotInQuery == "" {
		return notApplicable("the fixture sets no NotInQuery")
	}
	emitter, result, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{"exclude_items.sql": fixture.NotInQuery})
	if err != nil {
		return failed(generationFailedFormat, err)
	}
	query := findQuery(emitter.queries, "ExcludeItems")
	if query == nil {
		return failed("the ExcludeItems query should be analysed, got diagnostics %s", formatDiagnostics(result.Diagnostics))
	}
	if len(query.Parameters) != 1 {
		return failed("ExcludeItems holds one placeholder but reports %d parameters", len(query.Parameters))
	}
	var problems []string
	parameter := query.Parameters[0]
	if !strings.EqualFold(parameter.Name, "id") {
		problems = append(problems, fmt.Sprintf("the NOT IN placeholder should be named after the column id, got %q", parameter.Name))
	}
	if parameter.SQLType.Category != sqlmodel.TypeCategoryInteger {
		problems = append(problems, fmt.Sprintf("the NOT IN placeholder should take the integer type of the column id, got %s",
			parameter.SQLType.Category))
	}
	if hasDiagnostic(result, "exclude_items.sql", diag.CodeUnknownColumn) {
		problems = append(problems, "the NOT keyword was read as a column: "+formatDiagnostics(result.Diagnostics))
	}
	return outcomeOf(problems)
}

// checkUnknownColumnInWhere checks that an unknown column in a WHERE clause compared with
// a literal is reported.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkUnknownColumnInWhere(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return checkUnknownReference(ctx, fixture, unknownReference{
		name:    "UnknownInWhere",
		command: queryCommandMany,
		body:    "SELECT id FROM items WHERE missing_column = 1;",
		code:    diag.CodeUnknownColumn,
	})
}

// checkUnknownColumnInOrderBy checks that an unknown column in an ORDER BY clause is
// reported.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkUnknownColumnInOrderBy(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return checkUnknownReference(ctx, fixture, unknownReference{
		name:    "UnknownInOrderBy",
		command: queryCommandMany,
		body:    "SELECT id FROM items ORDER BY missing_column;",
		code:    diag.CodeUnknownColumn,
	})
}

// checkUnknownColumnInSet checks that an unknown column assigned in an UPDATE is
// reported.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkUnknownColumnInSet(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return checkUnknownReference(ctx, fixture, unknownReference{
		name:    "UnknownInSet",
		command: queryCommandExec,
		body:    "UPDATE items SET missing_column = 1 WHERE id = 1;",
		code:    diag.CodeUnknownColumn,
	})
}

// checkUnknownNamesInNestedQueriesAreReported checks that a subquery naming a column or a
// table the catalogue does not hold, in its select list or its FROM clause, and a
// qualifier naming a CTE no FROM clause reads are reported as they are at the top level,
// as the engine refuses each.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkUnknownNamesInNestedQueriesAreReported(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	references := []unknownReference{
		{name: "UnknownColumnInSubquery", command: queryCommandMany, body: "SELECT id FROM items WHERE id IN (SELECT missing_column FROM items);",
			code: diag.CodeUnknownColumn},
		{name: "UnknownTableInSubquery", command: queryCommandMany, body: "SELECT id FROM items WHERE EXISTS (SELECT 1 FROM missing_table);",
			code: diag.CodeUnknownTable},
		{name: "UnknownColumnInScalarSubquery", command: queryCommandMany, body: "SELECT id, (SELECT missing_column FROM items LIMIT 1) AS other FROM items;",
			code: diag.CodeUnknownColumn},
		{name: "CTEOutsideFrom", command: queryCommandMany, body: "WITH found AS (SELECT id FROM items) SELECT id FROM items WHERE found.id = 1;",
			code: diag.CodeUnknownColumn},
	}
	problems := make([]string, 0, len(references))
	for _, reference := range references {
		problems = append(problems, checkUnknownReference(ctx, fixture, reference).Problems...)
	}
	return outcomeOf(problems)
}

// checkInsertShapesTheEngineRefusesAreReported checks that an INSERT whose rows give
// another number of values than it names target columns, whose SELECT gives another
// number of columns, or which names a target column the table lacks is reported, as the
// engine refuses each.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkInsertShapesTheEngineRefusesAreReported(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	references := []unknownReference{
		{name: "TooFewValues", command: queryCommandExec, body: "INSERT INTO items (id, name) VALUES (1);", code: diag.CodeInsertShape},
		{name: "TooManyValues", command: queryCommandExec, body: "INSERT INTO items (id) VALUES (1, 'x');", code: diag.CodeInsertShape},
		{name: "SecondRowShort", command: queryCommandExec, body: "INSERT INTO items (id, name) VALUES (1, 'x'), (2);", code: diag.CodeInsertShape},
		{name: "SelectTooNarrow", command: queryCommandExec, body: "INSERT INTO items (id, name) SELECT id FROM items;", code: diag.CodeInsertShape},
		{name: "UnknownTargetColumn", command: queryCommandExec, body: "INSERT INTO items (id, missing_column) VALUES (1, 'x');",
			code: diag.CodeUnknownColumn},
	}
	problems := make([]string, 0, len(references))
	for _, reference := range references {
		problems = append(problems, checkUnknownReference(ctx, fixture, reference).Problems...)
	}
	return outcomeOf(problems)
}

// checkUnknownFunctionInWhere checks that an unknown function in a WHERE clause is
// reported.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkUnknownFunctionInWhere(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return checkUnknownReference(ctx, fixture, unknownReference{
		name:    "UnknownFunctionInWhere",
		command: queryCommandMany,
		body:    "SELECT id FROM items WHERE missing_function(id) = 1;",
		code:    diag.CodeUnknownFunction,
	})
}

// checkUnknownReference checks that a query naming something unknown produces the
// expected diagnostic.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
// Takes reference (unknownReference) which is the query and its expected diagnostic.
//
// Returns checkOutcome which holds any problem found.
func checkUnknownReference(ctx context.Context, fixture *Fixture, reference unknownReference) checkOutcome {
	engine := fixture.NewDialect()
	filename := strings.ToLower(reference.name) + ".sql"
	_, result, err := runGenerate(ctx, engine,
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{filename: directiveLine(engine, reference.name, reference.command) + reference.body})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if !hasDiagnostic(result, filename, reference.code) {
		return failed("%q must produce a %s diagnostic, got %s", reference.body, reference.code,
			formatDiagnostics(result.Diagnostics))
	}
	return passed
}

// checkExtraTypesResolve checks that a type registered through the dialect's extra-types
// option resolves through its type normaliser.
//
// Takes fixture (*Fixture) which supplies the dialect.
//
// Returns checkOutcome which holds any problem found.
func checkExtraTypesResolve(_ context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.NewDialectWithExtraType == nil {
		return notApplicable("the fixture sets no NewDialectWithExtraType")
	}
	registered := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, extraTypeName)
	engine := fixture.NewDialectWithExtraType(extraTypeName, registered)
	resolved := engine.NormaliseTypeName(extraTypeName)
	if resolved.Category != registered.Category {
		return failed("the extra type %q must resolve to the %s category, got %s", extraTypeName,
			registered.Category, resolved.Category)
	}
	return passed
}

// checkFlavourOptionsAreAdditive checks that a flavour built with the caller's options
// keeps both the caller's function and the flavour's own functions and types.
//
// Takes fixture (*Fixture) which supplies the flavour.
//
// Returns checkOutcome which holds every problem found.
func checkFlavourOptionsAreAdditive(_ context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Flavour == nil {
		return notApplicable("the dialect is not a flavour of a base dialect")
	}
	engine := fixture.Flavour.New()
	functions := engine.BuiltinFunctions()
	var problems []string
	for _, name := range append([]string{fixture.Flavour.UserFunction}, fixture.Flavour.FlavourFunctions...) {
		if functions == nil || len(functions.Functions[name]) == 0 {
			problems = append(problems, "the function "+name+" is missing once the caller's options apply; "+
				"options must add to the flavour's settings, not replace them")
		}
	}
	for _, name := range fixture.Flavour.FlavourTypes {
		if engine.NormaliseTypeName(name).Category == sqlmodel.TypeCategoryUnknown {
			problems = append(problems, "the flavour type "+name+" no longer resolves once the caller's options apply")
		}
	}
	return outcomeOf(problems)
}

// checkFlavourWithholdsWhatItsEngineLacks checks that a flavour registers none of the
// base dialect's functions its engine lacks, with or without the caller's options, and
// that a query calling one is reported as calling an unknown function, so neither the
// catalogue nor the dialect's own function resolver accepts the call.
//
// Takes fixture (*Fixture) which supplies the flavour.
//
// Returns checkOutcome which holds every problem found.
func checkFlavourWithholdsWhatItsEngineLacks(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Flavour == nil || len(fixture.Flavour.WithheldFunctions) == 0 {
		return notApplicable("the dialect is not a flavour withholding functions of its base dialect")
	}
	var problems []string
	for _, engine := range []dialect.DialectPort{fixture.NewDialect(), fixture.Flavour.New()} {
		functions := engine.BuiltinFunctions()
		for _, name := range fixture.Flavour.WithheldFunctions {
			if functions != nil && len(functions.Functions[name]) > 0 {
				problems = append(problems, "the flavour registers "+name+", which its engine lacks")
			}
		}
	}
	engine := fixture.NewDialect()
	queries := make(map[string]string, len(fixture.Flavour.WithheldFunctions))
	for _, name := range fixture.Flavour.WithheldFunctions {
		queries["withheld_"+name+".sql"] = directiveLine(engine, "Call_"+name, "many") +
			"SELECT " + name + "(id) AS withheld FROM items;"
	}
	_, result, err := runGenerate(ctx, engine, map[string]string{"001_items.up.sql": fixture.BaseMigration}, queries)
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	for _, name := range fixture.Flavour.WithheldFunctions {
		if !hasDiagnostic(result, "withheld_"+name+".sql", diag.CodeUnknownFunction) {
			problems = append(problems, fmt.Sprintf("a call of %s, which the engine lacks, must produce a %s diagnostic, got %s",
				name, diag.CodeUnknownFunction, formatDiagnostics(result.Diagnostics)))
		}
	}
	return outcomeOf(problems)
}

// checkStructuralAttributes checks built-in and core types for required structural
// attributes.
//
// Takes fixture (*Fixture) which supplies the dialect.
//
// Returns checkOutcome which lists every type without its attributes.
func checkStructuralAttributes(_ context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	types := engine.BuiltinTypes().Types
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(types)) {
		if !types[name].HasStructuralAttributes() {
			problems = append(problems, fmt.Sprintf("the built-in %s type %q has no structural attributes", types[name].Category, name))
			continue
		}
		if normalised := engine.NormaliseTypeName(name); !normalised.HasStructuralAttributes() {
			problems = append(problems, fmt.Sprintf("NormaliseTypeName(%q) drops the structural attributes of the %s type", name, normalised.Category))
		}
	}
	return outcomeOf(append(problems, coreTypeProblems(new(engine.CoreTypes()))...))
}

// coreTypeProblems describes each core type that does not suit its role.
//
// Takes core (*dialect.CoreTypes) which holds the dialect's core types.
//
// Returns []string which holds the problems, empty when the core types suit their roles.
func coreTypeProblems(core *dialect.CoreTypes) []string {
	var problems []string
	if core.Boolean.Category != sqlmodel.TypeCategoryBoolean {
		problems = append(problems, fmt.Sprintf("the core boolean type is %s, not boolean", core.Boolean.Category))
	}
	if core.Text.Category != sqlmodel.TypeCategoryText {
		problems = append(problems, fmt.Sprintf("the core text type is %s, not text", core.Text.Category))
	}
	if !isSizedInteger(&core.Count) {
		problems = append(problems, "the core count type is not an integer of a known width")
	}
	if !isSizedInteger(&core.RowCount) {
		problems = append(problems, "the core row count type is not an integer of a known width")
	}
	return problems
}

// isSizedInteger reports whether a type is an integer of a known width.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
//
// Returns bool which is true for an integer that carries its width.
func isSizedInteger(sqlType *sqlmodel.SQLType) bool {
	return sqlType.Category == sqlmodel.TypeCategoryInteger && sqlType.HasStructuralAttributes()
}

// catalogueColumnNames returns the lower-cased column names of a table, in order, looked
// up case-insensitively across every schema.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema state.
// Takes table (string) which is the table name.
//
// Returns []string which holds the column names, or nil when the table is absent.
func catalogueColumnNames(catalogue *sqlmodel.Catalogue, table string) []string {
	if catalogue == nil {
		return nil
	}
	for _, schema := range catalogue.Schemas {
		if schema == nil {
			continue
		}
		for name, definition := range schema.Tables {
			if !strings.EqualFold(name, table) || definition == nil {
				continue
			}
			names := make([]string, 0, len(definition.Columns))
			for index := range definition.Columns {
				names = append(names, strings.ToLower(definition.Columns[index].Name))
			}
			return names
		}
	}
	return nil
}

// outputColumnNames returns the lower-cased output column names of a query.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the analysed query.
//
// Returns []string which holds the names in order.
func outputColumnNames(query *sqlmodel.AnalysedQuery) []string {
	names := make([]string, 0, len(query.OutputColumns))
	for index := range query.OutputColumns {
		names = append(names, strings.ToLower(query.OutputColumns[index].Name))
	}
	return names
}
