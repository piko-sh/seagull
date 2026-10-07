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

package gogen

import (
	"slices"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// sqlTexts holds the texts the query-names lookup names queries by, as it gathers them.
type sqlTexts struct {
	// exact holds the SQL of the constants named so far.
	exact map[string]bool

	// stems holds the text before the first run-time edit of the constants edited so far.
	stems map[string]bool
}

// claim records the texts a query's SQL is named by and reports whether an earlier
// query's SQL holds one of them already.
//
// Takes driver (*Driver) which is the driver the package is generated for.
// Takes query (*Query) which is the query, rendered without a prefix.
//
// Returns bool which is true when an earlier query runs SQL written alike.
func (texts sqlTexts) claim(driver *Driver, query *Query) bool {
	runsSQL, repeatsRow := StatementRun(driver, query)
	if !runsSQL {
		return false
	}
	shared := texts.claimRendered(&query.Rendered, FirstEdit(query, &query.Rendered, repeatsRow))
	if query.Count != nil {
		shared = texts.claimRendered(query.Count, FirstEdit(query, query.Count, repeatsRow)) || shared
	}
	return shared
}

// claimRendered records the texts one rendering is named by and reports whether either
// was recorded before.
//
// Takes rendered (*emitkit.Rendered) which is the rendering.
// Takes edit (int) which is the offset of its first run-time edit, or -1 for none.
//
// Returns bool which is true when its SQL, or its text before the edit, was recorded
// before.
func (texts sqlTexts) claimRendered(rendered *emitkit.Rendered, edit int) bool {
	shared := texts.exact[rendered.SQL]
	texts.exact[rendered.SQL] = true
	if edit > 0 {
		stem := rendered.SQL[:edit]
		shared = shared || texts.stems[stem]
		texts.stems[stem] = true
	}
	return shared
}

// sharedSQLPrefixes generates identifying comments for queries whose rendered SQL matches
// an earlier query, so the query-names lookup can distinguish them. It returns no
// prefixes when the package has no lookup.
//
// Returns map[*sqlmodel.AnalysedQuery]string which maps each such query to its line.
// Returns error when a query cannot be rendered.
func (e *Emission) sharedSQLPrefixes() (map[*sqlmodel.AnalysedQuery]string, error) {
	if e.prefixes != nil {
		return e.prefixes, nil
	}
	prefixes := make(map[*sqlmodel.AnalysedQuery]string)
	if e.Request.Style.QueryNames.Enabled {
		seen := sqlTexts{exact: make(map[string]bool), stems: make(map[string]bool)}
		for _, analysed := range PlanOrder(e.Request.Queries) {
			query, err := e.renderQuery(analysed, goast.NewImportTracker(), "")
			if err != nil {
				return nil, err
			}
			if seen.claim(e.Driver, query) {
				prefixes[analysed] = emitkit.CommentLine(e.Dialect(), query.Naming.Label)
			}
		}
	}
	e.prefixes = prefixes
	return prefixes, nil
}

// StatementRun reports whether a query runs SQL and repeats its VALUES row.
//
// Copy protocols run no SQL text. Drivers without a batch builder run batch commands as
// plain statements.
//
// Takes driver (*Driver) which is the driver the package is generated for.
// Takes query (*Query) which is the query.
//
// Returns runsSQL (bool) which is false for a method that runs no SQL text.
// Returns repeatsRow (bool) which is true when the method edits the SQL from the VALUES
// row on.
func StatementRun(driver *Driver, query *Query) (runsSQL bool, repeatsRow bool) {
	if driver.Batch == nil {
		return true, false
	}
	switch query.Analysed.Command {
	case sqlmodel.QueryCommandBatch:
		return true, driver.Batch.RepeatsRow(query)
	case sqlmodel.QueryCommandCopyFrom:
		statement := driver.Batch.CopyStatement(query)
		return statement != CopyStatementNone, statement == CopyStatementRepeatsRow
	default:
		return true, false
	}
}

// FirstEdit locates the first run-time edit to a query rendering.
//
// It returns -1 when the rendering executes unchanged.
//
// Takes query (*Query) which is the query.
// Takes rendered (*emitkit.Rendered) which is its rendering or its count's.
// Takes repeatsRow (bool) which is true when the query's method repeats its VALUES row.
//
// Returns int which is the offset.
func FirstEdit(query *Query, rendered *emitkit.Rendered, repeatsRow bool) int {
	marks := &rendered.Marks
	var offsets []int
	if query.Renderer.ListMode == emitkit.ListExpand {
		for _, list := range marks.Lists {
			offsets = append(offsets, list.Operator.Start)
		}
	}
	if marks.OrderBy != nil && (query.Analysed.DynamicRuntime || query.Analysed.IsDynamic && query.Renderer.Omit != 0) {
		offsets = append(offsets, marks.OrderBy.Start)
	}
	if query.Analysed.DynamicRuntime {
		if marks.Filter != nil {
			offsets = append(offsets, marks.Filter.Condition.Start)
		}
		if marks.Pagination != nil {
			offsets = append(offsets, marks.Pagination.Start)
		}
	}
	if marks.Values != nil && repeatsRow {
		offsets = append(offsets, marks.Values.Start)
	}
	if len(offsets) == 0 {
		return -1
	}
	return slices.Min(offsets)
}
