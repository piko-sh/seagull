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

package assemble

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_duckdb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mysql"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_clickhouse"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
)

var errFactoryFailed = errors.New("factory failed")

func database(dialectName, driver string) *config.Database {
	configured := &config.Database{Name: "app", Dialect: dialectName}
	configured.Go.Driver = driver
	return configured
}

func TestDrivers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		info     dialect.Facts
		expected []string
	}{
		{name: "postgres", info: dialect_engine_postgres.New().Info(), expected: []string{driverDatabaseSQL, driverPgx}},
		{name: "clickhouse", info: dialect_engine_clickhouse.New().Info(), expected: []string{driverDatabaseSQL, driverClickHouse}},
		{name: "mysql", info: dialect_engine_mysql.New().Info(), expected: []string{driverDatabaseSQL}},
		{name: "sqlite", info: dialect_engine_sqlite.New().Info(), expected: []string{driverDatabaseSQL}},
		{name: "duckdb", info: dialect_engine_duckdb.New().Info(), expected: []string{driverDatabaseSQL}},
		{name: "an engine with no listed driver", info: dialect.Facts{Family: "exotic"}, expected: []string{driverDatabaseSQL}},
		{
			name:     "an engine that runs on pgx",
			info:     dialect.Facts{Family: "exotic", Drivers: []string{"github.com/jackc/pgx/v5"}},
			expected: []string{driverDatabaseSQL, driverPgx},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, Drivers(&testCase.info))
		})
	}
}

func TestEveryOfferedDriverBuilds(t *testing.T) {
	t.Parallel()

	for _, name := range dialect_registry.Names() {
		built, err := Dialect(database(name, ""), nil)
		require.NoError(t, err, name)
		info := built.Info()
		for _, driver := range Drivers(&info) {
			emitter, err := emitterFor(database(name, driver), &info, nil)
			require.NoError(t, err, "%s with %s", name, driver)
			require.NotNil(t, emitter, "%s with %s", name, driver)
		}
	}
}

func TestSupportsPrepared(t *testing.T) {
	t.Parallel()

	require.True(t, SupportsPrepared(driverDatabaseSQL))
	require.False(t, SupportsPrepared(driverPgx))
	require.False(t, SupportsPrepared(driverClickHouse))
	require.False(t, SupportsPrepared("recording"))
}

func TestDialect(t *testing.T) {
	t.Parallel()

	built, err := Dialect(database("sqlite", ""), nil)
	require.NoError(t, err)
	require.Equal(t, dialect_engine_sqlite.Family, built.Info().Family)

	factories := map[string]DialectFactory{
		"lite": func(dialect_registry.Options) (dialect.Dialect, error) {
			return dialect_engine_sqlite.New(dialect_engine_sqlite.WithName("lite")), nil
		},
		"down": func(dialect_registry.Options) (dialect.Dialect, error) {
			return dialect.Dialect{}, errFactoryFailed
		},
	}
	built, err = Dialect(database("lite", ""), factories)
	require.NoError(t, err)
	require.Equal(t, "lite", built.Info().Name)

	_, err = Dialect(database("down", ""), factories)
	require.ErrorIs(t, err, errFactoryFailed)
	require.EqualError(t, err, `database "app": dialect down: factory failed`)

	_, err = Dialect(database("oracle", ""), factories)
	require.ErrorIs(t, err, dialect_registry.ErrUnknownDialect)
	require.ErrorContains(t, err, `database "app": `)
	require.ErrorContains(t, err, "; this sgl knows clickhouse, cockroachdb, down, duckdb, lite, mariadb, mysql, postgres, sqlite, timescaledb")

	configured := database("sqlite", "")
	configured.DialectOptions.MaxParseDepth = -1
	_, err = Dialect(configured, nil)
	require.ErrorIs(t, err, dialect_registry.ErrInvalidOption)
}

