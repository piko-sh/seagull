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

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

func runtimeBuilderQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:           "SearchPosts",
		Filename:       "posts.sql",
		Command:        sqlmodel.QueryCommandMany,
		DynamicRuntime: true,
		ReadOnly:       true,
		Parameters:     []sqlmodel.QueryParameter{textParam("environment_id", 1), textParam("kind", 2)},
		AllowedColumns: []sqlmodel.AllowedColumn{
			{Name: "title", SourceExpression: "posts.title", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			{Name: "created_at", SourceExpression: "posts.created_at", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), textColumn("title", false)},
		Template: template(
			text("SELECT id, title FROM posts WHERE "),
			clause(sqlmodel.SegmentFilter, text("environment_id = "), parameter(0), text(" OR kind = "), parameter(1)),
			clause(sqlmodel.SegmentOrderBy),
			clause(sqlmodel.SegmentPagination),
		),
		CountTemplate: new(template(
			text("SELECT COUNT(*) FROM posts WHERE "),
			clause(sqlmodel.SegmentFilter, text("environment_id = "), parameter(0), text(" OR kind = "), parameter(1)),
		)),
	}
}

func TestRuntimeBuilderDeclaresItsWholeSurface(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	source := renderDecls(t, BuildRuntimeBuilderDeclarations(emission, queryFor(t, emission, runtimeBuilderQuery())))

	require.Contains(t, source, `searchPostsAllowedColumns = map[string]string{"created_at": "\"posts\".\"created_at\"", "title": "\"posts\".\"title\""}`)
	require.Contains(t, source, "const searchPostsCountSQL = `SELECT COUNT(*) FROM posts WHERE environment_id = $1 OR kind = $2`")
	require.Contains(t, source, "type SearchPostsBuilder struct {\n\tqueries\t\t*Queries\n\tparams\t\tSearchPostsParams\n\tpredicates\t[]sglPredicate\n\torders\t\t[]string\n\tlimit\t\tint\n\toffset\t\tint\n\tpendingError\terror\n}")
	require.Contains(t, source, "func (queries *Queries) SearchPosts(params SearchPostsParams) *SearchPostsBuilder {\n\treturn &SearchPostsBuilder{queries: queries, params: params}\n}")
	require.Contains(t, source, "predicate, err := sglNewPredicate(column, operator, value, searchPostsAllowedColumns)")
	require.Contains(t, source, "order, err := sglNewOrder(column, direction, searchPostsAllowedColumns)")
	require.Contains(t, source, "func (builder *SearchPostsBuilder) Limit(n int) *SearchPostsBuilder")
	require.Contains(t, source, "func (builder *SearchPostsBuilder) Offset(n int) *SearchPostsBuilder")
	require.Contains(t, source, "func (builder *SearchPostsBuilder) All(ctx context.Context) ([]SearchPostsRow, error)")
	require.Contains(t, source, "func (builder *SearchPostsBuilder) One(ctx context.Context) (SearchPostsRow, error)")
	require.Contains(t, source, "func (builder *SearchPostsBuilder) Count(ctx context.Context) (int64, error)")
}

func TestRuntimeBuilderWritesTheCallersAdditionsAtTheMarksWithItsOwnNumbering(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	built := newBuilder(emission, queryFor(t, emission, runtimeBuilderQuery()))

	require.Equal(t, `func (builder *SearchPostsBuilder) buildQuery() (string, []any, error) {
	if builder.pendingError != nil {
		return "", nil, builder.pendingError
	}
	args := make([]any, 0, 2+sglFilterCount(builder.predicates)+sglPaginationCount(builder.limit, builder.offset))
	args = append(args, builder.params.EnvironmentID)
	args = append(args, builder.params.Kind)
	filter, args := sglFilterInto(builder.predicates, 3, args)
	pagination, args := sglPaginationInto(builder.limit, builder.offset, 3+sglFilterCount(builder.predicates), args)
	if err := sglCheckBinds(len(args)); err != nil {
		return "", nil, err
	}
	return sglSplice(searchPostsSQL, sglWhereOpen(filter, 34, true), sglWhereClose(filter, 66, true), sglOrder(builder.orders, 66, 66, false), sglAt(pagination, 66)), args, nil
}`, renderDecl(t, built.buildQueryMethod()))
	require.Equal(t, `func (builder *SearchPostsBuilder) buildCountQuery() (string, []any, error) {
	if builder.pendingError != nil {
		return "", nil, builder.pendingError
	}
	args := make([]any, 0, 2+sglFilterCount(builder.predicates))
	args = append(args, builder.params.EnvironmentID)
	args = append(args, builder.params.Kind)
	filter, args := sglFilterInto(builder.predicates, 3, args)
	if err := sglCheckBinds(len(args)); err != nil {
		return "", nil, err
	}
	return sglSplice(searchPostsCountSQL, sglWhereOpen(filter, 33, true), sglWhereClose(filter, 65, true)), args, nil
}`, renderDecl(t, built.buildCountQueryMethod()))
}

