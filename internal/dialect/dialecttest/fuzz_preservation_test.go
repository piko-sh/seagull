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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestPreservationOracleRejectsLostCommentsLiteralsAndWhitespace(t *testing.T) {
	for _, name := range dialects.Names() {
		t.Run(name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			info := engine.Info()
			source := "SELECT  'literal ? {x:Int64}', /* keep LIMIT 5 */ 7 -- keep ORDER BY\n;"
			query := &sqlmodel.AnalysedQuery{Name: "Preserved", SQL: source}
			require.Empty(t, templatePreservationProblems(&info, query, emitkit.Rendered{SQL: source}))
			for _, change := range []string{
				strings.Replace(source, "/* keep LIMIT 5 */", "", 1),
				strings.Replace(source, "-- keep ORDER BY", "", 1),
				strings.Replace(source, "'literal ? {x:Int64}'", "'different'", 1),
				strings.Replace(source, "  ", " ", 1),
			} {
				require.NotEmpty(t, templatePreservationProblems(&info, query, emitkit.Rendered{SQL: change}))
			}
		})
	}
}

func TestPreservationOracleLimitsItsListRewriteExemption(t *testing.T) {
	engine, err := dialects.New("postgres", dialects.Options{})
	require.NoError(t, err)
	info := engine.Info()
	query := &sqlmodel.AnalysedQuery{
		Name: "List", SQL: "SELECT id /* outside */ FROM t WHERE id IN ( /* inside */ $1 )",
		Parameters: []sqlmodel.QueryParameter{{Number: 1, IsSlice: true}},
	}
	output := emitkit.Rendered{SQL: "SELECT id /* outside */ FROM t WHERE id IN ($1)", Arguments: []emitkit.Argument{{Parameter: 0}}}
	require.Empty(t, templatePreservationProblems(&info, query, output))
	output.SQL = strings.Replace(output.SQL, "/* outside */", "", 1)
	require.NotEmpty(t, templatePreservationProblems(&info, query, output))
	query.Parameters[0].IsSlice = false
	output.SQL = "SELECT id /* outside */ FROM t WHERE id IN ($1)"
	require.NotEmpty(t, templatePreservationProblems(&info, query, output))
}