func TestDialectNamesTheConfigurationKeyOfEachOptionItRefuses(t *testing.T) {
	t.Parallel()

	enabled := true
	negative := database("postgres", "")
	negative.DialectOptions.MaxParseDepth = -1
	wrongDialect := database("postgres", "")
	wrongDialect.DialectOptions.Returning = &enabled
	several := database("sqlite", "")
	several.DialectOptions.ANSIQuotes = &enabled
	several.DialectOptions.SearchPath = []string{"shop"}
	several.DialectOptions.MaxTypeParseDepth = 3
	testCases := []struct {
		database *config.Database
		name     string
		message  string
	}{
		{name: "a negative limit", database: negative,
			message: `database "app": dialect_options.max_parse_depth must not be negative, got -1`},
		{name: "an option of another dialect", database: wrongDialect,
			message: `database "app": dialect_options.returning applies to mysql and mariadb, not postgres`},
		{name: "several options", database: several,
			message: `database "app": dialect_options.max_type_parse_depth applies to clickhouse, not sqlite; ` +
				`dialect_options.ansi_quotes applies to mysql and mariadb, not sqlite; ` +
				`dialect_options.search_path applies to postgres, cockroachdb and timescaledb, not sqlite`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := Dialect(testCase.database, nil)
			require.EqualError(t, err, testCase.message)
			require.ErrorIs(t, err, dialect_registry.ErrInvalidOption)
		})
	}
}

func TestEveryDialectOptionHasAConfigurationKey(t *testing.T) {
	t.Parallel()

	configured := reflect.TypeFor[config.DialectOptions]()
	options := reflect.TypeFor[dialect_registry.Options]()
	for field := range options.Fields() {
		if field.Name == "Name" {
			continue
		}
		counterpart, found := configured.FieldByName(field.Name)
		require.True(t, found, "dialects.Options.%s has no config.DialectOptions field", field.Name)
		require.NotEmpty(t, counterpart.Tag.Get("yaml"), "config.DialectOptions.%s has no yaml key", field.Name)
	}
}

func TestDialectNamesTheDialectsItKnowsForAnUnknownOne(t *testing.T) {
	t.Parallel()

	_, err := Dialect(database("postgress", ""), nil)
	require.ErrorIs(t, err, dialect_registry.ErrUnknownDialect)
	require.EqualError(t, err, `database "app": unknown dialect "postgress"; this sgl knows `+
		`clickhouse, cockroachdb, duckdb, mariadb, mysql, postgres, sqlite, timescaledb`)
}

func TestDialectPassesTheServerSettings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		groupByUseNulls *bool
		joinUseNulls    *bool
		ansiQuotes      *bool
		name            string
	}{
		{name: "unset", groupByUseNulls: nil, joinUseNulls: nil, ansiQuotes: nil},
		{name: "group_by_use_nulls", groupByUseNulls: new(true), joinUseNulls: nil, ansiQuotes: nil},
		{name: "join_use_nulls", groupByUseNulls: nil, joinUseNulls: new(false), ansiQuotes: nil},
		{name: "ansi_quotes", groupByUseNulls: nil, joinUseNulls: nil, ansiQuotes: new(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var received dialect_registry.Options
			factories := map[string]DialectFactory{
				"recording": func(options dialect_registry.Options) (dialect.Dialect, error) {
					received = options
					return dialect_engine_sqlite.New(), nil
				},
			}
			configured := database("recording", "")
			configured.DialectOptions.GroupByUseNulls = tc.groupByUseNulls
			configured.DialectOptions.JoinUseNulls = tc.joinUseNulls
			configured.DialectOptions.ANSIQuotes = tc.ansiQuotes
			_, err := Dialect(configured, factories)
			require.NoError(t, err)
			require.Equal(t, tc.groupByUseNulls, received.GroupByUseNulls)
			require.Equal(t, tc.joinUseNulls, received.JoinUseNulls)
			require.Equal(t, tc.ansiQuotes, received.ANSIQuotes)
		})
	}
}

func TestDialectPassesTheSearchPath(t *testing.T) {
	t.Parallel()

	var received dialect_registry.Options
	factories := map[string]DialectFactory{
		"recording": func(options dialect_registry.Options) (dialect.Dialect, error) {
			received = options
			return dialect_engine_sqlite.New(), nil
		},
	}
	configured := database("recording", "")
	configured.DialectOptions.SearchPath = []string{"auth", "public"}

	_, err := Dialect(configured, factories)

	require.NoError(t, err)
	require.Equal(t, []string{"auth", "public"}, received.SearchPath)
}

