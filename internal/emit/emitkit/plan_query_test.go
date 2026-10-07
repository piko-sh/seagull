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

package emitkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestQueryPlanKeepsAnArgumentClearOfWhatTheMethodUses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		parameter string
		argument  string
	}{
		{parameter: "user_id", argument: "userID"},
		{parameter: "queries", argument: "queries2"},
		{parameter: "row", argument: "row2"},
		{parameter: "rows", argument: "rows2"},
		{parameter: "ctx", argument: "ctx2"},
		{parameter: "err", argument: "err2"},
		{parameter: "query", argument: "query2"},
		{parameter: "args", argument: "args2"},
		{parameter: "context", argument: "context2"},
		{parameter: "sql", argument: "sql2"},
		{parameter: "uuid", argument: "uuid2"},
		{parameter: "get_thing_sql", argument: "getThingSQL2"},
	}

	for _, test := range tests {
		t.Run(test.parameter, func(t *testing.T) {
			t.Parallel()

			query := queryOf("GetThing", sqlmodel.QueryCommandOne,
				[]sqlmodel.QueryParameter{parameterNamed(test.parameter)}, []sqlmodel.OutputColumn{columnNamed("name")})

			naming := plannedQuery(t, query, emitkit.NameStyle{SQLConstantSuffix: "SQL"})

			require.Empty(t, naming.Params)
			require.Equal(t, []string{test.argument}, naming.ParameterFields)
		})
	}
}

func TestQueryPlanKeepsQueryNamesClearOfTheReservedNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       string
		suffix      string
		method      string
		sqlConstant string
		row         string
	}{
		{name: "a query called Context with no suffix", query: "Context", suffix: "", method: "Context", sqlConstant: "context2", row: "ContextRow"},
		{name: "a query called Context with the SQL suffix", query: "Context", suffix: "SQL", method: "Context", sqlConstant: "contextSQL", row: "ContextRow"},
		{name: "a query called New", query: "New", suffix: "SQL", method: "New", sqlConstant: "newSQL", row: "NewRow"},
		{name: "a query called New with no suffix", query: "New", suffix: "", method: "New", sqlConstant: "new_", row: "NewRow"},
		{name: "a query called WithTx", query: "WithTx", suffix: "SQL", method: "WithTx2", sqlConstant: "withTxSQL", row: "WithTx2Row"},
		{name: "a query called Row with no suffix", query: "Row", suffix: "", method: "Row", sqlConstant: "row2", row: "RowRow"},
		{name: "a query called Queries", query: "Queries", suffix: "", method: "Queries", sqlConstant: "queries2", row: "QueriesRow"},
		{name: "a snake case query", query: "get_user", suffix: "SQL", method: "GetUser", sqlConstant: "getUserSQL", row: "GetUserRow"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			query := queryOf(test.query, sqlmodel.QueryCommandMany, nil, []sqlmodel.OutputColumn{columnNamed("id")})

			naming := plannedQuery(t, query, emitkit.NameStyle{SQLConstantSuffix: test.suffix})

			require.Equal(t, test.method, naming.Method)
			require.Equal(t, test.sqlConstant, naming.SQLConstant)
			require.Equal(t, test.row, naming.Row)
		})
	}
}

func TestQueryPlanWritesTheQueryNameInTheCaseTheStyleAsks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		query          string
		style          emitkit.NameStyle
		sqlConstant    string
		countConstant  string
		allowedColumns string
	}{
		{
			name:           "lower camel case keeps the words with the default suffix",
			query:          "SearchPosts",
			style:          emitkit.NameStyle{SQLConstantSuffix: "SQL"},
			sqlConstant:    "searchPostsSQL",
			countConstant:  "searchPostsCountSQL",
			allowedColumns: "searchPostsAllowedColumns",
		},
		{
			name:           "lower case keeps an earlier generator's names",
			query:          "SearchPosts",
			style:          emitkit.NameStyle{SQLConstantSuffix: "", SQLConstantLowerCase: true},
			sqlConstant:    "searchposts",
			countConstant:  "searchpostsCountSQL",
			allowedColumns: "searchpostsAllowedColumns",
		},
		{
			name:           "a snake case name gives the same names either way",
			query:          "search_posts",
			style:          emitkit.NameStyle{SQLConstantSuffix: "SQL", SQLConstantLowerCase: true},
			sqlConstant:    "searchPostsSQL",
			countConstant:  "searchPostsCountSQL",
			allowedColumns: "searchPostsAllowedColumns",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			query := queryOf(test.query, sqlmodel.QueryCommandMany, []sqlmodel.QueryParameter{parameterNamed("author")},
				[]sqlmodel.OutputColumn{columnNamed("title")})
			query.DynamicRuntime = true
			query.CountTemplate = &sqlmodel.SQLTemplate{}

			naming := plannedQuery(t, query, test.style)

			require.Equal(t, test.sqlConstant, naming.SQLConstant)
			require.Equal(t, test.countConstant, naming.CountConstant)
			require.Equal(t, test.allowedColumns, naming.AllowedColumns)
		})
	}
}

