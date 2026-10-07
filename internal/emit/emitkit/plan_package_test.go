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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func samplePackage() *emit.Request {
	catalogue := catalogueOf(
		tableOf("users", sqlmodel.NewColumn("id", integerType(), false), sqlmodel.NewColumn("name", textType(), false)),
		tableOf("queries", sqlmodel.NewColumn("id", integerType(), false)),
		tableOf("d_b_t_x", sqlmodel.NewColumn("id", integerType(), false)),
	)
	listUsers := queryOf("ListUsers", sqlmodel.QueryCommandMany, nil, []sqlmodel.OutputColumn{columnNamed("id"), columnNamed("name")})
	getUser := queryOf("GetUser", sqlmodel.QueryCommandOne, []sqlmodel.QueryParameter{parameterNamed("id")}, []sqlmodel.OutputColumn{columnNamed("name")})
	createUser := queryOf("CreateUser", sqlmodel.QueryCommandExec, []sqlmodel.QueryParameter{parameterNamed("name"), parameterNamed("name")}, nil)
	createUser.Filename = "writes.sql"
	context := queryOf("Context", sqlmodel.QueryCommandExec, []sqlmodel.QueryParameter{parameterNamed("rows")}, nil)
	context.Filename = "writes.sql"
	return requestOf(catalogue, listUsers, getUser, createUser, context)
}

func TestPlanPackageIsDeterministic(t *testing.T) {
	t.Parallel()

	request := samplePackage()
	style := emitkit.NameStyle{SQLConstantSuffix: "", NamedTypes: namesEnumsAndStructs}
	expected, problems := emitkit.PlanPackage(request, defaultReservation(), style)
	require.Empty(t, problems)

	original := slices.Clone(request.Queries)
	orders := [][]int{{3, 2, 1, 0}, {1, 3, 0, 2}, {2, 0, 3, 1}}
	for _, order := range orders {
		shuffled := requestOf(request.Catalogue)
		for _, index := range order {
			shuffled.Queries = append(shuffled.Queries, original[index])
		}

		actual, actualProblems := emitkit.PlanPackage(shuffled, defaultReservation(), style)

		require.Empty(t, actualProblems)
		require.Equal(t, expected.Files, actual.Files)
		require.Equal(t, expected.Imports, actual.Imports)
		require.Equal(t, expected.Models, actual.Models)
		require.Equal(t, expected.Types, actual.Types)
		for position, index := range order {
			require.Equal(t, expected.Queries[index], actual.Queries[position])
		}
	}
}

func TestPlanPackageNamesEveryPartOfThePackage(t *testing.T) {
	t.Parallel()

	naming, problems := emitkit.PlanPackage(samplePackage(), defaultReservation(), emitkit.NameStyle{SQLConstantSuffix: ""})

	require.Empty(t, problems)
	require.Equal(t, map[string]string{"queries.sql": "queries.sql.go", "writes.sql": "writes.sql.go"}, naming.Files)
	require.Equal(t, []emit.ModelNaming{
		{Schema: "public", Relation: "d_b_t_x", Type: "DBTX2", Fields: []string{"ID"}},
		{Schema: "public", Relation: "queries", Type: "Queries2", Fields: []string{"ID"}},
		{Schema: "public", Relation: "users", Type: "Users", Fields: []string{"ID", "Name"}},
	}, naming.Models)
	require.Equal(t, "listUsers", naming.Queries[0].SQLConstant)
	require.Equal(t, "ListUsersRow", naming.Queries[0].Row)
	require.Equal(t, []string{"ID", "Name"}, naming.Queries[0].ColumnFields)
	require.Equal(t, []string{"id"}, naming.Queries[1].ParameterFields)
	require.Equal(t, "CreateUserParams", naming.Queries[2].Params)
	require.Equal(t, []string{"Name", "Name2"}, naming.Queries[2].ParameterFields)
	require.Equal(t, "context2", naming.Queries[3].SQLConstant)
	require.Equal(t, []string{"rows2"}, naming.Queries[3].ParameterFields)
	require.Equal(t, "context", naming.Imports["context"])
}

