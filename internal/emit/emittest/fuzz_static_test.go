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

package emittest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestFuzzStaticMappingRejectsChangedSQLBindingsAndOutputs(t *testing.T) {
	const sql = "SELECT id, category FROM fuzz_items WHERE id >= $1 AND category < $2 AND 'AbC' <> ''"
	const source = `package generated
 import "context"
 const querySQL = "SELECT id, category FROM fuzz_items WHERE id >= $1 AND category < $2 AND 'AbC' <> ''"
 type row struct { ID int64 ` + "`json:\"id\"`" + `; Category int64 ` + "`json:\"category\"`" + ` }
 type params struct { ID int64; Category int64 }
 type rows interface { Scan(...any) error }
 type database interface { QueryContext(context.Context, string, ...any) (rows, error) }
 type Queries struct { db database }
 func (q *Queries) FuzzQuery(ctx context.Context, p params) (row, error) {
   records, err := q.db.QueryContext(ctx, querySQL, p.ID, p.Category)
   if err != nil { return row{}, err }
   var result row
   scanArgs := []any{&result.ID, &result.Category}
   err = records.Scan(scanArgs...)
   return result, err
 }`
	info := &dialect.Info{Lexical: sqllex.Rules{Parameters: sqllex.ParameterRules{DollarNumbers: true}}, Placeholders: dialect.Placeholders{Sigils: []dialect.PlaceholderSigil{{Prefix: "$", Kind: dialect.PlaceholderNumbered}}, Execution: dialect.PlaceholderSigil{Prefix: "$", Kind: dialect.PlaceholderNumbered}}}
	query := &sqlmodel.AnalysedQuery{Name: "FuzzQuery", SQL: sql,
		Parameters:    []sqlmodel.QueryParameter{{Name: "id", Number: 1}, {Name: "category", Number: 2}},
		OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}, {Name: "category"}},
	}
	generated := &app.DetailedResult{Result: &app.Result{Methods: map[string]string{"FuzzQuery": "FuzzQuery"}}, Report: &app.Report{Queries: []*sqlmodel.AnalysedQuery{query}}}
	cases := []struct{ name, before, after string }{
		{name: "unchanged"},
		{name: "swapped bindings", before: "p.ID, p.Category", after: "p.Category, p.ID"},
		{name: "moved placeholder", before: "id >= $1 AND category < $2", after: "id >= $2 AND category < $1"},
		{name: "changed predicate", before: "id >= $1", after: "id <= $1"},
		{name: "changed literal", before: "'AbC'", after: "'abc'"},
		{name: "missing argument", before: "p.ID, p.Category", after: "p.ID"},
		{name: "unused argument", before: "p.ID, p.Category", after: "p.ID, p.Category, p.ID"},
		{name: "swapped scan buffer", before: "&result.ID, &result.Category", after: "&result.Category, &result.ID"},
		{name: "duplicated scan destination", before: "&result.ID, &result.Category", after: "&result.ID, &result.ID"},
		{name: "changed column mapping", before: "json:\"id\"", after: "json:\"score\""},
		{name: "wrong executed constant", before: "ctx, querySQL,", after: "ctx, \"SELECT score, category FROM fuzz_items WHERE id >= $1 AND category < $2\","},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			text := source
			if testCase.before != "" {
				text = strings.Replace(text, testCase.before, testCase.after, 1)
			}
			checked := typeCheck([]sqlmodel.GeneratedFile{{Name: "query.go", Content: []byte(text)}})
			require.Empty(t, checked.problems)
			problems := fuzzStaticMappingProblems(checked, info, generated)
			if testCase.before == "" {
				require.Empty(t, problems)
			} else {
				require.NotEmpty(t, problems)
			}
		})
	}
}
