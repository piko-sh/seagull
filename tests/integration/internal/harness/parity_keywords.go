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
	_ "embed"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parityKeywords lists every single-word keyword of the engines Seagull supports, read
// from pg_get_keywords() on PostgreSQL and CockroachDB, information_schema.KEYWORDS on
// MySQL and MariaDB, system.keywords on ClickHouse, duckdb_keywords() on DuckDB and the
// SQLite keyword list, one per line.
//
//go:embed parity_keywords.txt
var parityKeywords string

// keywordForms are the ways a keyword is written as an output column's name.
var keywordForms = []struct {
	label  string
	format string
}{
	{label: "as", format: "SELECT 1 AS %s"},
	{label: "bare", format: "SELECT 1 %s"},
}

// CheckKeywordAliasParity writes every engine keyword as an output column's alias, with
// and without AS, and fails for each one Seagull accepts and the engine refuses, whose
// generated query would fail when it runs. Keywords Seagull refuses and the engine takes
// are logged.
//
// Takes t (*testing.T) which reports the disagreements.
// Takes suite (FuzzSuite) which supplies the dialect and a database.
func CheckKeywordAliasParity(t *testing.T, suite FuzzSuite) {
	t.Helper()
	statements := make([]parityStatement, 0, len(keywordForms)*strings.Count(parityKeywords, "\n"))
	for word := range strings.FieldsSeq(parityKeywords) {
		for _, form := range keywordForms {
			statements = append(statements, parityStatement{Label: form.label + " " + word, SQL: fmt.Sprintf(form.format, word)})
		}
	}
	migrations := fstest.MapFS{"migrations/0001_empty.up.sql": {Data: []byte("-- The keyword sweep reads no table.\n")}}
	report, err := checkParity(t.Context(), suite, suite.Open(t), migrations, statements)
	require.NoError(t, err)
	t.Logf("keyword alias parity: %d statements, %d accepted by Seagull and refused by the engine, %d accepted with a warning and refused, "+
		"%d refused by Seagull and accepted by the engine", report.Statements, len(report.Missed), len(report.Warned), len(report.Refused))
	for _, mismatch := range report.Missed {
		assert.Failf(t, "keyword alias parity", "Seagull accepts %q, which the engine refuses: %v", mismatch.SQL, mismatch.Engine)
	}
	for _, mismatch := range report.Refused {
		t.Logf("Seagull refuses %q, which the engine accepts: %s", mismatch.SQL, mismatch.Seagull)
	}
}