func TestDialectNames(t *testing.T) {
	t.Parallel()

	require.Equal(t, dialect_registry.Names(), DialectNames(nil))
	names := DialectNames(map[string]DialectFactory{"sqlite": nil, "aardvark": nil})
	require.Equal(t, "aardvark", names[0])
	require.Len(t, names, len(dialect_registry.Names())+1)
}

func TestEmitter(t *testing.T) {
	t.Parallel()

	postgresInfo := dialect_engine_postgres.New().Info()
	clickhouseInfo := dialect_engine_clickhouse.New().Info()
	sqliteInfo := dialect_engine_sqlite.New().Info()

	testCases := []struct {
		info     dialect.Facts
		expected emit.EmitterPort
		name     string
		dialect  string
		driver   string
		wantErr  string
		prepared bool
	}{
		{name: "database/sql", info: sqliteInfo, dialect: "sqlite", driver: driverDatabaseSQL, expected: emit_driver_databasesql.New()},
		{name: "prepared database/sql", info: sqliteInfo, dialect: "sqlite", driver: driverDatabaseSQL, prepared: true, expected: emit_driver_databasesql.New()},
		{name: "pgx", info: postgresInfo, dialect: "postgres", driver: driverPgx, expected: emit_driver_pgx.New()},
		{name: "clickhouse", info: clickhouseInfo, dialect: "clickhouse", driver: driverClickHouse, expected: emit_driver_clickhouse.New()},
		{
			name: "pgx on another family", info: sqliteInfo, dialect: "sqlite", driver: driverPgx,
			wantErr: `database "app": go.driver pgx needs a dialect that runs on pgx, such as postgres, not sqlite`,
		},
		{
			name: "clickhouse on another family", info: postgresInfo, dialect: "postgres", driver: driverClickHouse,
			wantErr: `database "app": go.driver clickhouse needs a dialect that runs on clickhouse-go, such as clickhouse, not postgres`,
		},
		{
			name: "prepared pgx", info: postgresInfo, dialect: "postgres", driver: driverPgx, prepared: true,
			wantErr: `database "app": go.prepared applies only to the database/sql driver, not pgx`,
		},
		{
			name: "prepared database/sql on a driver that prepares only some statements", info: clickhouseInfo, dialect: "clickhouse",
			driver: driverDatabaseSQL, prepared: true,
			wantErr: `database "app": go.prepared needs a driver that prepares every query, and the database/sql driver of clickhouse prepares only some statements; leave go.prepared out`,
		},
		{name: "database/sql without go.prepared on that driver", info: clickhouseInfo, dialect: "clickhouse", driver: driverDatabaseSQL, expected: emit_driver_databasesql.New()},
		{
			name: "prepared unknown driver", info: postgresInfo, dialect: "postgres", driver: "odbc", prepared: true,
			wantErr: `database "app": go.prepared applies only to the database/sql driver, not odbc`,
		},
		{
			name: "unknown driver", info: postgresInfo, dialect: "postgres", driver: "odbc",
			wantErr: `database "app": unknown go.driver "odbc"; use database/sql, pgx or clickhouse`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			configured := database(testCase.dialect, testCase.driver)
			configured.Go.Prepared = testCase.prepared
			emitter, err := emitterFor(configured, &testCase.info, nil)
			if testCase.wantErr != "" {
				require.EqualError(t, err, testCase.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, fmt.Sprintf("%T", testCase.expected), fmt.Sprintf("%T", emitter))
		})
	}
}

func TestEmitterFactories(t *testing.T) {
	t.Parallel()

	var received EmitterRequest
	custom := emit_driver_pgx.New()
	factories := map[string]EmitterFactory{
		"recording": func(request *EmitterRequest) (emit.EmitterPort, error) {
			received = *request
			return custom, nil
		},
		driverPgx: func(*EmitterRequest) (emit.EmitterPort, error) { return nil, errFactoryFailed },
	}
	info := dialect.Facts{Name: "sqlite", Family: dialect_engine_sqlite.Family}

	configured := database("sqlite", "recording")
	configured.Go.Prepared = true
	emitter, err := emitterFor(configured, &info, factories)
	require.NoError(t, err)
	require.Same(t, custom, emitter)
	require.Equal(t, EmitterRequest{Dialect: info, Prepared: true}, received)

	_, err = emitterFor(database("sqlite", driverPgx), &info, factories)
	require.ErrorIs(t, err, errFactoryFailed)
	require.EqualError(t, err, `database "app": go.driver pgx: factory failed`)
}

