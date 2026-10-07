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
	"errors"
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// quotedTableName is the table name QuotedMigration must create.
	quotedTableName = "weird name"

	// beginAliasStatements is two statements whose first aliases a column as begin, which a
	// splitter must not mistake for the start of a procedural block.
	beginAliasStatements = "SELECT 1 AS begin; SELECT 2;"

	// byteOrderMark is the UTF-8 byte order mark some editors write at the start of a file.
	byteOrderMark = "\uFEFF"

	// generationFailedFormat reports a generation that should have succeeded.
	generationFailedFormat = "generation should succeed: %v"

	// generationFatalFormat reports a generation that failed fatally instead of reporting a
	// diagnostic.
	generationFatalFormat = "generation itself should not fail fatally: %v"

	// undiagnosedFormat reports a statement that drew diagnostics where it must analyse
	// cleanly, given the statement and its diagnostics.
	undiagnosedFormat = "%q must analyse without diagnostics, got %s"

	// trailingTokens is appended to a complete query to make its tail unparseable.
	trailingTokens = " ) )"
)

var (
	// errConformanceCancelled is the cause the cancellation check cancels its context with.
	errConformanceCancelled = errors.New("the conformance check cancelled the context")
)

// checkStatementSplitting checks that the two-statement migration yields exactly the
// tables alpha and beta.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkStatementSplitting(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	emitter, _, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_split.up.sql": fixture.SplitterMigration}, nil)
	if err != nil {
		return failed(generationFailedFormat, err)
	}
	names := catalogueTableNames(emitter.catalogue)
	_, hasAlpha := names["alpha"]
	_, hasBeta := names["beta"]
	if !hasAlpha || !hasBeta || len(names) != 2 {
		return failed("the two-statement migration must split into exactly the tables alpha and beta, got %v", names)
	}
	return passed
}

// checkBeginAlias checks that a column aliased begin does not swallow the next statement.
//
// Takes fixture (*Fixture) which supplies the dialect.
//
// Returns checkOutcome which holds any problem found.
func checkBeginAlias(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	statements, err := fixture.NewDialect().ParseStatements(ctx, beginAliasStatements)
	if err != nil {
		return failed("parsing two statements with a begin alias should not error: %v", err)
	}
	if len(statements) != 2 {
		return failed("%q must split into exactly two statements, got %d; a swallowed trailing statement means "+
			"the splitter mistook the begin alias for a procedural block opener", beginAliasStatements, len(statements))
	}
	return passed
}

// checkParameterTracking checks that the one-parameter query reports one parameter
// numbered 1.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkParameterTracking(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	emitter, _, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{"get_item.sql": fixture.ParamQuery})
	if err != nil {
		return failed(generationFailedFormat, err)
	}
	query := findQuery(emitter.queries, "GetItem")
	if query == nil {
		return failed("the GetItem query should be analysed")
	}
	if len(query.Parameters) != 1 {
		return failed("exactly one parameter expected, got %d", len(query.Parameters))
	}
	if query.Parameters[0].Number != 1 {
		return failed("parameter numbering starts at 1, got %d", query.Parameters[0].Number)
	}
	return passed
}

// checkQuotedIdentifier checks that a delimited table name reaches the catalogue.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkQuotedIdentifier(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	emitter, _, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_quoted.up.sql": fixture.QuotedMigration}, nil)
	if err != nil {
		return failed(generationFailedFormat, err)
	}
	if _, found := catalogueTableNames(emitter.catalogue)[quotedTableName]; !found {
		return failed("the delimited table name %q should appear in the catalogue", quotedTableName)
	}
	return passed
}

// checkCommentStripping checks that comments around DDL do not hide it.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkCommentStripping(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	migration := "/* leading block comment */\n" +
		"-- a line comment\n" +
		fixture.BaseMigration + "\n" +
		"-- trailing comment\n"
	emitter, _, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_items.up.sql": migration}, nil)
	if err != nil {
		return failed("generation should succeed despite surrounding comments: %v", err)
	}
	if _, found := catalogueTableNames(emitter.catalogue)["items"]; !found {
		return failed("the table defined amid comments should appear in the catalogue")
	}
	return passed
}

// checkUnterminatedBlockComment checks that an unterminated block comment produces a
// parse-error diagnostic, matched by its code rather than its wording.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkUnterminatedBlockComment(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	query := directiveLine(engine, "Broken", "one") + "SELECT id FROM items /* unterminated"
	_, result, err := runGenerate(ctx, engine,
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{"broken.sql": query})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if !hasDiagnostic(result, "broken.sql", diag.CodeParseError) {
		return failed("an unterminated block comment must produce a %s diagnostic, got %s",
			diag.CodeParseError, formatDiagnostics(result.Diagnostics))
	}
	return passed
}

