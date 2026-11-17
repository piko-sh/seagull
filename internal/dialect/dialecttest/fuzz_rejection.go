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
	"path"
	"strings"
	"testing"
	"testing/fstest"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// checkStructuredRejection requires a deliberate failure to reach its diagnostic oracle.
//
// Takes t (*testing.T) which reports missing or unexpected errors.
// Takes newDialect (func() dialect.DialectPort) which supplies a fresh engine.
// Takes rejected (fuzzcase.Rejection) which records the independently expected error.
func checkStructuredRejection(t *testing.T, newDialect func() dialect.DialectPort, rejected fuzzcase.Rejection) {
	t.Helper()
	ctx, cancel := fuzzContext(t)
	defer cancel()
	engine := newDialect()
	model, ok := fuzzcase.Decode(engine.Info().Name, []byte{fuzzcase.Version})
	if !ok {
		t.Fatal("rejection fixture has no supported schema")
	}
	migration := strings.Join(model.Migrations(), "\n")
	sources := map[string]string{
		path.Join(migrationDirectory, fuzzMigrationFile): migration,
		path.Join(queryDirectory, "rejected.sql"):        rejected.SQL,
	}
	files := make(fstest.MapFS)
	for name, source := range sources {
		files[name] = &fstest.MapFile{Data: []byte(source)}
	}
	generator, err := app.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	details, err := generator.GenerateDetailed(ctx, fuzzInput(files))
	if cause := context.Cause(ctx); err != nil || cause != nil {
		t.Fatalf("structured rejection failed outside diagnostics: %v, %v", err, cause)
	}
	for _, problem := range rejectionResultProblems(details, rejected.Code) {
		t.Errorf("%s\n%s", problem, rejected.SQL)
	}
	if details == nil || details.Result == nil {
		return
	}
	for _, problem := range diagnosticLocationProblems(details.Result.Diagnostics, sources) {
		t.Error(problem)
	}
	checkFuzzBundleResult(t, engine, details)
}

// rejectionResultProblems rejects missing, weakened or internally recovered errors.
//
// Takes details (*app.DetailedResult) which holds the generation outcome.
// Takes code (string) which identifies the expected error category.
//
// Returns []string which describes a failure to reject the model correctly.
func rejectionResultProblems(details *app.DetailedResult, code string) []string {
	if details == nil || details.Result == nil || details.Report == nil {
		return []string{"rejection model returned no detailed result"}
	}
	var problems []string
	found := false
	for _, diagnostic := range details.Result.Diagnostics {
		if diagnostic.Code == diag.CodeRecoveredPanic {
			problems = append(problems, "rejection model recovered an internal panic")
		}
		if diagnostic.Code == code && diagnostic.Severity == diag.SeverityError {
			found = true
		}
	}
	if !found || details.Result.Err() == nil {
		problems = append(problems, "rejection model omitted error "+code)
	}
	return problems
}

// countDiagnosticProblems checks count hints independently of the numerical oracle.
//
// Takes model (fuzzcase.Model) which records row-shaping operations.
// Takes diagnostics ([]diag.SourceError) which contains pipeline findings.
//
// Returns []string which describes missing, incorrect or unavailable count hints.
func countDiagnosticProblems(model fuzzcase.Model, diagnostics []diag.SourceError) []string {
	if model.Query().Writes {
		return nil
	}
	var problems []string
	wrapped := false
	for _, diagnostic := range diagnostics {
		switch diagnostic.Code {
		case diag.CodeCountSemanticsWrapped:
			wrapped = true
			if diagnostic.Severity != diag.SeverityHint {
				problems = append(problems, "count wrapping was not informational")
			}
		case diag.CodeCountRewriteUnavailable:
			problems = append(problems, "supported model lost its count rewrite")
		}
	}
	if wrapped != model.CountWrapped() {
		problems = append(problems, fmt.Sprintf("count wrapping hint is %t, expected %t", wrapped, model.CountWrapped()))
	}
	return problems
}
