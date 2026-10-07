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
	"go/token"
	"maps"
	"reflect"
	"slices"
	"strings"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_clickhouse"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
)

const (
	// driverDatabaseSQL is the go.driver value for the database/sql emitter, which fits
	// every dialect.
	driverDatabaseSQL = "database/sql"

	// driverPgx is the go.driver value for the pgx emitter, which fits a dialect that runs
	// on pgx.
	driverPgx = "pgx"

	// driverClickHouse is the go.driver value for the native ClickHouse emitter, which fits
	// a dialect that runs on clickhouse-go.
	driverClickHouse = "clickhouse"

	// modulePgx is the Go module of the pgx driver.
	modulePgx = "github.com/jackc/pgx/v5"

	// moduleClickHouse is the Go module of the clickhouse-go driver.
	moduleClickHouse = "github.com/ClickHouse/clickhouse-go/v2"
)

// DialectFactory builds a dialect from the dialect_options of a database in sgl.yaml.
type DialectFactory func(options dialect_registry.Options) (dialect.Dialect, error)

// EmitterRequest describes the database a custom emitter generates code for.
type EmitterRequest struct {
	// Dialect describes the database's dialect.
	Dialect dialect.Facts

	// Prepared reports whether go.prepared is set.
	Prepared bool
}

// EmitterFactory builds the emitter for a go.driver value. The request is passed by
// pointer because it carries the whole dialect.Facts; the factory must not keep it.
type EmitterFactory func(request *EmitterRequest) (emit.EmitterPort, error)

// Factories holds the dialects and emitters a binary registers beyond the stock ones,
// keyed by the dialect: and go.driver values that select them.
//
// A registered name replaces a stock one. The zero value registers none.
type Factories struct {
	// Dialects holds the registered dialects, by dialect: value.
	Dialects map[string]DialectFactory

	// Emitters holds the registered emitters, by go.driver value.
	Emitters map[string]EmitterFactory
}

// stockDriver is one go.driver value a stock emitter serves.
type stockDriver struct {
	// build creates the emitter for a dialect.
	build func(info *dialect.Facts, prepared bool) emit.EmitterPort

	// name is the go.driver value.
	name string

	// module is the Go module of the database driver the emitter generates for, which a
	// dialect must list in Facts.Drivers, or empty when the emitter fits every dialect.
	module string

	// needs names the dialects the driver fits, for the error when it does not.
	needs string
}

// stockDrivers lists the go.driver values the stock emitters serve, in the order they are
// offered.
var stockDrivers = []stockDriver{
	{
		name:   driverDatabaseSQL,
		module: "",
		needs:  "",
		build: func(_ *dialect.Facts, prepared bool) emit.EmitterPort {
			return emit_driver_databasesql.New(emit_driver_databasesql.WithPrepared(prepared))
		},
	},
	{
		name:   driverPgx,
		module: modulePgx,
		needs:  "a dialect that runs on pgx, such as postgres",
		build:  func(*dialect.Facts, bool) emit.EmitterPort { return emit_driver_pgx.New() },
	},
	{
		name:   driverClickHouse,
		module: moduleClickHouse,
		needs:  "a dialect that runs on clickhouse-go, such as clickhouse",
		build:  func(*dialect.Facts, bool) emit.EmitterPort { return emit_driver_clickhouse.New() },
	},
}

// fits reports whether a driver serves a dialect.
//
// Takes drivers ([]string) which lists the modules of the dialect's drivers, its
// Facts.Drivers.
//
// Returns bool which is true when the driver fits every dialect or the dialect runs on
// the driver's module.
func (driver stockDriver) fits(drivers []string) bool {
	return driver.module == "" || slices.Contains(drivers, driver.module)
}

// rewordedError carries an error under a message in the configuration's own terms, so
// errors.Is and errors.As still find what it wraps.
type rewordedError struct {
	// err is the error reworded.
	err error

	// message is what Error returns.
	message string
}

// Error returns the reworded message.
//
// Returns string which is the message.
func (e *rewordedError) Error() string {
	return e.message
}

// Unwrap returns the error reworded.
//
// Returns error which is the original error.
func (e *rewordedError) Unwrap() error {
	return e.err
}

