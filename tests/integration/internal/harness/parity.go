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

package harness

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"path/filepath"
	"testing/fstest"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/emit"
)

const (
	// parityQueryPrefix starts the name of each statement's query block.
	parityQueryPrefix = "Parity"
)

// parityStatement is one statement whose acceptance the engine and Seagull must agree on.
type parityStatement struct {
	// Label identifies the statement in a report.
	Label string

	// SQL is the statement, without a trailing semicolon.
	SQL string
}

// parityMismatch is a statement the engine and Seagull disagree on.
type parityMismatch struct {
	// Engine is the engine's refusal, or nil when it accepts the statement.
	Engine error

	// Label identifies the statement.
	Label string

	// SQL is the statement.
	SQL string

	// Seagull is Seagull's refusal, or empty when it accepts the statement.
	Seagull string
}

// parityReport holds the statements the engine and Seagull disagree on.
type parityReport struct {
	// Missed holds the statements Seagull accepts without a warning and the engine refuses,
	// whose generated code fails when it runs.
	Missed []parityMismatch

	// Warned holds the statements Seagull accepts with a warning and the engine refuses.
	Warned []parityMismatch

	// Refused holds the statements Seagull refuses and the engine accepts.
	Refused []parityMismatch

	// Statements counts the statements checked.
	Statements int
}

// engineAccepts asks the engine whether it accepts a statement, without running it. A
// ClickHouse server plans it, and every other engine prepares it.
//
// Takes database (*sql.DB) which is connected to the engine.
// Takes engine (string) which names the engine.
// Takes statement (string) which is the statement.
// Takes arguments (...any) which bind a ClickHouse statement's parameters, which its
// server substitutes before it plans. Other engines prepare without them.
//
// Returns error which is the engine's refusal, or nil when it accepts the statement.
func engineAccepts(ctx context.Context, database *sql.DB, engine, statement string, arguments ...any) error {
	return engineChecks(ctx, database, engine, "EXPLAIN PLAN ", statement, arguments...)
}

// engineAcceptsDDL asks the engine whether it accepts a definition without running it. A
// ClickHouse server reads its syntax, and every other engine prepares it.
//
// Takes database (*sql.DB) which is connected to the engine.
// Takes engine (string) which names the engine.
// Takes statement (string) which is the definition.
//
// Returns error which is the engine's refusal, or nil when it accepts the definition.
func engineAcceptsDDL(ctx context.Context, database *sql.DB, engine, statement string) error {
	return engineChecks(ctx, database, engine, "EXPLAIN AST ", statement)
}

// engineChecks prepares a statement, or on ClickHouse, which prepares nothing, explains
// it.
//
// Takes database (*sql.DB) which is connected to the engine.
// Takes engine (string) which names the engine.
// Takes explain (string) which is the ClickHouse EXPLAIN form to use.
// Takes statement (string) which is the statement.
// Takes arguments (...any) which bind a ClickHouse statement's parameters.
//
// Returns error which is the engine's refusal, or nil when it accepts the statement.
func engineChecks(ctx context.Context, database *sql.DB, engine, explain, statement string, arguments ...any) error {
	ctx, cancel := context.WithTimeoutCause(ctx, scaled(databaseStatementDeadline), errFuzzStatementDeadline)
	defer cancel()
	if engine == engineClickHouse {
		rows, err := database.QueryContext(ctx, explain+statement, arguments...)
		if err != nil {
			return err
		}
		return rows.Close()
	}
	prepared, err := database.PrepareContext(ctx, statement)
	if err != nil {
		return err
	}
	return prepared.Close()
}

// checkParity asks the engine and Seagull whether each statement is accepted, over the
// schema the migrations build, and reports where they disagree.
//
// Takes suite (FuzzSuite) which supplies the dialect and a database.
// Takes database (*sql.DB) which holds the migrated schema.
// Takes migrations (fstest.MapFS) which holds the migrations, under migrations/.
// Takes statements ([]parityStatement) which are the statements.
//
// Returns parityReport which holds the disagreements.
// Returns error when Seagull cannot run at all.
func checkParity(ctx context.Context, suite FuzzSuite, database *sql.DB, migrations fstest.MapFS, statements []parityStatement) (parityReport, error) {
	refusals, warnings, err := seagullRefusals(ctx, suite, migrations, statements)
	if err != nil {
		return parityReport{Missed: nil, Warned: nil, Refused: nil, Statements: 0}, err
	}
	report := parityReport{Missed: nil, Warned: nil, Refused: nil, Statements: len(statements)}
	for index, statement := range statements {
		engineErr := engineAccepts(ctx, database, suite.Engine, statement.SQL)
		refusal := refusals[index]
		mismatch := parityMismatch{Engine: engineErr, Label: statement.Label, SQL: statement.SQL, Seagull: refusal}
		switch {
		case engineErr != nil && refusal == "" && warnings[index] != "":
			mismatch.Seagull = warnings[index]
			report.Warned = append(report.Warned, mismatch)
		case engineErr != nil && refusal == "":
			report.Missed = append(report.Missed, mismatch)
		case engineErr == nil && refusal != "":
			report.Refused = append(report.Refused, mismatch)
		}
	}
	return report, nil
}