func TestQueryPlanRejectsANameThatCannotBeExported(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", "0rows", "_hidden", "name.A", "my-query", "\xff", "日本"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			query := queryOf(name, sqlmodel.QueryCommandExec, nil, nil)

			naming, problems := emitkit.PlanPackage(requestOf(nil, query), defaultReservation(), emitkit.NameStyle{})

			require.Len(t, problems, 1)
			require.Same(t, query, problems[0].Query)
			require.Contains(t, problems[0].Message, "cannot become an exported Go identifier")
			require.Equal(t, emit.QueryNaming{}, naming.Queries[0])
		})
	}
}

func TestQueryPlanDisambiguatesParameterFields(t *testing.T) {
	t.Parallel()

	query := queryOf("ListThings", sqlmodel.QueryCommandMany,
		[]sqlmodel.QueryParameter{parameterNamed("kind"), parameterNamed("kind"), parameterNamed("Kind2")},
		[]sqlmodel.OutputColumn{columnNamed("id")})

	naming := plannedQuery(t, query, emitkit.NameStyle{SQLConstantSuffix: "SQL"})

	require.Equal(t, "ListThingsParams", naming.Params)
	require.Equal(t, "params", naming.ParamsArgument)
	require.Equal(t, []string{"Kind", "Kind2", "Kind22"}, naming.ParameterFields)
}

func TestQueryPlanNamesColumnsSharingANameInTimeLinearInTheColumns(t *testing.T) {
	t.Parallel()

	sharing := func(count int) *sqlmodel.AnalysedQuery {
		columns := make([]sqlmodel.OutputColumn, count)
		for index := range columns {
			columns[index] = columnNamed("x")
		}
		return queryOf("ListXs", sqlmodel.QueryCommandMany, nil, columns)
	}

	naming := plannedQuery(t, sharing(3), emitkit.NameStyle{SQLConstantSuffix: "SQL"})
	require.Equal(t, []string{"X", "X2", "X3"}, naming.ColumnFields)

	smaller, larger := sharing(2_000), sharing(8_000)
	plan := func(query *sqlmodel.AnalysedQuery) func() {
		return func() {
			_, problems := emitkit.PlanPackage(requestOf(nil, query), defaultReservation(), emitkit.NameStyle{SQLConstantSuffix: "SQL"})
			require.Empty(t, problems)
		}
	}
	scaling.Linear(t, plan(smaller), plan(larger))
}

func TestQueryPlanNamesASortableQuery(t *testing.T) {
	t.Parallel()

	sort := parameterNamed("sort")
	sort.Kind = sqlmodel.ParameterDirectiveSortable
	sort.SortableColumns = []string{"name", "created_at"}
	sortDirection := parameterNamed("sort_direction")
	sortDirection.IsOptional = true
	query := queryOf("ListThings", sqlmodel.QueryCommandMany,
		[]sqlmodel.QueryParameter{sort, sortDirection}, []sqlmodel.OutputColumn{columnNamed("name")})
	query.IsDynamic = true

	naming := plannedQuery(t, query, emitkit.NameStyle{SQLConstantSuffix: "SQL"})

	require.Equal(t, "ListThingsParams", naming.Params)
	require.Equal(t, []string{"Sort", "SortDirection2"}, naming.ParameterFields)
	require.Equal(t, "SortDirection", naming.DirectionField)
	require.Equal(t, "ListThingsOrderBy", naming.OrderBy)
	require.Equal(t, []string{"ListThingsOrderByName", "ListThingsOrderByCreatedAt"}, naming.OrderByConstants)
}