func TestPreset(t *testing.T) {
	t.Parallel()

	configuration := &config.Config{Directives: config.Directives{Prefixes: []string{"sgl", "app"}, RequireCanonical: true}}
	configured := database("postgres", driverDatabaseSQL)
	configured.Go.QueryNames = config.QueryNames{Enabled: true, File: "names.go", Function: "NameOf"}
	configured.Go.Types.Decimal = "github.com/shopspring/decimal.Decimal"
	configured.Go.Types.JSON = "[]byte"
	configured.Go.Types.Enum = "string"

	chosen, err := presetFor(configuration, configured)
	require.NoError(t, err)
	require.Equal(t, "sgl", chosen.Directives.Canonical)
	require.Equal(t, []string{"app"}, chosen.Directives.Aliases)
	require.True(t, chosen.Directives.RequireCanonical)
	require.Equal(t, seagull.QueryNames{Enabled: true, File: "names.go", Function: "NameOf"}, chosen.QueryNames)
	require.Equal(t, seagull.Slot{NotNull: seagull.GoType{Package: "github.com/shopspring/decimal", Name: "Decimal"}}, chosen.Types.Decimal)
	require.Equal(t, seagull.Slot{NotNull: seagull.GoType{Name: "[]byte"}}, chosen.Types.JSON)
	require.Equal(t, seagull.Slot{NotNull: seagull.GoType{Name: "string"}}, chosen.Types.Enum)
	require.Equal(t, seagull.DefaultPreset().Types.Money, chosen.Types.Money)
	require.False(t, chosen.NonNullElements)

	configured.Go.Types.NonNullElements = true
	declared, err := presetFor(configuration, configured)
	require.NoError(t, err)
	require.True(t, declared.NonNullElements)
	require.False(t, declared.KeepStars, "stars are expanded unless go.keep_stars says otherwise")

	configured.Go.KeepStars = true
	keeping, err := presetFor(configuration, configured)
	require.NoError(t, err)
	require.True(t, keeping.KeepStars)
	require.Equal(t, seagull.DefaultPreset().Interface, keeping.Interface, "the interface is off unless go.interface turns it on")
	require.Equal(t, seagull.JSONTags{}, keeping.JSONTags)
	require.Nil(t, keeping.StructTags)
	require.False(t, keeping.EmptySlices)
	require.False(t, keeping.DocComments)
	require.False(t, keeping.ReuseModels)
	require.False(t, keeping.WrapErrors, "generated errors are returned unwrapped unless go.wrap_errors says otherwise")

	configured.Go.Interface = config.Interface{Enabled: true, Name: "Store"}
	configured.Go.JSONTags = config.JSONTags{Case: "camel", Omit: false, OmitEmpty: true}
	configured.Go.StructTags = map[string]string{"db": "{name}"}
	configured.Go.EmptySlices = true
	configured.Go.DocComments = true
	configured.Go.ReuseModels = true
	configured.Go.WrapErrors = true
	shaped, err := presetFor(configuration, configured)
	require.NoError(t, err)
	require.Equal(t, seagull.Interface{Enabled: true, Name: "Store"}, shaped.Interface)
	require.Equal(t, seagull.JSONTags{Case: seagull.JSONCaseCamel, Omit: false, OmitEmpty: true}, shaped.JSONTags)
	require.Equal(t, map[string]string{"db": "{name}"}, shaped.StructTags)
	require.True(t, shaped.EmptySlices)
	require.True(t, shaped.DocComments)
	require.True(t, shaped.ReuseModels)
	require.True(t, shaped.WrapErrors)
	require.NoError(t, shaped.Validate())

	unprefixed, err := presetFor(&config.Config{}, database("postgres", ""))
	require.NoError(t, err)
	require.Equal(t, seagull.DefaultPreset().Directives.Canonical, unprefixed.Directives.Canonical)
	require.True(t, unprefixed.Types.Enum.IsZero(), "an unset enum slot keeps the generator's named enum types")

	configured.Go.Types.Money = "*string"
	_, err = presetFor(configuration, configured)
	require.ErrorContains(t, err, `database "app": go.types.money: go type "*string"`)
}

