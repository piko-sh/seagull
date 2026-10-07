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

package postgres

import (
	"cmp"
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	columnQueryColumns = []string{
		"attname", "attnotnull", "has_default", "is_identity", "attgenerated",
		"typname", "nspname", "typtype", "dimensions", "format_type",
	}
	compositeQueryColumns = []string{"nspname", "typname", "attname", "typname", "nspname", "typtype", "dimensions", "format_type"}
	domainQueryColumns    = []string{
		"nspname", "typname", "typnotnull", "typname", "nspname", "typtype", "dimensions", "format_type",
	}
	errFakePermissionDenied = errors.New("permission denied")
)

var fakeRelationColumns = map[string][]fakeColumn{
	"users": {
		withFlags(builtinColumn("id", "int4", "integer"), true, false, true),
		withFlags(builtinColumn("email", "varchar", "character varying(255)"), true, false, false),
		withFlags(builtinColumn("price", "numeric", "numeric(10,2)"), false, true, false),
		builtinColumn("seen", "timestamptz", "timestamp(3) with time zone"),
		withGenerated(builtinColumn("doubled", "int4", "integer"), "s"),
		withGenerated(builtinColumn("halved", "int4", "integer"), "v"),
		withDimensions(builtinColumn("tags", "text", "text[]"), 1),
		withDimensions(builtinColumn("grid", "int4", "integer[]"), 2),
		userTypeColumn("mood", "mood", "e", 0),
		userTypeColumn("moods", "mood", "e", 1),
		userTypeColumn("contact", "email", "d", 0),
		userTypeColumn("contacts", "email", "d", 1),
		userTypeColumn("aliases", "tag_list", "d", 0),
		userTypeColumn("nested", "nested_email", "d", 0),
		userTypeColumn("looped", "loop", "d", 0),
		userTypeColumn("home", "address", "c", 0),
		userTypeColumn("unknown_domain", "missing", "d", 0),
	},
	"events": {
		withFlags(builtinColumn("id", "int8", "bigint"), true, false, false),
		withFlags(builtinColumn("at", "date", "date"), true, false, false),
	},
	"remote_users": {
		builtinColumn("id", "int4", "integer"),
		builtinColumn("name", "text", "text"),
	},
	"user_emails": {
		builtinColumn("id", "int4", "integer"),
		builtinColumn("email", "varchar", "character varying(255)"),
	},
	"user_counts": {
		builtinColumn("total", "int8", "bigint"),
	},
	"notes": {
		withFlags(builtinColumn("id", "int8", "bigint"), true, false, true),
		builtinColumn("body", "text", "text"),
	},
	"tables": {
		builtinColumn("table_name", "name", "name"),
	},
}

var fakeRelations = []fakeRelation{
	{schema: "app", name: "users", kind: "r"},
	{schema: "app", name: "events", kind: "p"},
	{schema: "app", name: "events_2024", kind: "r", partitionOf: "events"},
	{schema: "app", name: "remote_users", kind: "f"},
	{schema: "app", name: "buffer_stats", kind: "r", extensionOwned: true},
	{schema: "app", name: "user_emails", kind: "v"},
	{schema: "app", name: "user_counts", kind: "m"},
	{schema: "app", name: "extension_view", kind: "v", extensionOwned: true},
	{schema: "app", name: "users_id_seq", kind: "S"},
	{schema: "public", name: "notes", kind: "r"},
	{schema: "information_schema", name: "tables", kind: "v"},
}

