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

package methods

import (
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func sliceParam(name string, number int) sqlmodel.QueryParameter {
	parameter := textParam(name, number)
	parameter.IsSlice = true
	return parameter
}

func slicesQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:          "FindItems",
		Filename:      "items.sql",
		Command:       sqlmodel.QueryCommandMany,
		Parameters:    []sqlmodel.QueryParameter{sliceParam("ids", 1), textParam("kind", 2), sliceParam("tags", 3)},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
		Template: template(
			text("SELECT id FROM items WHERE id "), list(0, false),
			text(" AND kind = "), parameter(1),
			text(" AND tag "), list(2, true),
		),
	}
}

func TestPrepareCallSplicesEachSliceAtItsMarkNumberedFromItsCanonicalNumber(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	emission.Dialect().Lists = dialect.ListSyntax{EmptyMembership: "IN (NONE)", EmptyExclusion: "NOT IN (NONE)"}
	query := queryFor(t, emission, slicesQuery())

	call := PrepareCall(emission, query, SQLConstant(query), ReturnGuard(goastutil.CachedIdent("nil")))

	require.Equal(t, "SELECT id FROM items WHERE id IN ($2) AND kind = $1 AND tag NOT IN ($3)", query.Rendered.SQL)
	require.Equal(t, `if err := sglCheckBinds(1 + len(params.IDs) + len(params.Tags)); err != nil {
	return nil, err
}
query := sglSplice(findItemsSQL, sglList(sglListMark{Empty: `+"`IN (NONE)`"+`, Operator: 30, Start: 33, End: 37}, 2, len(params.IDs)), sglList(sglListMark{Empty: `+"`NOT IN (NONE)`"+`, Operator: 60, Start: 67, End: 71}, 2+len(params.IDs), len(params.Tags)))
args := make([]any, 0, 1+len(params.IDs)+len(params.Tags))
args = append(args, params.Kind)
for _, v := range params.IDs {
	args = append(args, v)
}
for _, v := range params.Tags {
	args = append(args, v)
}
`, renderStatements(t, call.Statements))
	require.Equal(t, []string{"ctx", "query", "args"}, renderExpressions(t, call.Arguments))
	require.True(t, call.Spread)
}

func TestPrepareCallBindsEachOccurrenceOfAnAnonymousForm(t *testing.T) {
	t.Parallel()

	emission := indexedEmission(false)
	query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
		Name:          "OrdersFor",
		Filename:      "orders.sql",
		Command:       sqlmodel.QueryCommandMany,
		Parameters:    []sqlmodel.QueryParameter{textParam("who", 1), textParam("id", 2)},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
		Template: template(
			text("SELECT id FROM orders WHERE customer = "), parameter(0),
			text(" OR title = "), parameter(0), text(" OR id = "), parameter(1),
		),
	})

	call := PrepareCall(emission, query, SQLConstant(query), ReturnGuard(goastutil.CachedIdent("nil")))

	require.Equal(t, "SELECT id FROM orders WHERE customer = ? OR title = ? OR id = ?", query.Rendered.SQL)
	require.Empty(t, call.Statements)
	require.Equal(t, []string{"ctx", "ordersForSQL", "params.Who", "params.Who", "params.ID"}, renderExpressions(t, call.Arguments))
	require.False(t, call.Spread)
}

func TestPrepareCallGivesAnExcludedArrayAnEmptySliceForNil(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	emission.Driver.Lists = emitkit.ListArray
	emission.Dialect().Lists = dialect.ListSyntax{
		ArrayMembership: dialect.Affix{Before: "= ANY(", After: ")"},
		ArrayExclusion:  dialect.Affix{Before: "<> ALL(", After: ")"},
	}
	query := queryFor(t, emission, slicesQuery())

	call := PrepareCall(emission, query, SQLConstant(query), ReturnGuard(goastutil.CachedIdent("nil")))

	require.Equal(t, "SELECT id FROM items WHERE id = ANY($2) AND kind = $1 AND tag <> ALL($3)", query.Rendered.SQL)
	require.Equal(t, "if params.Tags == nil {\n\tparams.Tags = []string{}\n}\n", renderStatements(t, call.Statements))
	require.Equal(t, []string{"ctx", "findItemsSQL", "params.Kind", "params.IDs", "params.Tags"}, renderExpressions(t, call.Arguments))
}