func TestPresetAcceptsAForeignPrefixListedInTheConfiguration(t *testing.T) {
	t.Parallel()

	defaults := seagull.DefaultPreset()
	require.NotEmpty(t, defaults.Directives.Foreign)
	foreign := defaults.Directives.Foreign[0]

	testCases := []struct {
		name      string
		canonical string
		prefixes  []string
		aliases   []string
	}{
		{name: "as an alias", canonical: "sgl", prefixes: []string{"sgl", "seagull", foreign}, aliases: []string{"seagull", foreign}},
		{name: "as the canonical prefix", canonical: foreign, prefixes: []string{foreign, "sgl"}, aliases: []string{"sgl"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			configuration := &config.Config{Directives: config.Directives{Prefixes: testCase.prefixes}}
			chosen, err := presetFor(configuration, database("sqlite", driverDatabaseSQL))
			require.NoError(t, err)
			require.Equal(t, testCase.canonical, chosen.Directives.Canonical)
			require.Equal(t, testCase.aliases, chosen.Directives.Aliases)
			require.NotContains(t, chosen.Directives.Foreign, foreign)
			require.NoError(t, chosen.Validate())
		})
	}

	unrelated := &config.Config{Directives: config.Directives{Prefixes: []string{"sgl", "app"}}}
	chosen, err := presetFor(unrelated, database("sqlite", driverDatabaseSQL))
	require.NoError(t, err)
	require.Equal(t, defaults.Directives.Foreign, chosen.Directives.Foreign)
	require.Equal(t, defaults.Directives.Foreign, seagull.DefaultPreset().Directives.Foreign)
}

func TestTypeOverrides(t *testing.T) {
	t.Parallel()

	configured := database("postgres", "")
	configured.Go.Types.Overrides = []config.Override{
		{SQL: "uuid", Go: "github.com/google/uuid.UUID"},
		{Column: "authors.bio", Go: "string"},
		{Column: "posts.tags", NonNullElements: new(true)},
		{Column: "users.email", StructTags: map[string]string{"validate": "email"}},
	}
	overrides, err := typeOverrides(configured)
	require.NoError(t, err)
	require.Equal(t, []seagull.TypeOverride{
		{SQLTypeName: "uuid", GoPackage: "github.com/google/uuid", GoName: "UUID"},
		{Column: "authors.bio", GoName: "string"},
		{Column: "posts.tags", NonNullElements: new(true)},
		{Column: "users.email", StructTags: map[string]string{"validate": "email"}},
	}, overrides)

	configured.Go.Types.Overrides = append(configured.Go.Types.Overrides, config.Override{SQL: "text", Go: "*x"})
	_, err = typeOverrides(configured)
	require.ErrorContains(t, err, `database "app": go.types.overrides[4]: go type "*x"`)
}

func TestTypeOverridesLeaveUnusableGoTypesToTheGenerator(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		spec     string
		expected seagull.TypeOverride
	}{
		{spec: "bad type", expected: seagull.TypeOverride{SQLTypeName: "text", GoName: "bad type"}},
		{spec: "map[string]int", expected: seagull.TypeOverride{SQLTypeName: "text", GoName: "map[string]int"}},
		{spec: "a/b", expected: seagull.TypeOverride{SQLTypeName: "text", GoName: "a/b"}},
		{spec: ".Decimal", expected: seagull.TypeOverride{SQLTypeName: "text", GoName: ".Decimal"}},
		{spec: "uuid.", expected: seagull.TypeOverride{SQLTypeName: "text", GoName: "uuid."}},
		{spec: "gopkg.in/yaml", expected: seagull.TypeOverride{SQLTypeName: "text", GoName: "gopkg.in/yaml"}},
		{spec: " example.com/menu.123 ", expected: seagull.TypeOverride{SQLTypeName: "text", GoPackage: "example.com/menu", GoName: "123"}},
		{spec: "[]example.com/app/x.Tag", expected: seagull.TypeOverride{SQLTypeName: "text", GoPackage: "example.com/app/x", GoName: "[]Tag"}},
		{spec: "[][]example.com/app/x.Tag", expected: seagull.TypeOverride{SQLTypeName: "text", GoPackage: "example.com/app/x", GoName: "[][]Tag"}},
		{spec: "[]string", expected: seagull.TypeOverride{SQLTypeName: "text", GoName: "[]string"}},
		{spec: "github.com/x/opt.Option[github.com/google/uuid.UUID]", expected: seagull.TypeOverride{SQLTypeName: "text", GoPackage: "github.com/x/opt", GoName: "Option[github.com/google/uuid.UUID]"}},
		{spec: "[]example.com/pairs.Pair[string, example.com/y.Z]", expected: seagull.TypeOverride{SQLTypeName: "text", GoPackage: "example.com/pairs", GoName: "[]Pair[string, example.com/y.Z]"}},
		{spec: "uuid.UUID", expected: seagull.TypeOverride{SQLTypeName: "text", GoPackage: "uuid", GoName: "UUID"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.spec, func(t *testing.T) {
			t.Parallel()

			configured := database("postgres", "")
			configured.Go.Types.Overrides = []config.Override{{SQL: "text", Go: testCase.spec}}
			overrides, err := typeOverrides(configured)
			require.NoError(t, err)
			require.Equal(t, []seagull.TypeOverride{testCase.expected}, overrides)
		})
	}
}