func TestPlanPackageClaimsHigherPrioritiesFirst(t *testing.T) {
	t.Parallel()

	mood := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood_params")
	mood.Schema = "public"
	mood.EnumValues = []string{"calm"}
	catalogue := catalogueOf(
		tableOf("query_name", sqlmodel.NewColumn("id", integerType(), false)),
		tableOf("json", sqlmodel.NewColumn("body", sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "jsonb"), false)),
	)
	catalogue.Schemas["public"].Enums["mood_params"] = &sqlmodel.Enum{Name: "mood_params", Schema: "public", Values: []string{"calm"}}
	query := queryOf("Mood", sqlmodel.QueryCommandExec, []sqlmodel.QueryParameter{parameterNamed("a"), parameterNamed("b")}, nil)
	request := requestOf(catalogue, query)
	request.Types = engineNameResolver{"jsonb": {Name: emit.InlineJSONTypeName}}
	request.Style.QueryNames = emit.QueryNames{Enabled: true, File: "query_names.go", Function: "QueryName"}

	naming, problems := emitkit.PlanPackage(request, defaultReservation(), emitkit.NameStyle{SQLConstantSuffix: "SQL", NamedTypes: namesEnumsAndStructs})

	require.Empty(t, problems)
	require.Equal(t, "JSON2", naming.Models[0].Type)
	require.Equal(t, "QueryName2", naming.Models[1].Type)
	require.Equal(t, "MoodParams", naming.Types[0].Name)
	require.Equal(t, "MoodParams2", naming.Queries[0].Params)
}

func TestPlanPackageKeepsAJSONModelWhenThePackageDeclaresNoJSONType(t *testing.T) {
	t.Parallel()

	request := requestOf(catalogueOf(tableOf("json", sqlmodel.NewColumn("body", sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "jsonb"), false))))
	request.Types = engineNameResolver{"jsonb": {Package: "example.com/documents", Name: "Document"}}

	naming, problems := emitkit.PlanPackage(request, defaultReservation(), emitkit.NameStyle{})

	require.Empty(t, problems)
	require.Equal(t, "JSON", naming.Models[0].Type)
	require.Equal(t, "documents", naming.Imports["example.com/documents"])
}

func TestPlanPackageChoosesPackageWideImportQualifiers(t *testing.T) {
	t.Parallel()

	columns := []sqlmodel.Column{
		sqlmodel.NewColumn("note", sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "nullable_text"), true),
		sqlmodel.NewColumn("price", sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "money"), false),
		sqlmodel.NewColumn("external", sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "external_uuid"), false),
		sqlmodel.NewColumn("cursor", sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "cursor"), false),
		sqlmodel.NewColumn("handle", sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "handle"), false),
	}
	request := requestOf(catalogueOf(tableOf("things", columns...)))
	request.Types = engineNameResolver{
		"nullable_text": {Package: "gopkg.in/guregu/null.v4", Name: "String"},
		"money":         {Package: "github.com/Rhymond/go-money", Name: "Money"},
		"external_uuid": {Package: "example.com/other/uuid", Name: "UUID"},
		"cursor":        {Package: "example.com/rows", Name: "Cursor"},
		"handle":        {Package: "example.com/driver", Name: "Handle"},
	}

	naming, problems := emitkit.PlanPackage(request, defaultReservation(), emitkit.NameStyle{})

	require.Empty(t, problems)
	for importPath, qualifier := range map[string]string{
		"gopkg.in/guregu/null.v4":                           "null",
		"github.com/Rhymond/go-money":                       "money",
		"github.com/google/uuid":                            "uuid",
		"example.com/other/uuid":                            "uuid2",
		"example.com/rows":                                  "rows2",
		"database/sql/driver":                               "driver",
		"github.com/ClickHouse/clickhouse-go/v2/lib/driver": "driver2",
		"example.com/driver":                                "driver3",
		"github.com/ClickHouse/clickhouse-go/v2":            "clickhouse",
		"github.com/jackc/pgx/v5":                           "pgx",
	} {
		require.Equal(t, qualifier, naming.Imports[importPath], importPath)
	}
}