var catalogueHandlers = []fakeQueryHandler{
	{
		marker: "ORDER BY covered.nspname",
		respondToQuery: scopedSchemas(
			fakeSchema{name: "_timescaledb_internal"},
			fakeSchema{name: "app"},
			fakeSchema{name: "crdb_internal"},
			fakeSchema{name: "information_schema"},
			fakeSchema{name: "pg_catalog"},
			fakeSchema{name: "pg_toast_temp_1"},
			fakeSchema{name: "public"},
			fakeSchema{name: "timescaledb_information", extensionOwned: true},
		),
	},
	{
		marker: "domain_type.typtype",
		respond: fixedResult(domainQueryColumns,
			[]driver.Value{"app", "email", true, "varchar", "pg_catalog", "b", int64(0), "character varying(100)"},
			[]driver.Value{"app", "loop", false, "loop", "app", "d", int64(0), "app.loop"},
			[]driver.Value{"app", "nested_email", false, "email", "app", "d", int64(0), "app.email"},
			[]driver.Value{"app", "tag_list", false, "text", "pg_catalog", "b", int64(1), "text[]"},
		),
	},
	{
		marker: "relkind IN ('r'",
		respondToQuery: scopedRelations([]string{"nspname", "relname", "nspname", "relname"}, func(relation fakeRelation) []driver.Value {
			if relation.partitionOf == "" {
				return []driver.Value{relation.schema, relation.name, "", ""}
			}
			return []driver.Value{relation.schema, relation.name, relation.schema, relation.partitionOf}
		}, fakeRelations...),
	},
	{
		marker:         "pg_attrdef",
		respondToQuery: columnsResponder,
	},
	{
		marker:  "column_name = 'attishidden'",
		respond: fixedResult([]string{"count"}, []driver.Value{int64(0)}),
	},
	{
		marker: "FROM pg_constraint con",
		respond: fixedResult([]string{"nspname", "relname", "conname", "contype", "attname", "ord"},
			[]driver.Value{"app", "users", "users_email_key", "u", "email", int64(1)},
			[]driver.Value{"app", "users", "users_pkey", "p", "id", int64(1)},
			[]driver.Value{"app", "buffer_stats", "buffer_stats_pkey", "p", "id", int64(1)},
			[]driver.Value{"public", "notes", "notes_pkey", "p", "id", int64(1)},
		),
	},
	{
		marker: "FROM pg_index ix",
		respond: fixedResult([]string{"nspname", "relname", "index_name", "indisunique", "indisprimary", "attname", "column_position"},
			[]driver.Value{"app", "users", "users_email_key", true, false, "email", int64(1)},
			[]driver.Value{"app", "users", "users_pkey", true, true, "id", int64(1)},
			[]driver.Value{"app", "users", "users_seen_price", false, false, "seen", int64(1)},
			[]driver.Value{"app", "users", "users_seen_price", false, false, "price", int64(2)},
			[]driver.Value{"public", "notes", "notes_pkey", true, true, "id", int64(1)},
		),
	},
	{
		marker: "relkind IN ('v'",
		respondToQuery: scopedRelations([]string{"nspname", "relname", "pg_get_viewdef"}, func(relation fakeRelation) []driver.Value {
			return []driver.Value{relation.schema, relation.name, " SELECT * FROM " + relation.schema + "." + relation.name + "_source;"}
		}, fakeRelations...),
	},
	{
		marker: "JOIN pg_enum e",
		respond: fixedResult([]string{"nspname", "typname", "enumlabel"},
			[]driver.Value{"app", "mood", "sad"},
			[]driver.Value{"app", "mood", "ok"},
			[]driver.Value{"app", "mood", "happy"},
			[]driver.Value{"public", "weather", "dry"},
		),
	},
	{
		marker:         "c.relkind = 'c'",
		respondToQuery: compositesResponder,
	},
	{
		marker: "FROM pg_proc p",
		respond: fixedResult(functionParameterColumns,
			[]driver.Value{"app", int64(10), "add", "f", true, false, false, int64(1), "integer", "i", "a", "integer"},
			[]driver.Value{"app", int64(10), "add", "f", true, false, false, int64(1), "integer", "i", "b", "integer"},
			[]driver.Value{"app", int64(11), "split_name", "f", false, false, false, int64(0), "record", "i", "full_name", "text"},
			[]driver.Value{"app", int64(11), "split_name", "f", false, false, false, int64(0), "record", "o", "first_name", "text"},
			[]driver.Value{"public", int64(12), "now_utc", "f", false, false, false, int64(0), "timestamp", nil, nil, nil},
		),
	},
	{
		marker:  "FROM pg_extension",
		respond: fixedResult([]string{"extname"}, []driver.Value{"pgcrypto"}),
	},
}

type fakeColumn struct {
	name       string
	generated  string
	typeName   string
	typeSchema string
	typeKind   string
	formatted  string
	dimensions int64
	notNull    bool
	hasDefault bool
	isIdentity bool
}