func TestQueryPlanNamesAGroupedQuery(t *testing.T) {
	t.Parallel()

	query := queryOf("ListOrders", sqlmodel.QueryCommandMany, []sqlmodel.QueryParameter{parameterNamed("customer")},
		[]sqlmodel.OutputColumn{
			columnNamed("id"),
			columnNamed("id"),
			embeddedColumn("id", "orders"),
			embeddedColumn("total", "orders"),
			embeddedColumn("id", "items"),
			embeddedColumn("id", "items"),
			embeddedColumn("index", "group"),
		})
	query.GroupByKey = []string{"orders.id"}

	naming := plannedQuery(t, query, emitkit.NameStyle{SQLConstantSuffix: "SQL"})

	require.Equal(t, []string{"customer"}, naming.ParameterFields)
	require.Equal(t, "ListOrdersRow", naming.Row)
	require.Equal(t, []emit.EmbedNaming{
		{Group: "orders", Field: "Orders", Type: "ListOrdersOrders"},
		{Group: "items", Field: "Items", Type: "ListOrdersItems"},
		{Group: "group", Field: "Group", Type: "ListOrdersGroup"},
	}, naming.Embeds)
	require.Equal(t, []string{"ID", "ID2", "ID", "Total", "ID", "ID2", "Index"}, naming.ColumnFields)
	require.Equal(t, []string{"colID", "colID2", "ordersID", "ordersTotal", "itemsID", "itemsID2", "groupIndex2"}, naming.ColumnLocals)
}

func TestQueryPlanKeepsAnEmbedFieldClearOfTheRowFields(t *testing.T) {
	t.Parallel()

	query := queryOf("GetBook", sqlmodel.QueryCommandOne, nil,
		[]sqlmodel.OutputColumn{columnNamed("author"), embeddedColumn("name", "author")})

	naming := plannedQuery(t, query, emitkit.NameStyle{SQLConstantSuffix: "SQL"})

	require.Equal(t, []string{"Author", "Name"}, naming.ColumnFields)
	require.Equal(t, []emit.EmbedNaming{{Group: "author", Field: "Author2", Type: "GetBookAuthor"}}, naming.Embeds)
	require.Nil(t, naming.ColumnLocals)
}

func TestQueryPlanShapesTheMethodByCommand(t *testing.T) {
	t.Parallel()

	slice := parameterNamed("ids")
	slice.IsSlice = true
	optional := parameterNamed("status")
	optional.IsOptional = true

	tests := []struct {
		name            string
		command         sqlmodel.QueryCommand
		parameters      []sqlmodel.QueryParameter
		columns         []sqlmodel.OutputColumn
		dynamic         bool
		params          string
		parameterFields []string
		row             string
	}{
		{name: "no parameters", command: sqlmodel.QueryCommandExec, params: "", parameterFields: nil, row: ""},
		{name: "one parameter", command: sqlmodel.QueryCommandOne, parameters: []sqlmodel.QueryParameter{parameterNamed("id")}, columns: []sqlmodel.OutputColumn{columnNamed("id")}, params: "", parameterFields: []string{"id"}, row: "DoRow"},
		{name: "two parameters", command: sqlmodel.QueryCommandExecRows, parameters: []sqlmodel.QueryParameter{parameterNamed("a"), parameterNamed("b")}, params: "DoParams", parameterFields: []string{"A", "B"}, row: ""},
		{name: "one slice parameter", command: sqlmodel.QueryCommandMany, parameters: []sqlmodel.QueryParameter{slice}, params: "DoParams", parameterFields: []string{"IDs"}, row: "DoRow"},
		{name: "a batch of one parameter", command: sqlmodel.QueryCommandBatch, parameters: []sqlmodel.QueryParameter{parameterNamed("id")}, params: "DoParams", parameterFields: []string{"ID"}, row: ""},
		{name: "a batch returning columns", command: sqlmodel.QueryCommandBatch, parameters: []sqlmodel.QueryParameter{parameterNamed("id")}, columns: []sqlmodel.OutputColumn{columnNamed("id")}, params: "DoParams", parameterFields: []string{"ID"}, row: "DoRow"},
		{name: "a copyfrom", command: sqlmodel.QueryCommandCopyFrom, parameters: []sqlmodel.QueryParameter{parameterNamed("id")}, params: "DoParams", parameterFields: []string{"ID"}, row: ""},
		{name: "an optional parameter", command: sqlmodel.QueryCommandMany, parameters: []sqlmodel.QueryParameter{optional}, dynamic: true, params: "DoParams", parameterFields: []string{"Status"}, row: "DoRow"},
		{name: "a stream with no columns", command: sqlmodel.QueryCommandStream, params: "", parameterFields: nil, row: "DoRow"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			query := queryOf("Do", test.command, test.parameters, test.columns)
			query.IsDynamic = test.dynamic

			naming := plannedQuery(t, query, emitkit.NameStyle{SQLConstantSuffix: "SQL"})

			require.Equal(t, test.params, naming.Params)
			require.Equal(t, test.parameterFields, naming.ParameterFields)
			require.Equal(t, test.row, naming.Row)
			require.Len(t, naming.ColumnFields, map[bool]int{true: len(test.columns), false: 0}[test.row != ""])
			require.Empty(t, naming.Builder)
		})
	}
}