// Options assembles everything seagull.New needs for one database: its dialect and the
// options that set its emitter, preset, type overrides and custom functions.
//
// Takes configuration (*config.Config) which holds the directive prefixes.
// Takes database (*config.Database) which is the database.
// Takes factories (Factories) which holds the registered dialects and emitters.
//
// Returns []seagull.Option which configures the generator.
// Returns dialect.Dialect which is the database's dialect.
// Returns error when the dialect, the driver or a Go type in the configuration cannot be
// used.
func Options(configuration *config.Config, database *config.Database, factories Factories) ([]seagull.Option, dialect.Dialect, error) {
	built, err := Dialect(database, factories.Dialects)
	if err != nil {
		return nil, dialect.Dialect{}, err
	}
	chosenEmitter, err := emitterFor(database, new(built.Info()), factories.Emitters)
	if err != nil {
		return nil, dialect.Dialect{}, err
	}
	chosen, err := presetFor(configuration, database)
	if err != nil {
		return nil, dialect.Dialect{}, err
	}
	overrides, err := typeOverrides(database)
	if err != nil {
		return nil, dialect.Dialect{}, err
	}
	options := []seagull.Option{
		seagull.WithEmitter(chosenEmitter),
		seagull.WithPreset(chosen),
		seagull.WithTypeOverrides(overrides...),
		seagull.WithPackageNames(database.Go.Types.PackageNames),
		seagull.WithCustomFunctions(customFunctions(database)...),
	}
	return options, built, nil
}

// Dialect creates the dialect a database names, from the registered dialects first and
// the stock ones after.
//
// Takes database (*config.Database) which names the dialect and its options.
// Takes factories (map[string]DialectFactory) which holds the registered dialects, or is
// nil.
//
// Returns dialect.Dialect which is ready to use.
// Returns error when the dialect is unknown or an option does not apply to it.
func Dialect(database *config.Database, factories map[string]DialectFactory) (dialect.Dialect, error) {
	options := dialect_registry.Options{
		Returning:             database.DialectOptions.Returning,
		Name:                  "",
		MaxParseDepth:         database.DialectOptions.MaxParseDepth,
		MaxTokensPerStatement: database.DialectOptions.MaxTokensPerStatement,
		MaxTypeParseDepth:     database.DialectOptions.MaxTypeParseDepth,
		JoinUseNulls:          database.DialectOptions.JoinUseNulls,
		GroupByUseNulls:       database.DialectOptions.GroupByUseNulls,
		ANSIQuotes:            database.DialectOptions.ANSIQuotes,
		SearchPath:            database.DialectOptions.SearchPath,
	}
	if factory, found := factories[database.Dialect]; found {
		built, err := factory(options)
		if err != nil {
			return dialect.Dialect{}, fmt.Errorf("database %q: dialect %s: %w", database.Name, database.Dialect, err)
		}
		return built, nil
	}
	built, err := dialect_registry.New(database.Dialect, options)
	if err != nil {
		return dialect.Dialect{}, fmt.Errorf("database %q: %w", database.Name, configuredDialectError(database.Dialect, err, factories))
	}
	return built, nil
}

// DialectNames returns the stock dialect names together with the registered ones.
//
// Takes factories (map[string]DialectFactory) which holds the registered dialects, or is
// nil.
//
// Returns []string which holds each name once, in ascending order.
func DialectNames(factories map[string]DialectFactory) []string {
	names := map[string]bool{}
	for _, name := range dialect_registry.Names() {
		names[name] = true
	}
	for name := range factories {
		names[name] = true
	}
	return slices.Sorted(maps.Keys(names))
}

// Drivers lists the stock go.driver values that fit a dialect: database/sql always, plus
// pgx and clickhouse when Facts.Drivers lists their driver modules. Only database/sql
// takes go.prepared; see SupportsPrepared.
//
// Takes info (*dialect.Facts) which describes the dialect.
//
// Returns []string which holds the drivers, database/sql first.
func Drivers(info *dialect.Facts) []string {
	drivers := make([]string, 0, len(stockDrivers))
	for _, driver := range stockDrivers {
		if driver.fits(info.Drivers) {
			drivers = append(drivers, driver.name)
		}
	}
	return drivers
}

// SupportsPrepared reports whether a stock go.driver takes go.prepared.
//
// Takes driver (string) which is the go.driver value.
//
// Returns bool which is true for database/sql only.
func SupportsPrepared(driver string) bool {
	return driver == driverDatabaseSQL
}