type fakeCompositeField struct {
	typeName       string
	relationKind   string
	column         fakeColumn
	extensionOwned bool
}

func (column fakeColumn) values() []driver.Value {
	return []driver.Value{
		column.name, column.notNull, column.hasDefault, column.isIdentity, column.generated,
		column.typeName, column.typeSchema, column.typeKind, column.dimensions, column.formatted,
	}
}

func builtinColumn(name string, typeName string, formatted string) fakeColumn {
	return fakeColumn{name: name, typeName: typeName, typeSchema: "pg_catalog", typeKind: "b", formatted: formatted}
}

func userTypeColumn(name string, typeName string, kind string, dimensions int64) fakeColumn {
	formatted := "app." + typeName
	for range dimensions {
		formatted += "[]"
	}
	return fakeColumn{name: name, typeName: typeName, typeSchema: "app", typeKind: kind, dimensions: dimensions, formatted: formatted}
}

func withFlags(column fakeColumn, notNull bool, hasDefault bool, isIdentity bool) fakeColumn {
	column.notNull = notNull
	column.hasDefault = hasDefault
	column.isIdentity = isIdentity
	return column
}

func withDimensions(column fakeColumn, dimensions int64) fakeColumn {
	column.dimensions = dimensions
	return column
}

func withGenerated(column fakeColumn, generated string) fakeColumn {
	column.generated = generated
	return column
}

func columnsResponder(query string, _ []driver.NamedValue) fakeQueryResult {
	result := fakeQueryResult{columns: append([]string{"nspname", "relname"}, columnQueryColumns...)}
	relations := slices.Clone(fakeRelations)
	slices.SortFunc(relations, func(left, right fakeRelation) int {
		return cmp.Or(strings.Compare(left.schema, right.schema), strings.Compare(left.name, right.name))
	})
	for _, relation := range relations {
		if relation.extensionOwned || !strings.Contains(query, "'"+relation.kind+"'") {
			continue
		}
		for _, column := range fakeRelationColumns[relation.name] {
			result.rows = append(result.rows, append([]driver.Value{relation.schema, relation.name}, column.values()...))
		}
	}
	return result
}

func compositesResponder(query string, _ []driver.NamedValue) fakeQueryResult {
	result := fakeQueryResult{columns: compositeQueryColumns}
	fields := []fakeCompositeField{
		{typeName: "address", relationKind: "c", column: builtinColumn("street", "text", "text")},
		{typeName: "address", relationKind: "c", column: builtinColumn("postcode", "varchar", "character varying(10)")},
		{typeName: "address", relationKind: "c", column: withDimensions(builtinColumn("tags", "text", "text[]"), 1)},
		{typeName: "address", relationKind: "c", column: userTypeColumn("feeling", "mood", "e", 0)},
		{typeName: "users", relationKind: "r", column: builtinColumn("id", "int4", "integer")},
		{typeName: "pair", relationKind: "c", column: builtinColumn("key", "text", "text"), extensionOwned: true},
	}
	for _, field := range fields {
		if field.relationKind != "c" && strings.Contains(query, "c.relkind = 'c'") {
			continue
		}
		if field.extensionOwned && strings.Contains(query, "deptype = 'e'") {
			continue
		}
		column := field.column
		result.rows = append(result.rows, []driver.Value{
			"app", field.typeName, column.name, column.typeName, column.typeSchema, column.typeKind, column.dimensions, column.formatted,
		})
	}
	return result
}

func overrideHandler(handlers []fakeQueryHandler, marker string, result fakeQueryResult) {
	for index := range handlers {
		if handlers[index].marker != marker {
			continue
		}
		handlers[index].respondToQuery = nil
		handlers[index].respond = func([]driver.NamedValue) fakeQueryResult { return result }
	}
}

func buildFakeCatalogue(t *testing.T, handlers []fakeQueryHandler) (*sqlmodel.Catalogue, error) {
	t.Helper()

	provider := New(openFakeDatabase(t, handlers...), modifierTypeNormaliser{})
	catalogue, sourceErrors, err := provider.BuildCatalogue(t.Context())
	if err != nil {
		assert.Nil(t, sourceErrors)
		return catalogue, err
	}
	require.Len(t, sourceErrors, 1, "the looped domain is nested too deeply to follow")
	return catalogue, err
}