func TestRuntimeBuilderOfAnAnonymousFormBindsTheCallersValuesWhereTheyAreWritten(t *testing.T) {
	t.Parallel()

	emission := indexedEmission(false)
	query := runtimeBuilderQuery()
	query.Parameters = append(query.Parameters, textParam("page", 3))
	query.Template = template(
		text("SELECT id, title FROM posts WHERE "),
		clause(sqlmodel.SegmentFilter, text("kind = "), parameter(1)),
		clause(sqlmodel.SegmentOrderBy),
		text(" GROUP BY id HAVING count(*) > "), parameter(0),
		text(" "), clause(sqlmodel.SegmentPagination, text("LIMIT "), parameter(2)),
	)
	query.CountTemplate = nil
	built := newBuilder(emission, queryFor(t, emission, query))

	source := renderDecl(t, built.buildQueryMethod())

	require.Contains(t, source, `	args = append(args, builder.params.Kind)
	filter, args := sglFilterInto(builder.predicates, 4, args)
	args = append(args, builder.params.EnvironmentID)
	args = append(args, builder.params.Page)
`)
	require.NotContains(t, source, "pagination")
	require.Nil(t, built.pagination)
}

func TestRuntimeBuilderOfAFormBindingByNameBindsItsValuesByPosition(t *testing.T) {
	t.Parallel()

	braced := dialect.PlaceholderSigil{Prefix: "{", Suffix: "}", TypeSeparator: ":", Kind: dialect.PlaceholderNamed}
	driver := testDriver()
	driver.Binder = namedBinder{}
	emission := testEmission(testDialectInfo(braced, dollar), textMappings(), driver)
	built := newBuilder(emission, queryFor(t, emission, runtimeBuilderQuery()))

	for name, source := range map[string]string{"query": renderDecl(t, built.buildQueryMethod()), "count": renderDecl(t, built.buildCountQueryMethod())} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Contains(t, source, "\targs = append(args, builder.params.EnvironmentID)\n\targs = append(args, builder.params.Kind)\n")
			require.NotContains(t, source, "driver.Named")
		})
	}
}

func TestRuntimeBuilderLeavesOutWhatTheQueryHasNoPlaceFor(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	query := runtimeBuilderQuery()
	query.Template = template(text("SELECT id, title FROM a UNION SELECT id, title FROM b "), clause(sqlmodel.SegmentOrderBy, text("ORDER BY id")), text(" "), clause(sqlmodel.SegmentPagination, text("LIMIT 5")))
	query.Parameters = nil
	query.CountTemplate = nil

	source := renderDecls(t, BuildRuntimeBuilderDeclarations(emission, queryFor(t, emission, query)))

	require.NotContains(t, source, ") Where(")
	require.NotContains(t, source, ") Limit(")
	require.NotContains(t, source, ") Count(")
	require.NotContains(t, source, "params")
	require.Contains(t, source, ") OrderBy(")
	require.Contains(t, source, "sglOrder(builder.orders, 54, 65, true)")
}

func TestRuntimeBuilderChainablesKeepTheFirstErrorWithoutPanicking(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	built := newBuilder(emission, queryFor(t, emission, runtimeBuilderQuery()))

	require.Equal(t, `func (builder *SearchPostsBuilder) Where(column string, operator string, value any) *SearchPostsBuilder {
	predicate, err := sglNewPredicate(column, operator, value, searchPostsAllowedColumns)
	if err != nil {
		if builder.pendingError == nil {
			builder.pendingError = err
		}
		return builder
	}
	builder.predicates = append(builder.predicates, predicate)
	return builder
}`, renderDecl(t, built.whereMethod()))
}

func TestRuntimeBuilderEntryPointTakesOnlyTheQuerysArguments(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		parameters []sqlmodel.QueryParameter
		want       string
	}{
		{name: "no parameter", parameters: nil, want: "func (queries *Queries) SearchPosts() *SearchPostsBuilder {"},
		{name: "one parameter", parameters: runtimeBuilderQuery().Parameters[:1], want: "func (queries *Queries) SearchPosts(environmentID string) *SearchPostsBuilder {"},
		{name: "a Params struct", parameters: runtimeBuilderQuery().Parameters, want: "func (queries *Queries) SearchPosts(params SearchPostsParams) *SearchPostsBuilder {"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := runtimeBuilderQuery()
			query.Parameters = testCase.parameters
			query.Template = template(text("SELECT id, title FROM posts WHERE "), clause(sqlmodel.SegmentFilter, text("TRUE")),
				clause(sqlmodel.SegmentOrderBy), clause(sqlmodel.SegmentPagination))
			query.CountTemplate = nil
			emission := runtimeEmission()

			source := renderDecls(t, BuildRuntimeBuilderDeclarations(emission, queryFor(t, emission, query)))

			require.Contains(t, source, testCase.want)
			require.NotContains(t, source, "SearchPosts(ctx")
		})
	}
}
