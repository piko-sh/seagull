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

package sqltemplate

import (
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

// distinctOnWords is how many words open a DISTINCT ON quantifier.
const distinctOnWords = 2

// filtersResult reports whether conditions apply to a query's result rows.
//
// A noncompound SELECT with aggregates, windows, or set-returning items needs this
// because WHERE would change their inputs. A statement that picks its own rows also needs
// conditions applied after that choice.
//
// Returns bool which is true when the conditions filter the result rows.
func (b *builder) filtersResult() bool {
	return b.runtimeSelect() && (b.input.ItemsWrapCount || b.picksRows())
}

// wrapsPickedRows reports whether a runtime builder query is always read through a
// derived table, because the statement picks its own rows. The builder's conditions,
// order, limit and count then apply to the rows the statement returns, as written.
//
// Returns bool which is true when the query's template wraps the statement.
func (b *builder) wrapsPickedRows() bool {
	return b.runtimeSelect() && b.picksRows()
}

// runtimeSelect reports whether the query is a runtime builder query over a SELECT that
// is not compound.
//
// Returns bool which is true for such a query.
func (b *builder) runtimeSelect() bool {
	return b.input.Query.DynamicRuntime && len(b.input.Analysis.CompoundBranches) == 0 && b.selectsRows()
}

// picksRows reports whether the statement picks which of its rows it returns: by its own
// LIMIT, OFFSET or FETCH, by DISTINCT ON, or by a clause such as QUALIFY or ClickHouse's
// LIMIT n BY.
//
// Returns bool which is true when such a clause is written.
func (b *builder) picksRows() bool {
	return b.spans.Pagination.Known() || len(b.spans.RowShaping) > 0 || b.distinctOn()
}

// distinctOn reports whether the statement's quantifier is DISTINCT ON, which keeps one
// row of each group by the statement's order.
//
// Returns bool which is true for DISTINCT ON.
func (b *builder) distinctOn() bool {
	quantifier := b.absolute(b.spans.Quantifier)
	if !quantifier.Known() {
		return false
	}
	words := make([]sqllex.Token, 0, distinctOnWords)
	for _, token := range b.tokens {
		if token.Start >= quantifier.End || len(words) == distinctOnWords {
			break
		}
		if token.Start >= quantifier.Start {
			words = append(words, token)
		}
	}
	return len(words) == distinctOnWords && words[0].IsWord("DISTINCT") && words[1].IsWord("ON")
}

// readableByName reports whether the statement's rows can be read from a derived table by
// their output names. When they cannot, it reports the query, and that error stops its
// code being generated.
//
// Returns bool which is true when every output column has a name no other has.
func (b *builder) readableByName() bool {
	if b.namesDistinct() {
		return true
	}
	b.report(diag.CodeRuntimeBuilderRowNames, ast.Span{},
		"a dynamic: runtime query that picks its own rows, by LIMIT, OFFSET, FETCH, DISTINCT ON or a clause such as "+
			"QUALIFY, is read from a derived table by its output names, so each output column needs a name no other has; "+
			"give the columns distinct aliases")
	return false
}

// pickedRows finishes the templates of a query wrapsPickedRows applies to. The rows
// template wraps the statement as written in a derived table followed by the places of
// the filter, the ORDER BY and the pagination, and the count counts that derived table's
// rows through the filter's place.
//
// Takes root (*node) which is the query's tree.
// Takes built (Built) which holds what Build made so far.
//
// Returns Built which holds the templates, or no template when they cannot be written.
// Returns []Problem which holds the problems found.
func (b *builder) pickedRows(root *node, built Built) (Built, []Problem) {
	rows, rowsOK := b.wrappedRows(root, sqlmodel.SegmentFilter, sqlmodel.SegmentOrderBy, sqlmodel.SegmentPagination)
	count, countOK := b.derivedCount(root, sqlmodel.SegmentFilter)
	if !rowsOK || !countOK {
		b.report(diag.CodeInternalNilGuard, ast.Span{}, "the derived table of a query that picks its own rows could not be written")
		return built, b.problems
	}
	built.Template = b.withArguments(rows)
	built.CountTemplate = new(b.withArguments(count))
	built.CountWrapped = true
	built.ReadsResultRows = true
	return built, b.problems
}

// filteredTemplates builds row and count templates for result-row conditions.
//
// The rows template wraps the original statement, then applies filtering, ordering, and
// pagination. The count template counts filtered rows from that derived table.
//
// Takes root (*node) which is the query's tree.
// Takes counts (bool) which is true when the query's rows can be counted.
//
// Returns rows (*sqlmodel.SQLTemplate) which reads the filtered rows, or nil when the
// conditions do not filter the result rows or a template cannot be written.
// Returns count (*sqlmodel.SQLTemplate) which counts them, or nil when rows is nil or
// counts is false.
func (b *builder) filteredTemplates(root *node, counts bool) (rows *sqlmodel.SQLTemplate, count *sqlmodel.SQLTemplate) {
	if !b.filtersResult() || !b.namesDistinct() {
		return nil, nil
	}
	segments, ok := b.wrappedRows(root, sqlmodel.SegmentFilter, sqlmodel.SegmentOrderBy, sqlmodel.SegmentPagination)
	if !ok {
		return nil, nil
	}
	rows = new(b.withArguments(segments))
	if !counts {
		return rows, nil
	}
	if segments, ok = b.derivedCount(root, sqlmodel.SegmentFilter); !ok {
		return nil, nil
	}
	return rows, new(b.withArguments(segments))
}

// namesDistinct reports whether every output column has a name no other output column
// has, compared under the dialect's identifier rules. A derived table holding the
// statement can then be read by name on every engine.
//
// Returns bool which is true when the names are distinct.
func (b *builder) namesDistinct() bool {
	outputs := b.input.Query.OutputColumns
	seen := make(map[string]struct{}, len(outputs))
	for index := range outputs {
		if outputs[index].Name == "" {
			return false
		}
		key := b.input.Info.Identifiers.Key(outputs[index].Name)
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}