// checkInfoIsValid checks that Info describes the dialect completely: a name, a family, a
// line comment prefix with no whitespace, a non-empty expression set, the placeholder
// forms, the identifier quote, and the SQL the generated code writes itself, which
// generatedSyntaxProblems checks.
//
// Takes fixture (*Fixture) which supplies the dialect.
//
// Returns checkOutcome which holds every problem found.
func checkInfoIsValid(_ context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	info := fixture.NewDialect().Info()
	var problems []string
	if info.Name == "" {
		problems = append(problems, "Info.Name must name the dialect")
	}
	if info.Family == "" {
		problems = append(problems, "Info.Family must name the dialect's family")
	}
	prefix := info.Comments.LinePrefix
	if prefix == "" || strings.ContainsAny(prefix, " \t\r\n") {
		problems = append(problems, "Info.Comments.LinePrefix must be a line comment prefix without whitespace, got "+
			"\""+prefix+"\"; an empty prefix makes every line a directive comment")
	}
	if info.Expressions == 0 {
		problems = append(problems, "Info.Expressions must list the expression kinds the dialect supports")
	}
	if info.Placeholders.Execution.Prefix == "" {
		problems = append(problems, "Info.Placeholders.Execution must give the form the generated code writes each parameter in")
	}
	if info.Placeholders.BindsByName() && info.Placeholders.PositionalForm().Kind == dialect.PlaceholderNamed {
		problems = append(problems, "Info.Placeholders.Positional must give a numbered or anonymous form for a dialect that executes named placeholders")
	}
	if info.IdentifierQuote.Open == "" || info.IdentifierQuote.Close == "" {
		problems = append(problems, "Info.IdentifierQuote must give the text that delimits an identifier")
	}
	if info.SelectAliasOperands&^info.SelectAliases != 0 {
		problems = append(problems, "Info.SelectAliasOperands may list only clauses Info.SelectAliases lists, got "+
			info.SelectAliasOperands.String()+" against "+info.SelectAliases.String())
	}
	if info.UngroupedColumns > dialect.UngroupedColumnsRejected {
		problems = append(problems, fmt.Sprintf("Info.UngroupedColumns must be one of the declared rules, got %d", info.UngroupedColumns))
	}
	problems = append(problems, generatedSyntaxProblems(&info)...)
	return outcomeOf(problems)
}

// checkCancelledContext checks that ParseStatements and AnalyseQuery honour a cancelled
// context, returning its error rather than doing the work.
//
// Takes fixture (*Fixture) which supplies the dialect and its query.
//
// Returns checkOutcome which holds any problem found.
func checkCancelledContext(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.ParamQuery == "" {
		return notApplicable("the fixture sets no ParamQuery")
	}
	engine := fixture.NewDialect()
	statements, err := engine.ParseStatements(ctx, fixture.ParamQuery)
	if err != nil || len(statements) == 0 {
		return failed("parsing the ParamQuery should give a statement, got %d and %v", len(statements), err)
	}
	cancelled, cancel := context.WithCancelCause(ctx)
	cancel(errConformanceCancelled)

	var problems []string
	if _, parseErr := engine.ParseStatements(cancelled, fixture.ParamQuery); !stoppedByCancellation(parseErr) {
		problems = append(problems, fmt.Sprintf("ParseStatements with a cancelled context must return its cause or error, got %v", parseErr))
	}
	if _, analyseErr := engine.AnalyseQuery(cancelled, emptyCatalogue(), statements[len(statements)-1]); !stoppedByCancellation(analyseErr) {
		problems = append(problems, fmt.Sprintf("AnalyseQuery with a cancelled context must return its cause or error, got %v", analyseErr))
	}
	return outcomeOf(problems)
}

// checkPlaceholders checks that every distinct placeholder in the placeholder query
// becomes a parameter.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkPlaceholders(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.PlaceholderQuery == "" {
		return notApplicable("the fixture sets no PlaceholderQuery")
	}
	emitter, result, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{"placeholders.sql": fixture.PlaceholderQuery})
	if err != nil {
		return failed(generationFailedFormat, err)
	}
	query := findQuery(emitter.queries, "CountPlaceholders")
	if query == nil {
		return failed("the CountPlaceholders query should be analysed, got diagnostics %s", formatDiagnostics(result.Diagnostics))
	}
	if len(query.Parameters) != fixture.PlaceholderCount {
		return failed("the query holds %d distinct placeholders but reports %d parameters; a dropped placeholder "+
			"generates a method that binds too few arguments", fixture.PlaceholderCount, len(query.Parameters))
	}
	return passed
}

// checkTrailingTokens checks that tokens left over after a complete statement produce an
// error diagnostic instead of being dropped.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkTrailingTokens(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	query := strings.TrimSuffix(strings.TrimSpace(fixture.ParamQuery), ";") + trailingTokens + ";"
	_, result, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{"trailing.sql": query})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if !hasErrorDiagnostic(result, "trailing.sql") {
		return failed("the tokens %q after a complete statement must produce an error diagnostic, got %s",
			trailingTokens, formatDiagnostics(result.Diagnostics))
	}
	return passed
}