func TestPlanPackageNamesSafeGeneratedFiles(t *testing.T) {
	t.Parallel()

	filenames := []string{"users.sql", "_hidden.sql", ".dotted.sql", "devices_ios.sql", "Users.sql", "q_hidden.sql", "build_linux_amd64.sql", "a_test.sql", "reports/monthly_windows.sql"}
	queries := make([]*sqlmodel.AnalysedQuery, 0, len(filenames))
	for _, filename := range filenames {
		query := queryOf("Q"+string(rune('A'+len(queries))), sqlmodel.QueryCommandExec, nil, nil)
		query.Filename = filename
		queries = append(queries, query)
	}

	naming, problems := emitkit.PlanPackage(requestOf(nil, queries...), defaultReservation(), emitkit.NameStyle{})

	require.Empty(t, problems)
	require.Equal(t, map[string]string{
		".dotted.sql":                 "q.dotted.sql.go",
		"Users.sql":                   "Users.sql.go",
		"_hidden.sql":                 "q_hidden.sql.go",
		"a_test.sql":                  "a_test.sql.go",
		"build_linux_amd64.sql":       "build_linux_amd64_sql.sql.go",
		"devices_ios.sql":             "devices_ios_sql.sql.go",
		"q_hidden.sql":                "q_hidden2.sql.go",
		"reports/monthly_windows.sql": "reports/monthly_windows_sql.sql.go",
		"users.sql":                   "users2.sql.go",
	}, naming.Files)
}

func TestPlanPackageReportsAQueryNamesFileOrFunctionTheEmitterWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		names    emit.QueryNames
		messages []string
	}{
		{name: "a free file and function", names: emit.QueryNames{Enabled: true, File: "query_names.go", Function: "QueryName"}, messages: nil},
		{name: "a disabled file", names: emit.QueryNames{Enabled: false, File: "querier.go", Function: "New"}, messages: nil},
		{name: "the querier's file", names: emit.QueryNames{Enabled: true, File: "querier.go", Function: "QueryName"}, messages: []string{
			`the query-names file "querier.go" is a file the emitter already writes; choose another name`,
		}},
		{name: "a file differing only in case", names: emit.QueryNames{Enabled: true, File: "Models.go", Function: "QueryName"}, messages: []string{
			`the query-names file "Models.go" is a file the emitter already writes; choose another name`,
		}},
		{name: "the querier's constructor", names: emit.QueryNames{Enabled: true, File: "otel.go", Function: "New"}, messages: []string{
			`the query-names function "New" is a name the generated package already declares or imports; choose another name`,
		}},
		{name: "an import qualifier", names: emit.QueryNames{Enabled: true, File: "otel.go", Function: "fmt"}, messages: []string{
			`the query-names function "fmt" is a name the generated package already declares or imports; choose another name`,
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := requestOf(nil)
			request.Style.QueryNames = test.names

			_, problems := emitkit.PlanPackage(request, defaultReservation(), emitkit.NameStyle{})

			messages := make([]string, 0, len(problems))
			for _, problem := range problems {
				require.Nil(t, problem.Query)
				require.Equal(t, diag.SeverityError, problem.Severity)
				messages = append(messages, problem.Message)
			}
			require.ElementsMatch(t, test.messages, messages)
		})
	}
}

func TestPlanPackageReportsAQueryNameThatCannotBeExported(t *testing.T) {
	t.Parallel()

	bad := queryOf("0bad", sqlmodel.QueryCommandExec, nil, nil)
	good := queryOf("Good", sqlmodel.QueryCommandExec, nil, nil)

	naming, problems := emitkit.PlanPackage(requestOf(nil, bad, good), defaultReservation(), emitkit.NameStyle{})

	require.Len(t, problems, 1)
	require.Same(t, bad, problems[0].Query)
	require.Equal(t, diag.CodeInvalidKeywordArgumentValue, problems[0].Code)
	require.Equal(t, diag.SeverityError, problems[0].Severity)
	require.Contains(t, problems[0].Message, `query name "0bad" cannot become an exported Go identifier`)
	require.Equal(t, emit.QueryNaming{}, naming.Queries[0])
	require.Equal(t, "Good", naming.Queries[1].Method)
}