// configuredDialectError rewords an error dialect_registry.New returned in the
// configuration's terms: an unknown dialect lists the dialects this binary knows, its
// registered ones included, and a refused option is named by its sgl.yaml key.
//
// Takes name (string) which is the configured dialect name.
// Takes err (error) which is dialect_registry.New's error.
// Takes factories (map[string]DialectFactory) which holds the registered dialects, or is
// nil.
//
// Returns error which wraps err.
func configuredDialectError(name string, err error, factories map[string]DialectFactory) error {
	if errors.Is(err, dialect_registry.ErrUnknownDialect) {
		return &rewordedError{err: err, message: fmt.Sprintf("unknown dialect %q; this sgl knows %s", name, strings.Join(DialectNames(factories), ", "))}
	}
	refused := dialect_registry.OptionErrors(err)
	if len(refused) == 0 {
		return err
	}
	descriptions := make([]string, len(refused))
	for index, optionError := range refused {
		descriptions[index] = optionError.Describe(dialectOptionKey(optionError.Option))
	}
	return &rewordedError{err: err, message: strings.Join(descriptions, "; ")}
}

// dialectOptionKey returns the sgl.yaml key that sets a dialect_registry.Options field:
// the dialect_options key of the config.DialectOptions field of the same name.
//
// Takes field (string) which names the dialect_registry.Options field.
//
// Returns string which is the key, such as dialect_options.returning, or the field's name
// when the configuration has no such field.
func dialectOptionKey(field string) string {
	configured, found := reflect.TypeFor[config.DialectOptions]().FieldByName(field)
	if !found {
		return field
	}
	key, _, _ := strings.Cut(configured.Tag.Get("yaml"), ",")
	return "dialect_options." + key
}

// emitterFor creates the emitter a database's go.driver names, from the registered
// emitters first and the stock ones after. Stock drivers are matched against the driver
// modules listed in the dialect's Facts.Drivers.
//
// Takes database (*config.Database) which names the driver.
// Takes info (*dialect.Facts) which describes the database's dialect.
// Takes factories (map[string]EmitterFactory) which holds the registered emitters, or is
// nil.
//
// Returns emit.EmitterPort which generates the Go code.
// Returns error when a registered factory fails, or a stock driver is unknown, does not
// fit the dialect, or is asked to prepare statements it cannot: a driver other than
// database/sql, or a dialect whose database/sql driver prepares only some statements.
func emitterFor(database *config.Database, info *dialect.Facts, factories map[string]EmitterFactory) (emit.EmitterPort, error) {
	if factory, found := factories[database.Go.Driver]; found {
		built, err := factory(&EmitterRequest{Dialect: *info, Prepared: database.Go.Prepared})
		if err != nil {
			return nil, fmt.Errorf("database %q: go.driver %s: %w", database.Name, database.Go.Driver, err)
		}
		return built, nil
	}
	if database.Go.Prepared && !SupportsPrepared(database.Go.Driver) {
		return nil, fmt.Errorf("database %q: go.prepared applies only to the %s driver, not %s", database.Name, driverDatabaseSQL, database.Go.Driver)
	}
	if database.Go.Prepared && !emit_driver_databasesql.PreparesStatements(info, "") {
		return nil, fmt.Errorf("database %q: go.prepared needs a driver that prepares every query, and the %s driver of %s prepares only some statements; leave go.prepared out",
			database.Name, driverDatabaseSQL, database.Dialect)
	}
	index := slices.IndexFunc(stockDrivers, func(driver stockDriver) bool { return driver.name == database.Go.Driver })
	if index < 0 {
		return nil, fmt.Errorf("database %q: unknown go.driver %q; use %s", database.Name, database.Go.Driver, stockDriverList())
	}
	driver := stockDrivers[index]
	if !driver.fits(info.Drivers) {
		return nil, fmt.Errorf("database %q: go.driver %s needs %s, not %s", database.Name, driver.name, driver.needs, database.Dialect)
	}
	return driver.build(info, database.Go.Prepared), nil
}

