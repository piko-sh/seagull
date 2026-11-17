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
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
)

// identifierForms are the ways a keyword is written as a name in a migration, each a
// format taking the keyword and, where the engine needs one, its table engine clause.
var identifierForms = []struct {
	label  string
	format string
}{
	{label: "column", format: "CREATE TABLE parity_t (id INT, %s INT)%s"},
	{label: "table", format: "CREATE TABLE %s (id INT)%s"},
}

// identifierParityExclusions lists, per engine, the keywords the identifier check leaves
// out, each with the reason.
var identifierParityExclusions = map[string]map[string]string{
	engineCockroachDB: {
		"INDEX_AFTER_ORDER_BY_BEFORE_AT": "a token CockroachDB's grammar uses internally, which pg_get_keywords lists but no schema names a column",
		"INDEX_BEFORE_NAME_THEN_PAREN":   "a token CockroachDB's grammar uses internally, which pg_get_keywords lists but no schema names a column",
		"INDEX_BEFORE_PAREN":             "a token CockroachDB's grammar uses internally, which pg_get_keywords lists but no schema names a column",
		"NOTHING_AFTER_RETURNING":        "a token CockroachDB's grammar uses internally, which pg_get_keywords lists but no schema names a column",
	},
}

// CheckKeywordIdentifierParity writes every engine keyword unquoted as a column's and a
// table's name in a migration, and fails for each one Seagull accepts and the engine
// refuses, whose migration would fail when it runs.
//
// Takes t (*testing.T) which reports the disagreements.
// Takes suite (FuzzSuite) which supplies the dialect and a database.
func CheckKeywordIdentifierParity(t *testing.T, suite FuzzSuite) {
	t.Helper()
	database := suite.Open(t)
	suffix := tableSuffix(suite.Engine)
	generator, err := seagull.New(suite.Dialect(), seagull.WithSkipFailingQueries())
	require.NoError(t, err)
	missed, refused, checked := 0, 0, 0
	for word := range strings.FieldsSeq(parityKeywords) {
		if reason, excluded := identifierParityExclusions[suite.Engine][word]; excluded {
			t.Logf("capability exclusion: %s: %s", word, reason)
			continue
		}
		for _, form := range identifierForms {
			statement := fmt.Sprintf(form.format, word, suffix)
			checked++
			engineErr := engineAcceptsDDL(t.Context(), database, suite.Engine, statement)
			refusal := seagullMigrationRefusal(t.Context(), generator, statement)
			switch {
			case engineErr != nil && refusal == "":
				missed++
				assert.NoError(t, engineErr, "Seagull accepts the migration %q, which the engine refuses", statement)
			case engineErr == nil && refusal != "":
				refused++
				t.Logf("Seagull refuses the migration %q, which the engine accepts: %s", statement, refusal)
			}
		}
	}
	t.Logf("keyword identifier parity: %d statements, %d accepted by Seagull and refused by the engine, %d refused by Seagull and accepted by the engine",
		checked, missed, refused)
}

// seagullMigrationRefusal builds a catalogue from one migration and reads whether Seagull
// refuses it.
//
// Takes generator (*seagull.Generator) which is set up for the engine's dialect.
// Takes statement (string) which is the migration's only statement.
//
// Returns string which is the first error Seagull reported, or empty when it accepts the
// migration.
func seagullMigrationRefusal(ctx context.Context, generator *seagull.Generator, statement string) string {
	files := fstest.MapFS{
		"migrations/0001_parity.up.sql": {Data: []byte(statement + ";\n")},
		"queries/parity.sql":            {Data: []byte("-- sgl.query(name: Parity, command: one)\nSELECT 1;\n")},
	}
	details, err := generator.GenerateDetailed(ctx, seagull.Input{Package: "parity", PackagePath: "", Queries: nil, Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	if err != nil {
		return err.Error()
	}
	if details.Report == nil {
		return ""
	}
	for _, diagnostic := range details.Report.Diagnostics {
		if diagnostic.Severity == seagull.SeverityError {
			return diagnostic.Message
		}
	}
	return ""
}
