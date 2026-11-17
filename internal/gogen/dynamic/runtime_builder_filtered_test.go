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

package dynamic

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

const windowStatement = "SELECT i.id, i.grp, row_number() OVER (ORDER BY i.id) AS rn FROM items i WHERE i.grp = "

func windowBuilderQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:           "NumberItems",
		Filename:       "items.sql",
		Command:        sqlmodel.QueryCommandMany,
		DynamicRuntime: true,
		ReadOnly:       true,
		Parameters:     []sqlmodel.QueryParameter{textParam("grp", 1)},
		AllowedColumns: []sqlmodel.AllowedColumn{
			{Name: "id", SourceQualifier: "i", SourceColumn: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			{Name: "grp", SourceQualifier: "i", SourceColumn: "grp", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), textColumn("grp", false), textColumn("rn", false)},
		Template: template(
			text(windowStatement), parameter(0), text(" "),
			clause(sqlmodel.SegmentOrderBy, text("ORDER BY i.id")),
			clause(sqlmodel.SegmentPagination),
		),
		CountTemplate: new(template(text("SELECT COUNT(*) FROM ("+windowStatement), parameter(0), text(") sub"))),
		FilteredTemplate: new(template(
			text("SELECT * FROM ("+windowStatement), parameter(0), text(" ORDER BY i.id) AS result_rows"),
			clause(sqlmodel.SegmentFilter),
			clause(sqlmodel.SegmentOrderBy),
			clause(sqlmodel.SegmentPagination),
		)),
		FilteredCountTemplate: new(template(
			text("SELECT COUNT(*) FROM ("+windowStatement), parameter(0), text(" ORDER BY i.id) AS result_rows"),
			clause(sqlmodel.SegmentFilter),
		)),
	}
}

func TestRuntimeBuilderDeclaresTheFilteredFormOfAQueryWhoseConditionsFilterItsResultRows(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	source := renderDecls(t, BuildRuntimeBuilderDeclarations(emission, queryFor(t, emission, windowBuilderQuery())))

	require.Contains(t, source, `numberItemsAllowedColumns = map[string]string{"grp": "\"i\".\"grp\"", "id": "\"i\".\"id\""}`)
	require.Contains(t, source, `numberItemsFilteredColumns = map[string]string{"grp": "\"grp\"", "id": "\"id\""}`)
	require.Contains(t, source, "const numberItemsCountSQL = `SELECT COUNT(*) FROM ("+windowStatement+"$1) sub`")
	require.Contains(t, source, "const numberItemsFilteredSQL = `SELECT * FROM ("+windowStatement+"$1 ORDER BY i.id) AS result_rows`")
	require.Contains(t, source, "const numberItemsFilteredCountSQL = `SELECT COUNT(*) FROM ("+windowStatement+"$1 ORDER BY i.id) AS result_rows`")
	require.Contains(t, source, "\torders\t\t[]string\n\tfilteredOrders\t[]string\n")
	require.Contains(t, source, "predicate, err := sglNewPredicate(column, operator, value, numberItemsFilteredColumns)")
	require.Contains(t, source, `func (builder *NumberItemsBuilder) OrderBy(column string, direction string) *NumberItemsBuilder {
	order, err := sglNewOrder(column, direction, numberItemsAllowedColumns, false)
	if err != nil {
		if builder.pendingError == nil {
			builder.pendingError = err
		}
		return builder
	}
	filteredOrder, err := sglNewOrder(column, direction, numberItemsFilteredColumns, false)
	if err != nil {
		if builder.pendingError == nil {
			builder.pendingError = err
		}
		return builder
	}
	builder.orders = append(builder.orders, order)
	builder.filteredOrders = append(builder.filteredOrders, filteredOrder)
	return builder
}`)
}