func TestPlanPackageNamesModels(t *testing.T) {
	t.Parallel()

	hidden := sqlmodel.NewColumn("rank", integerType(), false)
	hidden.Hidden = true
	catalogue := catalogueOf(tableOf("events",
		sqlmodel.NewColumn("foo_bar", integerType(), false),
		hidden,
		sqlmodel.NewColumn("foo__bar", integerType(), false),
		sqlmodel.NewColumn("2fa", integerType(), false),
	))
	catalogue.Schemas["audit"] = &sqlmodel.Schema{Name: "audit", Tables: map[string]*sqlmodel.Table{
		"events": {Name: "events", Schema: "audit", Columns: []sqlmodel.Column{sqlmodel.NewColumn("id", integerType(), false)}},
	}}

	naming, problems := emitkit.PlanPackage(requestOf(catalogue), defaultReservation(), emitkit.NameStyle{})

	require.Empty(t, problems)
	require.Equal(t, []emit.ModelNaming{
		{Schema: "audit", Relation: "events", Type: "AuditEvents", Fields: []string{"ID"}},
		{Schema: "public", Relation: "events", Type: "Events", Fields: []string{"FooBar", "", "FooBar2", "X2fa"}},
	}, naming.Models)
}

func TestPlanPackageModelsNoPartition(t *testing.T) {
	t.Parallel()

	catalogue := catalogueOf(tableOf("events", sqlmodel.NewColumn("id", integerType(), false)))
	catalogue.Schemas["public"].Tables["events_2026"] = &sqlmodel.Table{
		Name:        "events_2026",
		Schema:      "public",
		Columns:     []sqlmodel.Column{sqlmodel.NewColumn("id", integerType(), false)},
		PartitionOf: &sqlmodel.TableName{Schema: "public", Name: "events"},
	}

	naming, problems := emitkit.PlanPackage(requestOf(catalogue), defaultReservation(), emitkit.NameStyle{})

	require.Empty(t, problems)
	require.Equal(t, []emit.ModelNaming{
		{Schema: "public", Relation: "events", Type: "Events", Fields: []string{"ID"}},
	}, naming.Models)
}

func TestPlanPackageTakesTheInterfaceNameBeforeAnyModel(t *testing.T) {
	t.Parallel()

	catalogue := catalogueOf(tableOf("querier", sqlmodel.NewColumn("id", integerType(), false)))
	request := requestOf(catalogue, queryOf("ListQueriers", sqlmodel.QueryCommandMany, nil, []sqlmodel.OutputColumn{columnNamed("id")}))
	request.Style.Interface = emit.Interface{Enabled: true, Name: "Querier"}

	naming, problems := emitkit.PlanPackage(request, defaultReservation(), emitkit.NameStyle{})

	require.Empty(t, problems)
	require.Equal(t, "Querier", naming.Interface)
	require.Equal(t, "Querier2", naming.Models[0].Type)
	require.Equal(t, "ListQueriers", naming.Queries[0].Method)
}

func TestPlanPackageLeavesTheInterfaceOutUnlessTheStyleAsksForIt(t *testing.T) {
	t.Parallel()

	catalogue := catalogueOf(tableOf("querier", sqlmodel.NewColumn("id", integerType(), false)))
	request := requestOf(catalogue)
	request.Style.Interface = emit.Interface{Enabled: false, Name: "Querier"}

	naming, problems := emitkit.PlanPackage(request, defaultReservation(), emitkit.NameStyle{})

	require.Empty(t, problems)
	require.Empty(t, naming.Interface)
	require.Equal(t, "Querier", naming.Models[0].Type)
}

func TestPlanPackageReportsAnInterfaceNameThePackageAlreadyDeclares(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Queries", "DBTX", "context", "sglSplice"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			request := requestOf(nil)
			request.Style.Interface = emit.Interface{Enabled: true, Name: name}

			naming, problems := emitkit.PlanPackage(request, defaultReservation(), emitkit.NameStyle{})

			require.Len(t, problems, 1)
			require.Nil(t, problems[0].Query)
			require.Equal(t, diag.SeverityError, problems[0].Severity)
			require.Equal(t, `the interface name "`+name+`" is a name the generated package already declares or imports; choose another name`, problems[0].Message)
			require.Empty(t, naming.Interface)
		})
	}
}