func TestQueryPlanNamesARuntimeBuilder(t *testing.T) {
	t.Parallel()

	query := queryOf("SearchPosts", sqlmodel.QueryCommandMany, []sqlmodel.QueryParameter{parameterNamed("author")},
		[]sqlmodel.OutputColumn{columnNamed("title")})
	query.DynamicRuntime = true
	query.CountTemplate = &sqlmodel.SQLTemplate{}

	naming := plannedQuery(t, query, emitkit.NameStyle{SQLConstantSuffix: ""})

	require.Equal(t, emit.QueryNaming{
		Method:          "SearchPosts",
		SQLConstant:     "searchPosts",
		CountConstant:   "searchPostsCountSQL",
		Label:           "SearchPosts",
		Row:             "SearchPostsRow",
		Builder:         "SearchPostsBuilder",
		AllowedColumns:  "searchPostsAllowedColumns",
		ParameterFields: []string{"author"},
		ColumnFields:    []string{"Title"},
	}, naming)
}

func TestQueryPlanLabelsTheQueryInPascalCase(t *testing.T) {
	t.Parallel()

	naming := plannedQuery(t, queryOf("get_user_by_id", sqlmodel.QueryCommandExec, nil, nil), emitkit.NameStyle{})

	require.Equal(t, "GetUserByID", naming.Label)
	require.Equal(t, "GetUserByID", naming.Method)
}

func TestQueryPlanNamesTheMethodOfASnakeCaseQueryInPascalCase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		query  string
		method string
		row    string
	}{
		{name: "a snake case name joins its words", query: "get_report_by_id", method: "GetReportByID", row: "GetReportByIDRow"},
		{name: "an upper snake case name joins its words", query: "LIST_ACTIVE_USERS", method: "ListActiveUsers", row: "ListActiveUsersRow"},
		{name: "a mixed name keeps the case inside a word", query: "listAll_byOwner", method: "ListAllByOwner", row: "ListAllByOwnerRow"},
		{name: "a camel case name keeps its case", query: "getUserByID", method: "GetUserByID", row: "GetUserByIDRow"},
		{name: "a pascal case name stays as it is", query: "GetUserById", method: "GetUserById", row: "GetUserByIdRow"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			naming := plannedQuery(t, queryOf(testCase.query, sqlmodel.QueryCommandMany, nil, []sqlmodel.OutputColumn{columnNamed("title")}),
				emitkit.NameStyle{})

			require.Equal(t, testCase.method, naming.Method)
			require.Equal(t, testCase.row, naming.Row)
		})
	}
}

func TestMethodNameIsTheNameAQueryPlansItsMethodUnder(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		query string
	}{
		{name: "a snake case name", query: "get_report_by_id"},
		{name: "an upper snake case name", query: "LIST_ACTIVE_USERS"},
		{name: "a mixed name", query: "listAll_byOwner"},
		{name: "a camel case name", query: "getUserByID"},
		{name: "a pascal case name", query: "GetUserById"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			method, err := emitkit.MethodName(testCase.query)
			require.NoError(t, err)
			naming := plannedQuery(t, queryOf(testCase.query, sqlmodel.QueryCommandExec, nil, nil), emitkit.NameStyle{})
			require.Equal(t, naming.Method, method)
		})
	}
}

func TestMethodNameRejectsANameThatCannotBeExported(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", "_hidden", "1st", "get-user"} {
		_, err := emitkit.MethodName(name)
		require.Error(t, err, name)
	}
}