func TestBuildCatalogueDescribesSchemas(t *testing.T) {
	catalogue, err := buildFakeCatalogue(t, catalogueHandlers)
	require.NoError(t, err)

	assert.Equal(t, "public", catalogue.DefaultSchema)
	assert.Contains(t, catalogue.Extensions, "pgcrypto")
	assert.ElementsMatch(t, []string{"app", "public"}, schemaNames(catalogue))
	assert.Equal(t, []string{"notes"}, mapKeys(catalogue.Schemas["public"].Tables))

	app := catalogue.Schemas["app"]
	require.NotNil(t, app)

	users := app.Tables["users"]
	require.NotNil(t, users)
	assert.Equal(t, "app", users.Schema)
	assert.Equal(t, []string{"id"}, users.PrimaryKey)
	require.Len(t, users.Constraints, 1)
	assert.Equal(t, "users_email_key", users.Constraints[0].Name)
	assert.Equal(t, sqlmodel.ConstraintUnique, users.Constraints[0].Kind)
	assert.Equal(t, []string{"email"}, users.Constraints[0].Columns)

	require.Len(t, users.Indexes, 3)
	assert.Equal(t, "users_pkey", users.Indexes[1].Name)
	assert.True(t, users.Indexes[1].IsPrimary)
	assert.Equal(t, []string{"seen", "price"}, users.Indexes[2].Columns)

	require.Contains(t, app.Enums, "mood")
	assert.Equal(t, []string{"sad", "ok", "happy"}, app.Enums["mood"].Values)

	require.Len(t, app.Functions["add"], 1)
	assert.Equal(t, 1, app.Functions["add"][0].MinArguments)
	require.Len(t, app.Functions["split_name"], 1)
	assert.Len(t, app.Functions["split_name"][0].Arguments, 1)
}

func TestBuildCatalogueScopesRelations(t *testing.T) {
	catalogue, err := buildFakeCatalogue(t, catalogueHandlers)
	require.NoError(t, err)

	app := catalogue.Schemas["app"]
	require.NotNil(t, app)

	assert.ElementsMatch(t, []string{"events", "events_2024", "remote_users", "users"}, mapKeys(app.Tables),
		"partitioned parents, partitions and foreign tables are tables; extension relations and sequences are not")
	assert.Equal(t, &sqlmodel.TableName{Schema: "app", Name: "events"}, app.Tables["events_2024"].PartitionOf)
	assert.Nil(t, app.Tables["events"].PartitionOf)
	assert.ElementsMatch(t, []string{"user_counts", "user_emails"}, mapKeys(app.Views),
		"views and materialised views are views; extension views are not")
	assert.Equal(t, " SELECT * FROM app.user_emails_source;", app.Views["user_emails"].Definition)
	assert.Len(t, app.Views["user_emails"].Columns, 2)
	assert.ElementsMatch(t, []string{"address"}, mapKeys(app.CompositeTypes),
		"row types of relations and extension composites are not composite types")
}