func TestCustomFunctions(t *testing.T) {
	t.Parallel()

	configured := database("postgres", "")
	require.Empty(t, customFunctions(configured))

	configured.CustomFunctions = []config.CustomFunction{{
		Name: "similarity", Returns: "float4", Nullable: "never", Args: []string{"text", "text"},
		MinArgs: 2, Aggregate: true, Variadic: true,
	}}
	require.Equal(t, []seagull.CustomFunction{{
		Name: "similarity", ReturnType: "float4", Nullable: "never", Arguments: []string{"text", "text"},
		MinArguments: 2, IsAggregate: true, IsVariadic: true,
	}}, customFunctions(configured))
}

func TestParseGoType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		spec     string
		expected seagull.GoType
		wantErr  bool
	}{
		{spec: "string", expected: seagull.GoType{Name: "string"}},
		{spec: "[]byte", expected: seagull.GoType{Name: "[]byte"}},
		{spec: " time.Time ", expected: seagull.GoType{Package: "time", Name: "Time"}},
		{spec: "github.com/cockroachdb/apd/v3.Decimal", expected: seagull.GoType{Package: "github.com/cockroachdb/apd/v3", Name: "Decimal"}},
		{spec: "*string", wantErr: true},
		{spec: "a/b", wantErr: true},
		{spec: "1abc", wantErr: true},
		{spec: ".Decimal", wantErr: true},
		{spec: "example.com/x.lower", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.spec, func(t *testing.T) {
			t.Parallel()

			goType, err := parseGoType(testCase.spec)
			if testCase.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.expected, goType)
		})
	}
}

func TestOptions(t *testing.T) {
	t.Parallel()

	configuration := &config.Config{Directives: config.Directives{Prefixes: []string{"sgl"}}}
	options, built, err := Options(configuration, database("sqlite", driverDatabaseSQL), Factories{})
	require.NoError(t, err)
	require.Len(t, options, 5)
	require.Equal(t, dialect_engine_sqlite.Family, built.Info().Family)

	generator, err := seagull.New(built, options...)
	require.NoError(t, err)
	require.NotNil(t, generator)

	broken := []struct {
		name     string
		database *config.Database
	}{
		{name: "unknown dialect", database: database("oracle", driverDatabaseSQL)},
		{name: "unfit driver", database: database("sqlite", driverPgx)},
		{name: "malformed slot", database: func() *config.Database {
			configured := database("sqlite", driverDatabaseSQL)
			configured.Go.Types.BigInt = "*big.Int"
			return configured
		}()},
		{name: "pointer override", database: func() *config.Database {
			configured := database("sqlite", driverDatabaseSQL)
			configured.Go.Types.Overrides = []config.Override{{SQL: "text", Go: "*string"}}
			return configured
		}()},
	}
	for _, testCase := range broken {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			options, built, err := Options(configuration, testCase.database, Factories{})
			require.Error(t, err)
			require.Nil(t, options)
			require.Zero(t, built)
		})
	}
}