// presetFor derives the generator's preset from the configuration.
//
// Takes configuration (*config.Config) which holds the directive prefixes; with none, the
// default preset's prefixes are kept. A default foreign prefix the configuration lists is
// accepted like any other and is no longer foreign.
// Takes database (*config.Database) which holds the type slots and query names.
//
// Returns seagull.Preset which is the preset.
// Returns error when a type slot is malformed.
func presetFor(configuration *config.Config, database *config.Database) (seagull.Preset, error) {
	chosen := seagull.DefaultPreset()
	if prefixes := configuration.Directives.Prefixes; len(prefixes) > 0 {
		chosen.Directives.Canonical = prefixes[0]
		chosen.Directives.Aliases = slices.Clone(prefixes[1:])
		chosen.Directives.Foreign = slices.DeleteFunc(chosen.Directives.Foreign, func(foreign string) bool {
			return slices.Contains(prefixes, foreign)
		})
	}
	chosen.Directives.RequireCanonical = configuration.Directives.RequireCanonical
	chosen.NonNullElements = database.Go.Types.NonNullElements
	chosen.KeepStars = database.Go.KeepStars
	chosen.EmptySlices = database.Go.EmptySlices
	chosen.DocComments = database.Go.DocComments
	chosen.ReuseModels = database.Go.ReuseModels
	chosen.WrapErrors = database.Go.WrapErrors
	chosen.StructTags = maps.Clone(database.Go.StructTags)
	chosen.Interface.Enabled = database.Go.Interface.Enabled
	if name := database.Go.Interface.Name; name != "" {
		chosen.Interface.Name = name
	}
	chosen.JSONTags = seagull.JSONTags{
		Case:      seagull.JSONCase(database.Go.JSONTags.Case),
		Omit:      database.Go.JSONTags.Omit,
		OmitEmpty: database.Go.JSONTags.OmitEmpty,
	}
	chosen.QueryNames = seagull.QueryNames{
		Enabled:  database.Go.QueryNames.Enabled,
		File:     database.Go.QueryNames.File,
		Function: database.Go.QueryNames.Function,
	}

	slots := []struct {
		target *seagull.Slot
		key    string
		value  string
	}{
		{target: &chosen.Types.Decimal, key: "decimal", value: database.Go.Types.Decimal},
		{target: &chosen.Types.Money, key: "money", value: database.Go.Types.Money},
		{target: &chosen.Types.BigInt, key: "bigint", value: database.Go.Types.BigInt},
		{target: &chosen.Types.JSON, key: "json", value: database.Go.Types.JSON},
		{target: &chosen.Types.Interval, key: "interval", value: database.Go.Types.Interval},
		{target: &chosen.Types.TimeOfDay, key: "time_of_day", value: database.Go.Types.TimeOfDay},
		{target: &chosen.Types.IPAddress, key: "ip_address", value: database.Go.Types.IPAddress},
		{target: &chosen.Types.Enum, key: "enum", value: database.Go.Types.Enum},
	}
	for _, slot := range slots {
		if slot.value == "" {
			continue
		}
		goType, err := parseGoType(slot.value)
		if err != nil {
			return chosen, fmt.Errorf("database %q: go.types.%s: %w", database.Name, slot.key, err)
		}
		*slot.target = seagull.Slot{NotNull: goType, Nullable: seagull.GoType{Composite: nil, Package: "", Name: ""}}
	}
	return chosen, nil
}

// typeOverrides converts the configured type overrides.
//
// Each Go type is only split into its package and name here. The generator checks it
// against what it can write and reports one it cannot as a Q049 diagnostic, alongside the
// rest of the run's diagnostics.
//
// Takes database (*config.Database) which holds the overrides.
//
// Returns []seagull.TypeOverride which holds them in library form.
// Returns error when a Go type names a pointer.
func typeOverrides(database *config.Database) ([]seagull.TypeOverride, error) {
	overrides := make([]seagull.TypeOverride, 0, len(database.Go.Types.Overrides))
	for index, override := range database.Go.Types.Overrides {
		goType, err := splitOverrideGoType(override.Go)
		if err != nil {
			return nil, fmt.Errorf("database %q: go.types.overrides[%d]: %w", database.Name, index, err)
		}
		overrides = append(overrides, seagull.TypeOverride{
			SQLTypeName:     override.SQL,
			Column:          override.Column,
			GoPackage:       goType.Package,
			GoName:          goType.Name,
			NonNullElements: override.NonNullElements,
			StructTags:      maps.Clone(override.StructTags),
		})
	}
	return overrides, nil
}