func TestBuildCatalogueDescribesColumns(t *testing.T) {
	catalogue, err := buildFakeCatalogue(t, catalogueHandlers)
	require.NoError(t, err)

	moodValues := []string{"sad", "ok", "happy"}
	columns := catalogue.Schemas["app"].Tables["users"].Columns
	testCases := []struct {
		name              string
		wantType          string
		wantSchema        string
		wantEnumValues    []string
		wantCategory      sqlmodel.SQLTypeCategory
		wantGeneratedKind sqlmodel.GeneratedKind
		wantDimensions    int
		wantNullable      bool
		wantHasDefault    bool
		wantGenerated     bool
	}{
		{name: "id", wantType: "int4", wantCategory: sqlmodel.TypeCategoryInteger, wantHasDefault: true},
		{name: "email", wantType: "varchar[255]", wantCategory: sqlmodel.TypeCategoryText},
		{name: "price", wantType: "numeric[10 2]", wantCategory: sqlmodel.TypeCategoryUnknown, wantNullable: true, wantHasDefault: true},
		{name: "seen", wantType: "timestamptz[3]", wantCategory: sqlmodel.TypeCategoryUnknown, wantNullable: true},
		{
			name:              "doubled",
			wantType:          "int4",
			wantCategory:      sqlmodel.TypeCategoryInteger,
			wantNullable:      true,
			wantGenerated:     true,
			wantGeneratedKind: sqlmodel.GeneratedKindStored,
		},
		{
			name:              "halved",
			wantType:          "int4",
			wantCategory:      sqlmodel.TypeCategoryInteger,
			wantNullable:      true,
			wantGenerated:     true,
			wantGeneratedKind: sqlmodel.GeneratedKindVirtual,
		},
		{name: "tags", wantType: "text", wantCategory: sqlmodel.TypeCategoryText, wantDimensions: 1, wantNullable: true},
		{name: "grid", wantType: "int4", wantCategory: sqlmodel.TypeCategoryInteger, wantDimensions: 2, wantNullable: true},
		{
			name:           "mood",
			wantType:       "mood",
			wantSchema:     "app",
			wantEnumValues: moodValues,
			wantCategory:   sqlmodel.TypeCategoryEnum,
			wantNullable:   true,
		},
		{
			name:           "moods",
			wantType:       "mood",
			wantSchema:     "app",
			wantEnumValues: moodValues,
			wantCategory:   sqlmodel.TypeCategoryEnum,
			wantDimensions: 1,
			wantNullable:   true,
		},
		{name: "contact", wantType: "varchar[100]", wantCategory: sqlmodel.TypeCategoryText},
		{name: "contacts", wantType: "varchar[100]", wantCategory: sqlmodel.TypeCategoryText, wantDimensions: 1, wantNullable: true},
		{name: "aliases", wantType: "text", wantCategory: sqlmodel.TypeCategoryText, wantDimensions: 1, wantNullable: true},
		{name: "nested", wantType: "varchar[100]", wantCategory: sqlmodel.TypeCategoryText},
		{name: "looped", wantType: "loop", wantCategory: sqlmodel.TypeCategoryUnknown, wantNullable: true},
		{name: "home", wantType: "address", wantSchema: "app", wantCategory: sqlmodel.TypeCategoryComposite, wantNullable: true},
		{name: "unknown_domain", wantType: "missing", wantCategory: sqlmodel.TypeCategoryUnknown, wantNullable: true},
	}

	require.Len(t, columns, len(testCases))
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			column := columns[index]
			assert.Equal(t, testCase.name, column.Name)
			assert.Equal(t, testCase.wantType, column.SQLType.EngineName)
			assert.Equal(t, testCase.wantCategory, column.SQLType.Category)
			assert.Equal(t, testCase.wantSchema, column.SQLType.Schema)
			assert.Equal(t, testCase.wantEnumValues, column.SQLType.EnumValues)
			assert.Equal(t, testCase.wantDimensions, column.ArrayDimensions)
			assert.Equal(t, testCase.wantDimensions > 0, column.IsArray)
			assert.Equal(t, testCase.wantNullable, column.Nullable)
			assert.Equal(t, testCase.wantHasDefault, column.HasDefault)
			assert.Equal(t, testCase.wantGenerated, column.IsGenerated)
			assert.Equal(t, testCase.wantGeneratedKind, column.GeneratedKind)
		})
	}
}

func TestBuildCatalogueDescribesCompositeFields(t *testing.T) {
	catalogue, err := buildFakeCatalogue(t, catalogueHandlers)
	require.NoError(t, err)

	address := catalogue.Schemas["app"].CompositeTypes["address"]
	require.NotNil(t, address)
	require.Len(t, address.Fields, 4)

	for _, field := range address.Fields {
		assert.Truef(t, field.Nullable, "composite field %s is nullable", field.Name)
	}
	assert.Equal(t, "varchar[10]", address.Fields[1].SQLType.EngineName)
	assert.Equal(t, 1, address.Fields[2].ArrayDimensions)
	assert.True(t, address.Fields[2].IsArray)
	assert.Equal(t, sqlmodel.TypeCategoryEnum, address.Fields[3].SQLType.Category)
	assert.Equal(t, []string{"sad", "ok", "happy"}, address.Fields[3].SQLType.EnumValues)
}