// seagullRefusals generates every statement as a query of a file of its own, so a
// statement that runs into the next cannot hide it, and reads which ones Seagull refuses.
//
// Takes suite (FuzzSuite) which supplies the dialect.
// Takes migrations (fstest.MapFS) which holds the migrations.
// Takes statements ([]parityStatement) which are the statements.
//
// Returns []string which holds, parallel to the statements, each refusal's message, or
// empty for a statement Seagull accepts.
// Returns []string which holds, parallel to the statements, each statement's first
// warning, or empty.
// Returns error when Seagull cannot run at all.
func seagullRefusals(ctx context.Context, suite FuzzSuite, migrations fstest.MapFS, statements []parityStatement) (refusals, warnings []string, err error) {
	files := fstest.MapFS{}
	maps.Copy(files, migrations)
	for index, statement := range statements {
		files[parityQueryPath(index)] = &fstest.MapFile{Data: fmt.Appendf(nil, "-- sgl.query(name: %s%d, command: many)\n%s;\n", parityQueryPrefix, index, statement.SQL)}
	}
	generator, err := seagull.New(suite.Dialect(), seagull.WithSkipFailingQueries())
	if err != nil {
		return nil, nil, err
	}
	details, err := generator.GenerateDetailed(ctx, seagull.Input{Package: "parity", PackagePath: "", Queries: nil, Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	if err != nil {
		return nil, nil, err
	}
	if details.Report == nil {
		return nil, nil, fmt.Errorf("seagull produced no report for %d statements", len(statements))
	}
	refusals = refusalsByStatement(details.Report.Queries, details.Report.Diagnostics, len(statements))
	return refusals, diagnosticsByStatement(details.Report.Diagnostics, seagull.SeverityWarning, len(statements)), nil
}

// parityQueryPath names the query file of one statement.
//
// Takes index (int) which is the statement's index.
//
// Returns string which is the file's path.
func parityQueryPath(index int) string {
	return fmt.Sprintf("queries/parity_%05d.sql", index)
}

// refusalsByStatement reads, for each statement, the first error Seagull reported in its
// file, and refuses a statement whose query Seagull left out without one.
//
// Takes queries ([]*emit.AnalysedQuery) which are the queries Seagull generated.
// Takes diagnostics ([]seagull.SourceError) which are the run's diagnostics.
// Takes count (int) which is the number of statements.
//
// Returns []string which holds each statement's refusal, or empty when it is accepted.
func refusalsByStatement(queries []*emit.AnalysedQuery, diagnostics []seagull.SourceError, count int) []string {
	refusals := make([]string, count)
	generated := make(map[int]bool, len(queries))
	for _, query := range queries {
		var index int
		if _, err := fmt.Sscanf(query.Name, parityQueryPrefix+"%d", &index); err == nil && index >= 0 && index < count {
			generated[index] = true
		}
	}
	for index, message := range diagnosticsByStatement(diagnostics, seagull.SeverityError, count) {
		refusals[index] = message
	}
	for index := range refusals {
		if refusals[index] == "" && !generated[index] {
			refusals[index] = "the query was left out of the generated package"
		}
	}
	return refusals
}

// diagnosticsByStatement reads, for each statement, the first diagnostic of a severity
// Seagull reported in its file.
//
// Takes diagnostics ([]seagull.SourceError) which are the run's diagnostics.
// Takes severity (seagull.ErrorSeverity) which selects the diagnostics.
// Takes count (int) which is the number of statements.
//
// Returns []string which holds each statement's first such message, or empty.
func diagnosticsByStatement(diagnostics []seagull.SourceError, severity seagull.ErrorSeverity, count int) []string {
	messages := make([]string, count)
	for _, diagnostic := range diagnostics {
		var index int
		if diagnostic.Severity != severity {
			continue
		}
		if _, err := fmt.Sscanf(filepath.Base(diagnostic.Filename), "parity_%05d.sql", &index); err != nil || index < 0 || index >= count {
			continue
		}
		if messages[index] == "" {
			messages[index] = diagnostic.Message
		}
	}
	return messages
}