// customFunctions converts the configured custom functions.
//
// Takes database (*config.Database) which holds the declarations.
//
// Returns []seagull.CustomFunction which holds them in library form.
func customFunctions(database *config.Database) []seagull.CustomFunction {
	functions := make([]seagull.CustomFunction, 0, len(database.CustomFunctions))
	for _, function := range database.CustomFunctions {
		functions = append(functions, seagull.CustomFunction{
			Name:         function.Name,
			ReturnType:   function.Returns,
			Nullable:     function.Nullable,
			DataAccess:   function.DataAccess,
			Arguments:    function.Args,
			MinArguments: function.MinArgs,
			IsAggregate:  function.Aggregate,
			IsVariadic:   function.Variadic,
		})
	}
	return functions
}

// splitOverrideGoType splits a configured override's Go type at its last dot into its
// package and name, leaving any judgement of the name to the generator.
//
// Leading slice markers stay with the name, as they do in a go_type directive, so
// "[]example.com/app/x.Tag" names package "example.com/app/x" and type "[]Tag", and so do
// type arguments, which are split at the last dot before them, so
// "github.com/x/opt.Option[uuid.UUID]" names package "github.com/x/opt" and type
// "Option[uuid.UUID]". A value with no usable split is kept whole as the name.
//
// Takes spec (string) which is the configured type.
//
// Returns seagull.GoType which holds the package and name.
// Returns error when the type names a pointer.
func splitOverrideGoType(spec string) (seagull.GoType, error) {
	spec = strings.TrimSpace(spec)
	if strings.HasPrefix(spec, "*") {
		return seagull.GoType{Composite: nil, Package: "", Name: ""}, pointerGoTypeError(spec)
	}
	base := strings.TrimLeft(spec, "[]")
	markers := spec[:len(spec)-len(base)]
	head, arguments := base, ""
	if typeName, typeArguments, generic := strings.Cut(base, "["); generic && strings.HasSuffix(base, "]") {
		head, arguments = typeName, "["+typeArguments
	}
	packagePath, name, qualified := strings.CutLast(head, ".")
	if !qualified || packagePath == "" || name == "" || strings.Contains(name, "/") || strings.Count(markers, "[") != strings.Count(markers, "]") {
		return seagull.GoType{Composite: nil, Package: "", Name: spec}, nil
	}
	return seagull.GoType{Composite: nil, Package: packagePath, Name: markers + name + arguments}, nil
}

// pointerGoTypeError reports a configured Go type written as a pointer.
//
// Takes spec (string) which is the configured type.
//
// Returns error which says to name the value type instead.
func pointerGoTypeError(spec string) error {
	return fmt.Errorf("go type %q: name the value type; nullable columns become pointers automatically", spec)
}

// parseGoType splits a configured Go type, "import/path.Name" or a built-in such as
// "string", into its package and name.
//
// Takes spec (string) which is the configured type.
//
// Returns seagull.GoType which holds the package and name.
// Returns error when the type is malformed.
func parseGoType(spec string) (seagull.GoType, error) {
	spec = strings.TrimSpace(spec)
	if strings.HasPrefix(spec, "*") {
		return seagull.GoType{Composite: nil, Package: "", Name: ""}, pointerGoTypeError(spec)
	}
	if spec == "[]byte" {
		return seagull.GoType{Composite: nil, Package: "", Name: spec}, nil
	}
	packagePath, name, qualified := strings.CutLast(spec, ".")
	if !qualified || strings.Contains(name, "/") {
		if strings.Contains(spec, "/") || !token.IsIdentifier(spec) {
			return seagull.GoType{Composite: nil, Package: "", Name: ""}, fmt.Errorf("go type %q must be import/path.Name or a built-in type such as string", spec)
		}
		return seagull.GoType{Composite: nil, Package: "", Name: spec}, nil
	}
	if packagePath == "" || !token.IsExported(name) || !token.IsIdentifier(name) {
		return seagull.GoType{Composite: nil, Package: "", Name: ""}, fmt.Errorf("go type %q must be import/path.Name with an exported Name", spec)
	}
	return seagull.GoType{Composite: nil, Package: packagePath, Name: name}, nil
}

// stockDriverList names the stock go.driver values for an error message.
//
// Returns string which reads like "database/sql, pgx or clickhouse".
func stockDriverList() string {
	names := make([]string, 0, len(stockDrivers))
	for _, driver := range stockDrivers {
		names = append(names, driver.name)
	}
	last := len(names) - 1
	return strings.Join(names[:last], ", ") + " or " + names[last]
}