func TestBuildCatalogueReportsFailingQueries(t *testing.T) {
	testCases := []struct {
		name      string
		marker    string
		wantError string
	}{
		{name: "schemas", marker: "ORDER BY covered.nspname", wantError: "listing schemas"},
		{name: "domains", marker: "domain_type.typtype", wantError: "listing domains"},
		{name: "tables", marker: "relkind IN ('r'", wantError: "introspecting tables"},
		{name: "columns", marker: "pg_attrdef", wantError: "introspecting columns"},
		{name: "constraints", marker: "FROM pg_constraint con", wantError: "introspecting constraints"},
		{name: "indexes", marker: "FROM pg_index ix", wantError: "introspecting indexes"},
		{name: "views", marker: "relkind IN ('v'", wantError: "introspecting views"},
		{name: "enums", marker: "JOIN pg_enum e", wantError: "introspecting enums"},
		{name: "composite types", marker: "c.relkind = 'c'", wantError: "introspecting composite types"},
		{name: "functions", marker: "FROM pg_proc p", wantError: "introspecting functions"},
		{name: "hidden columns", marker: "column_name = 'attishidden'", wantError: "introspecting hidden columns"},
		{name: "extensions", marker: "FROM pg_extension", wantError: "introspecting extensions"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			handlers := slices.Clone(catalogueHandlers)
			overrideHandler(handlers, testCase.marker, fakeQueryResult{err: errFakePermissionDenied})

			catalogue, err := buildFakeCatalogue(t, handlers)
			require.ErrorIs(t, err, errFakePermissionDenied)
			assert.ErrorContains(t, err, testCase.wantError)
			assert.Nil(t, catalogue)
		})
	}
}

func TestBuildCatalogueReportsRowFailures(t *testing.T) {
	closeFailure := errors.New("connection reset while closing")
	iterationFailure := errors.New("connection reset while reading")

	testCases := []struct {
		wantError  error
		name       string
		marker     string
		columns    []string
		closeError bool
	}{
		{name: "schema iteration", marker: "ORDER BY covered.nspname", columns: []string{"nspname"}, wantError: iterationFailure},
		{name: "domain iteration", marker: "domain_type.typtype", columns: domainQueryColumns, wantError: iterationFailure},
		{name: "domain close", marker: "domain_type.typtype", columns: domainQueryColumns, closeError: true, wantError: closeFailure},
		{name: "column close", marker: "pg_attrdef", columns: columnQueryColumns, closeError: true, wantError: closeFailure},
		{name: "table iteration", marker: "relkind IN ('r'", columns: []string{"nspname", "relname", "nspname", "relname"}, wantError: iterationFailure},
		{name: "constraint iteration", marker: "FROM pg_constraint con", columns: []string{"nspname", "relname", "conname", "contype", "attname", "ord"}, wantError: iterationFailure},
		{name: "index close", marker: "FROM pg_index ix", columns: []string{"nspname", "relname", "index_name", "indisunique", "indisprimary", "attname", "column_position"}, closeError: true, wantError: closeFailure},
		{name: "view iteration", marker: "relkind IN ('v'", columns: []string{"nspname", "relname", "pg_get_viewdef"}, wantError: iterationFailure},
		{name: "enum close", marker: "JOIN pg_enum e", columns: []string{"nspname", "typname", "enumlabel"}, closeError: true, wantError: closeFailure},
		{name: "function iteration", marker: "FROM pg_proc p", columns: functionParameterColumns, wantError: iterationFailure},
		{name: "composite iteration", marker: "c.relkind = 'c'", columns: compositeQueryColumns, wantError: iterationFailure},
		{name: "extension close", marker: "FROM pg_extension", columns: []string{"extname"}, closeError: true, wantError: closeFailure},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := fakeQueryResult{columns: testCase.columns, nextError: iterationFailure}
			if testCase.closeError {
				result = fakeQueryResult{columns: testCase.columns, closeError: closeFailure}
			}
			handlers := slices.Clone(catalogueHandlers)
			overrideHandler(handlers, testCase.marker, result)

			catalogue, err := buildFakeCatalogue(t, handlers)
			require.ErrorIs(t, err, testCase.wantError)
			assert.Nil(t, catalogue)
		})
	}
}