// checkByteOrderMarkMigration checks that a migration starting with a byte order mark
// reads like any other.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkByteOrderMarkMigration(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	emitter, result, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_items.up.sql": byteOrderMark + fixture.BaseMigration}, nil)
	if err != nil {
		return failed(generationFailedFormat, err)
	}
	if hasErrorDiagnostic(result, "") {
		return failed("a migration starting with a byte order mark must read cleanly, got %s", formatDiagnostics(result.Diagnostics))
	}
	if _, found := catalogueTableNames(emitter.catalogue)["items"]; !found {
		return failed("the table created after a byte order mark should appear in the catalogue")
	}
	return passed
}

// checkByteOrderMarkQuery checks that a query file starting with a byte order mark reads
// like any other.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds any problem found.
func checkByteOrderMarkQuery(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	emitter, result, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{"get_item.sql": byteOrderMark + fixture.ParamQuery})
	if err != nil {
		return failed(generationFailedFormat, err)
	}
	if hasErrorDiagnostic(result, "") {
		return failed("a query file starting with a byte order mark must read cleanly, got %s", formatDiagnostics(result.Diagnostics))
	}
	if findQuery(emitter.queries, "GetItem") == nil {
		return failed("the GetItem query after a byte order mark should be analysed")
	}
	return passed
}

// hasDiagnostic reports whether the run produced a diagnostic with the given code in a
// file whose name ends with filename.
//
// Takes result (*app.Result) which holds the run's diagnostics.
// Takes filename (string) which is the file name suffix to match.
// Takes code (string) which is the diagnostic code.
//
// Returns bool which is true when such a diagnostic exists.
func hasDiagnostic(result *app.Result, filename, code string) bool {
	return slices.ContainsFunc(result.Diagnostics, func(diagnostic diag.SourceError) bool {
		return diagnostic.Code == code && strings.HasSuffix(diagnostic.Filename, filename)
	})
}

// hasErrorDiagnostic reports whether the run produced an error diagnostic in a file whose
// name ends with filename; an empty filename matches every file.
//
// Takes result (*app.Result) which holds the run's diagnostics.
// Takes filename (string) which is the file name suffix to match.
//
// Returns bool which is true when such a diagnostic exists.
func hasErrorDiagnostic(result *app.Result, filename string) bool {
	return slices.ContainsFunc(result.Diagnostics, func(diagnostic diag.SourceError) bool {
		return diagnostic.Severity == diag.SeverityError && strings.HasSuffix(diagnostic.Filename, filename)
	})
}

// quotesName reports whether a diagnostic's message quotes a name, in whatever case the
// dialect folded it to, so an engine that folds unquoted names to upper case is judged as
// one that folds them to lower case.
//
// Takes message (string) which is the diagnostic's message.
// Takes name (string) which is the name, written unquoted in the probe.
//
// Returns bool which is true when the message quotes the name.
func quotesName(message, name string) bool {
	return strings.Contains(strings.ToLower(message), `"`+strings.ToLower(name)+`"`)
}

// formatDiagnostics renders diagnostics compactly for a problem message.
//
// Takes diagnostics ([]diag.SourceError) which are the run's diagnostics.
//
// Returns string which lists each diagnostic's code, severity, position and message.
func formatDiagnostics(diagnostics []diag.SourceError) string {
	if len(diagnostics) == 0 {
		return "[]"
	}
	rendered := make([]string, 0, len(diagnostics))
	for index := range diagnostics {
		diagnostic := &diagnostics[index]
		rendered = append(rendered, fmt.Sprintf("%s %s %s:%d:%d %s", diagnostic.Code, diagnostic.Severity,
			diagnostic.Filename, diagnostic.Line, diagnostic.Column, diagnostic.Message))
	}
	return "[" + strings.Join(rendered, "; ") + "]"
}

// emptyCatalogue returns a catalogue with no schemas.
//
// Returns *sqlmodel.Catalogue which is the catalogue.
func emptyCatalogue() *sqlmodel.Catalogue {
	return &sqlmodel.Catalogue{
		Schemas:       map[string]*sqlmodel.Schema{},
		Extensions:    map[string]struct{}{},
		DefaultSchema: "",
		SearchPath:    nil,
		Identifiers:   sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: false},
	}
}

// stoppedByCancellation reports whether an error is the one a dialect returns when the
// conformance check cancels its context: the cancellation's cause, as context.Cause gives
// it, or the context's own error.
//
// Takes err (error) which the dialect returned.
//
// Returns bool which is true when the dialect stopped because of the cancellation.
func stoppedByCancellation(err error) bool {
	return errors.Is(err, errConformanceCancelled) || errors.Is(err, context.Canceled)
}