func TestRuntimeBuilderRunsTheFilteredFormOnceWhereAddedACondition(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	built := newBuilder(emission, queryFor(t, emission, windowBuilderQuery()))

	require.Equal(t, `func (builder *NumberItemsBuilder) buildQuery() (string, []any, error) {
	if len(builder.predicates) > 0 {
		return builder.buildFilteredQuery()
	}
	if builder.pendingError != nil {
		return "", nil, builder.pendingError
	}
	args := make([]any, 0, 1+sglPaginationCount(builder.limit, builder.offset))
	args = append(args, builder.params)
	pagination, args := sglPaginationInto(builder.limit, builder.offset, 2, args)
	if err := sglCheckBinds(len(args)); err != nil {
		return "", nil, err
	}
	return sglSplice(numberItemsSQL, sglOrder(builder.orders, 90, 103, true), sglAt(pagination, 103)), args, nil
}`, renderDecl(t, built.buildQueryMethod()))
	require.Equal(t, `func (builder *NumberItemsBuilder) buildFilteredQuery() (string, []any, error) {
	if builder.pendingError != nil {
		return "", nil, builder.pendingError
	}
	args := make([]any, 0, 1+sglFilterCount(builder.predicates)+sglPaginationCount(builder.limit, builder.offset))
	args = append(args, builder.params)
	filter, args := sglFilterInto(builder.predicates, 2, args)
	pagination, args := sglPaginationInto(builder.limit, builder.offset, 2+sglFilterCount(builder.predicates), args)
	if err := sglCheckBinds(len(args)); err != nil {
		return "", nil, err
	}
	return sglSplice(numberItemsFilteredSQL, sglWhereOpen(filter, 134, false), sglWhereClose(filter, 134, false), sglOrder(builder.filteredOrders, 134, 134, false), sglAt(pagination, 134)), args, nil
}`, renderDecl(t, built.buildFilteredQueryMethod()))
	require.Equal(t, `func (builder *NumberItemsBuilder) buildCountQuery() (string, []any, error) {
	if len(builder.predicates) > 0 {
		return builder.buildFilteredCountQuery()
	}
	if builder.pendingError != nil {
		return "", nil, builder.pendingError
	}
	args := make([]any, 0, 1)
	args = append(args, builder.params)
	if err := sglCheckBinds(len(args)); err != nil {
		return "", nil, err
	}
	return sglSplice(numberItemsCountSQL), args, nil
}`, renderDecl(t, built.buildCountQueryMethod()))
	require.Contains(t, renderDecl(t, built.buildFilteredCountQueryMethod()),
		"return sglSplice(numberItemsFilteredCountSQL, sglWhereOpen(filter, 141, false), sglWhereClose(filter, 141, false)), args, nil")
}

func TestRuntimeBuilderWithoutAFilteredFormKeepsOneForm(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	source := renderDecls(t, BuildRuntimeBuilderDeclarations(emission, queryFor(t, emission, runtimeBuilderQuery())))

	require.NotContains(t, source, "Filtered")
	require.NotContains(t, source, "filteredOrders")
}

const limitedStatement = "SELECT id, region FROM players WHERE score > "

func limitedBuilderQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:           "TopPlayers",
		Filename:       "players.sql",
		Command:        sqlmodel.QueryCommandMany,
		DynamicRuntime: true,
		ReadOnly:       true,
		Parameters:     []sqlmodel.QueryParameter{textParam("score", 1)},
		AllowedColumns: []sqlmodel.AllowedColumn{
			{Name: "id", SourceColumn: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			{Name: "region", SourceColumn: "region", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), textColumn("region", false)},
		Template: template(
			text(limitedStatement), parameter(0), text(" "),
			clause(sqlmodel.SegmentOrderBy, text("ORDER BY score DESC")), text(" "),
			clause(sqlmodel.SegmentPagination, text("LIMIT 10")),
		),
		CountTemplate: new(template(text("SELECT COUNT(*) FROM players WHERE score > "), parameter(0))),
		FilteredTemplate: new(template(
			text("SELECT * FROM ("+limitedStatement), parameter(0), text(" ORDER BY score DESC LIMIT 10) AS result_rows"),
			clause(sqlmodel.SegmentFilter),
			clause(sqlmodel.SegmentOrderBy),
			clause(sqlmodel.SegmentPagination),
		)),
		FilteredCountTemplate: new(template(
			text("SELECT COUNT(*) FROM ("+limitedStatement), parameter(0), text(" ORDER BY score DESC LIMIT 10) AS result_rows"),
			clause(sqlmodel.SegmentFilter),
		)),
	}
}

func TestRuntimeBuilderFiltersTheRowsOfAQueryWithItsOwnLimit(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	query := queryFor(t, emission, limitedBuilderQuery())
	source := renderDecls(t, BuildRuntimeBuilderDeclarations(emission, query))
	built := newBuilder(emission, query)

	require.NotContains(t, source, ") Limit(")
	require.NotContains(t, source, ") Offset(")
	require.Contains(t, source, "const topPlayersFilteredSQL = `SELECT * FROM ("+limitedStatement+"$1 ORDER BY score DESC LIMIT 10) AS result_rows`")
	require.Contains(t, renderDecl(t, built.buildQueryMethod()), "if len(builder.predicates) > 0 {\n\t\treturn builder.buildFilteredQuery()\n\t}")
	require.Equal(t, `func (builder *TopPlayersBuilder) buildFilteredQuery() (string, []any, error) {
	if builder.pendingError != nil {
		return "", nil, builder.pendingError
	}
	args := make([]any, 0, 1+sglFilterCount(builder.predicates))
	args = append(args, builder.params)
	filter, args := sglFilterInto(builder.predicates, 2, args)
	if err := sglCheckBinds(len(args)); err != nil {
		return "", nil, err
	}
	return sglSplice(topPlayersFilteredSQL, sglWhereOpen(filter, 107, false), sglWhereClose(filter, 107, false), sglOrder(builder.filteredOrders, 107, 107, false)), args, nil
}`, renderDecl(t, built.buildFilteredQueryMethod()))
	require.Contains(t, renderDecl(t, built.buildCountQueryMethod()), "if len(builder.predicates) > 0 {\n\t\treturn builder.buildFilteredCountQuery()\n\t}")
}