func TestBuildCatalogueStopsWithTheCauseOfTheCancellation(t *testing.T) {
	cause := errors.New("caller gave up")
	tests := []struct {
		name   string
		marker string
		want   string
	}{
		{name: "before the schemas are listed", marker: "", want: "listing schemas: " + context.Canceled.Error() + ": caller gave up"},
		{name: "while the columns are read", marker: "pg_attrdef", want: context.Canceled.Error() + ": caller gave up"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			handlers := slices.Clone(catalogueHandlers)
			if testCase.marker == "" {
				cancel(cause)
			}
			for index := range handlers {
				if handlers[index].marker != testCase.marker {
					continue
				}
				original := handlers[index]
				handlers[index].respondToQuery = func(query string, arguments []driver.NamedValue) fakeQueryResult {
					cancel(cause)
					return original.respondToQuery(query, arguments)
				}
			}

			catalogue, _, err := New(openFakeDatabase(t, handlers...), modifierTypeNormaliser{}).BuildCatalogue(ctx)
			require.ErrorIs(t, err, context.Canceled)
			require.ErrorIs(t, err, cause)
			assert.ErrorContains(t, err, testCase.want)
			assert.Nil(t, catalogue)
		})
	}
}

func TestBuildCatalogueRejectsScanFailures(t *testing.T) {
	malformedColumn := builtinColumn("id", "int4", "integer").values()
	malformedColumn[1] = "perhaps"

	testCases := []struct {
		name    string
		marker  string
		columns []string
		row     []driver.Value
	}{
		{name: "schema", marker: "ORDER BY covered.nspname", columns: []string{"nspname"}, row: []driver.Value{nil}},
		{name: "domain", marker: "domain_type.typtype", columns: domainQueryColumns, row: []driver.Value{"app", "d", "maybe", "text", "pg_catalog", "b", int64(0), "text"}},
		{name: "table", marker: "relkind IN ('r'", columns: []string{"nspname", "relname", "nspname", "relname"}, row: []driver.Value{"app", nil, "", ""}},
		{name: "column", marker: "pg_attrdef", columns: append([]string{"nspname", "relname"}, columnQueryColumns...), row: append([]driver.Value{"app", "users"}, malformedColumn...)},
		{name: "constraint", marker: "FROM pg_constraint con", columns: []string{"nspname", "relname", "conname", "contype", "attname", "ord"}, row: []driver.Value{"app", "users", "c", "p", "id", "first"}},
		{name: "index", marker: "FROM pg_index ix", columns: []string{"nspname", "relname", "index_name", "indisunique", "indisprimary", "attname", "column_position"}, row: []driver.Value{"app", "users", "i", "maybe", false, "id", int64(1)}},
		{name: "view", marker: "relkind IN ('v'", columns: []string{"nspname", "relname", "pg_get_viewdef"}, row: []driver.Value{"app", nil, nil}},
		{name: "enum", marker: "JOIN pg_enum e", columns: []string{"nspname", "typname", "enumlabel"}, row: []driver.Value{"app", nil, "ok"}},
		{name: "composite", marker: "c.relkind = 'c'", columns: compositeQueryColumns, row: []driver.Value{"app", nil, "a", "text", "pg_catalog", "b", int64(0), "text"}},
		{name: "function", marker: "FROM pg_proc p", columns: functionParameterColumns, row: []driver.Value{"app", "not a number", "f", "f", true, false, false, int64(0), "integer", nil, nil, nil}},
		{name: "hidden column", marker: "column_name = 'attishidden'", columns: []string{"count"}, row: []driver.Value{"several"}},
		{name: "extension", marker: "FROM pg_extension", columns: []string{"extname"}, row: []driver.Value{nil}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			handlers := slices.Clone(catalogueHandlers)
			overrideHandler(handlers, testCase.marker, fakeQueryResult{columns: testCase.columns, rows: [][]driver.Value{testCase.row}})

			catalogue, err := buildFakeCatalogue(t, handlers)
			require.Error(t, err)
			assert.ErrorContains(t, err, "Scan error")
			assert.Nil(t, catalogue)
		})
	}
}

func TestBuildCatalogueNotConfigured(t *testing.T) {
	provider := New(nil, nil)

	catalogue, sourceErrors, buildError := provider.BuildCatalogue(t.Context())

	assert.Nil(t, catalogue)
	assert.Nil(t, sourceErrors)
	assert.ErrorIs(t, buildError, ErrNotConfigured)
}

func TestBuildCatalogueMarksTheColumnsTheServerHides(t *testing.T) {
	handlers := slices.Clone(catalogueHandlers)
	overrideHandler(handlers, "column_name = 'attishidden'", fakeQueryResult{columns: []string{"count"}, rows: [][]driver.Value{{int64(1)}}})
	handlers = append([]fakeQueryHandler{{
		marker: "AND a.attishidden",
		respond: fixedResult([]string{"nspname", "relname", "attname"},
			[]driver.Value{"app", "users", "email"},
			[]driver.Value{"app", "missing", "id"},
			[]driver.Value{"elsewhere", "users", "id"},
		),
	}}, handlers...)

	catalogue, err := buildFakeCatalogue(t, handlers)

	require.NoError(t, err)
	hidden := make(map[string]bool)
	for _, column := range catalogue.Schemas["app"].Tables["users"].Columns {
		hidden[column.Name] = column.Hidden
	}
	assert.True(t, hidden["email"])
	assert.False(t, hidden["id"])
}

func TestBuildCatalogueRunsEachQueryOnceHoweverManySchemasThereAre(t *testing.T) {
	tests := []struct {
		name    string
		schemas int
	}{
		{name: "two schemas", schemas: 2},
		{name: "two hundred schemas", schemas: 200},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			schemas := []fakeSchema{{name: "app"}, {name: "public"}}
			for index := range testCase.schemas - len(schemas) {
				schemas = append(schemas, fakeSchema{name: fmt.Sprintf("tenant_%03d", index)})
			}
			handlers := slices.Clone(catalogueHandlers)
			handlers[0] = fakeQueryHandler{marker: "ORDER BY covered.nspname", respondToQuery: scopedSchemas(schemas...)}
			counts := map[string]int{}
			for index := range handlers {
				original := handlers[index]
				handlers[index].respondToQuery = func(query string, arguments []driver.NamedValue) fakeQueryResult {
					counts[original.marker]++
					if original.respondToQuery != nil {
						return original.respondToQuery(query, arguments)
					}
					return original.respond(arguments)
				}
			}
			database := openFakeDatabase(t, handlers...)
			database.SetMaxOpenConns(1)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			catalogue, _, err := New(database, modifierTypeNormaliser{}).BuildCatalogue(ctx)
			require.NoError(t, err)
			require.Len(t, catalogue.Schemas, testCase.schemas)
			for _, handler := range catalogueHandlers {
				if handler.marker == "AND a.attishidden" {
					continue
				}
				assert.Equal(t, 1, counts[handler.marker], handler.marker)
			}
			require.Equal(t, []string{"id"}, catalogue.Schemas["app"].Tables["users"].PrimaryKey)
			require.Empty(t, catalogue.Schemas["app"].Tables["events"].PrimaryKey)
		})
	}
}

func TestBuildCatalogueFilesEachRowUnderItsOwnSchema(t *testing.T) {
	catalogue, err := buildFakeCatalogue(t, catalogueHandlers)
	require.NoError(t, err)

	public := catalogue.Schemas["public"]
	require.NotNil(t, public)
	notes := public.Tables["notes"]
	require.NotNil(t, notes)
	assert.Equal(t, "public", notes.Schema)
	assert.Equal(t, []string{"id", "body"}, []string{notes.Columns[0].Name, notes.Columns[1].Name})
	assert.Equal(t, []string{"id"}, notes.PrimaryKey)
	require.Len(t, notes.Indexes, 1)
	assert.Equal(t, "notes_pkey", notes.Indexes[0].Name)
	assert.Equal(t, []string{"dry"}, public.Enums["weather"].Values)
	require.Len(t, public.Functions["now_utc"], 1)
	assert.Equal(t, "public", public.Functions["now_utc"][0].Schema)
	assert.NotContains(t, catalogue.Schemas, "information_schema", "rows from a schema the catalogue does not cover are dropped")
	assert.NotContains(t, catalogue.Schemas["app"].Tables, "notes")
	assert.Empty(t, catalogue.Schemas["app"].Tables["buffer_stats"], "keys of a relation the catalogue leaves out are dropped")
}
