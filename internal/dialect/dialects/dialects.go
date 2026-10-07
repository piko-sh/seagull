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

package dialects

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/cockroachdb"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/mariadb"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// ErrUnknownDialect is returned by New for a name that is not a built-in dialect. It
	// wraps the generator's invalid-configuration sentinel.
	ErrUnknownDialect = fmt.Errorf("unknown dialect: %w", fault.ErrInvalidConfig)

	// ErrInvalidOption is returned by New when a limit is negative or above its ceiling, the
	// name is blank or another family's built-in name, or an option does not apply to the
	// named dialect. It wraps the generator's invalid-configuration sentinel.
	ErrInvalidOption = fmt.Errorf("invalid dialect option: %w", fault.ErrInvalidConfig)

	// builtins lists each built-in dialect New builds, keyed by name: its constructor and
	// the family it reports.
	builtins = map[string]builtinDialect{
		"postgres": {family: postgres.Family,
			construct: func(options Options) dialect.DialectPort { return postgres.New(postgresOptions(options)...) }},
		"cockroachdb": {family: postgres.Family,
			construct: func(options Options) dialect.DialectPort { return cockroachdb.New(postgresOptions(options)...) }},
		"timescaledb": {family: postgres.Family,
			construct: func(options Options) dialect.DialectPort { return timescaledb.New(postgresOptions(options)...) }},
		"mysql": {family: mysql.Family,
			construct: func(options Options) dialect.DialectPort { return mysql.New(mysqlOptions(options)...) }},
		"mariadb": {family: mysql.Family,
			construct: func(options Options) dialect.DialectPort { return mariadb.New(mysqlOptions(options)...) }},
		"sqlite": {family: sqlite.Family,
			construct: func(options Options) dialect.DialectPort { return sqlite.New(sqliteOptions(options)...) }},
		"duckdb": {family: duckdb.Family,
			construct: func(options Options) dialect.DialectPort { return duckdb.New(duckdbOptions(options)...) }},
		"clickhouse": {family: clickhouse.Family,
			construct: func(options Options) dialect.DialectPort { return clickhouse.New(clickhouseOptions(options)...) }},
	}
)

var (
	// mysqlFamily lists the dialects that read MySQL's options.
	mysqlFamily = []string{"mysql", "mariadb"}

	// clickhouseOnly lists the dialect that reads ClickHouse's options.
	clickhouseOnly = []string{"clickhouse"}

	// optionScopes lists the options that apply to some dialects only, in the order New
	// reports them.
	optionScopes = []optionScope{
		{field: "Returning", dialects: mysqlFamily, isSet: func(options Options) bool { return options.Returning != nil }},
		{field: "MaxTypeParseDepth", dialects: clickhouseOnly, isSet: func(options Options) bool { return options.MaxTypeParseDepth != 0 }},
		{field: "JoinUseNulls", dialects: clickhouseOnly, isSet: func(options Options) bool { return options.JoinUseNulls != nil }},
		{field: "GroupByUseNulls", dialects: clickhouseOnly, isSet: func(options Options) bool { return options.GroupByUseNulls != nil }},
		{field: "ANSIQuotes", dialects: mysqlFamily, isSet: func(options Options) bool { return options.ANSIQuotes != nil }},
		{field: "SearchPath", dialects: []string{"postgres", "cockroachdb", "timescaledb"},
			isSet: func(options Options) bool { return len(options.SearchPath) > 0 }},
	}
)

// Options is the plain-data configuration New applies to a dialect. The zero value keeps
// every default.
type Options struct {
	// Returning overrides RETURNING support for MySQL-family dialects. Nil preserves the
	// dialect's own support.
	Returning *bool

	// JoinUseNulls sets the ClickHouse join_use_nulls value assumed during analysis. Nil
	// uses the server default of false.
	JoinUseNulls *bool

	// GroupByUseNulls sets the ClickHouse group_by_use_nulls value assumed during analysis.
	// Nil uses the server default of false.
	GroupByUseNulls *bool

	// ANSIQuotes says whether the MySQL or MariaDB server's sql_mode holds ANSI_QUOTES,
	// which reads "..." as a quoted identifier rather than a string. Nil uses the server
	// default, which reads it as a string.
	ANSIQuotes *bool

	// Name, when set, replaces the name the dialect reports in its Info.
	//
	// The family is unchanged. It may not be blank, nor the name of a built-in dialect of
	// another family, since tools look a dialect up by its name before its family.
	Name string

	// SearchPath lists the schemas the PostgreSQL, CockroachDB or TimescaleDB sessions start
	// with on their search path, in order, as dialect_engine_postgres.WithSearchPath
	// describes. Empty keeps public alone.
	SearchPath []string

	// MaxParseDepth caps the parser's recursion depth; zero keeps the dialect's default. It
	// may be at most dialect_registry.MaxParseDepthCeiling.
	MaxParseDepth int

	// MaxTokensPerStatement caps the tokens the parser walks in one statement; zero keeps
	// the dialect's default. It may be at most
	// dialect_registry.MaxTokensPerStatementCeiling.
	MaxTokensPerStatement int

	// MaxTypeParseDepth caps the nesting of type names such as Array(Nullable(...)).
	//
	// It applies to clickhouse only; zero keeps the default. It may be at most
	// dialect.MaxTypeDepth.
	MaxTypeParseDepth int
}

// OptionError reports one option New refuses: a limit that is negative or above its
// ceiling, a name that is blank or another family's built-in name, or an option set for a
// dialect it does not apply to.
//
// New joins the problems into one error. Each OptionError wraps ErrInvalidOption, and
// OptionErrors extracts the individual problems.
type OptionError struct {
	// Option names the Options field, such as Returning.
	Option string

	// Dialect names the dialect New was asked for.
	Dialect string

	// Text is the refused text of an option set to text, such as a Name, or empty for a
	// limit or an option that does not apply.
	Text string

	// AppliesTo lists the dialects the option applies to, or is nil for a refused limit.
	AppliesTo []string

	// Value is the refused limit, or zero for an option that does not apply.
	Value int

	// Maximum is the ceiling of a limit set above it, or zero for a negative limit or an
	// option that does not apply.
	Maximum int
}

// Error describes the problem by the name of the Options field.
//
// Returns string which is ErrInvalidOption's text followed by the description.
func (e *OptionError) Error() string {
	return ErrInvalidOption.Error() + ": " + e.Describe(e.Option)
}

// Unwrap returns ErrInvalidOption.
//
// Returns error which is ErrInvalidOption.
func (*OptionError) Unwrap() error {
	return ErrInvalidOption
}

// Describe describes the problem under the name the caller set the option by, such as a
// configuration file's key, so a tool can report it in its own terms.
//
// Takes name (string) which names the option.
//
// Returns string which says the limit must not be negative or must be at most its
// ceiling, the name must not be blank or another family's built-in name, or which
// dialects the option applies to.
func (e *OptionError) Describe(name string) string {
	switch {
	case e.AppliesTo != nil:
		return fmt.Sprintf("%s applies to %s, not %s", name, listedNames(e.AppliesTo), e.Dialect)
	case e.Text != "" && strings.TrimSpace(e.Text) == "":
		return fmt.Sprintf("%s must not be blank", name)
	case e.Text != "":
		return fmt.Sprintf("%s must not name a built-in dialect of another family than %s, got %q", name, e.Dialect, e.Text)
	case e.Maximum > 0:
		return fmt.Sprintf("%s must be at most %d, got %d", name, e.Maximum, e.Value)
	default:
		return fmt.Sprintf("%s must not be negative, got %d", name, e.Value)
	}
}

// builtinDialect is one dialect New builds.
type builtinDialect struct {
	// construct builds the dialect from its options.
	construct func(Options) dialect.DialectPort

	// family is the family the dialect reports in its Info, such as postgres for
	// cockroachdb.
	family string
}

// optionScope says which dialects one option applies to.
type optionScope struct {
	// isSet reports whether the options set it.
	isSet func(Options) bool

	// field names the Options field.
	field string

	// dialects lists the dialects it applies to.
	dialects []string
}

// joinedError is an error that holds several errors, as errors.Join returns.
type joinedError interface {
	error

	// Unwrap returns the errors it holds.
	//
	// Returns []error which holds the wrapped errors.
	Unwrap() []error
}

// Names returns the names New accepts, sorted.
//
// Returns []string which is a fresh, sorted list of the built-in dialect names.
func Names() []string {
	return slices.Sorted(maps.Keys(builtins))
}

// New constructs the built-in dialect with the given name.
//
// Takes name (string) which is one of Names, such as postgres or sqlite.
// Takes options (Options) which configure the dialect.
//
// Returns dialect.DialectPort which is the configured dialect.
// Returns error which wraps ErrUnknownDialect, and lists the known names, when name is
// not a built-in dialect, or wraps ErrInvalidOption when a limit is negative or above its
// ceiling, the name is blank or another family's built-in name, or an option does not
// apply to the dialect.
func New(name string, options Options) (dialect.DialectPort, error) {
	builtin, found := builtins[name]
	if !found {
		return nil, fmt.Errorf("%w %q: expected one of %s", ErrUnknownDialect, name, strings.Join(Names(), ", "))
	}
	if err := validate(name, options); err != nil {
		return nil, err
	}
	return builtin.construct(options), nil
}

// OptionErrors finds every OptionError in an error New returned.
//
// Takes err (error) which is the error, or nil.
//
// Returns []*OptionError which holds them in the order New found them, or nil when the
// error holds none.
func OptionErrors(err error) []*OptionError {
	if joined, ok := errors.AsType[joinedError](err); ok {
		children := joined.Unwrap()
		found := make([]*OptionError, 0, len(children))
		for _, each := range children {
			found = append(found, OptionErrors(each)...)
		}
		return found
	}
	if optionError, found := errors.AsType[*OptionError](err); found {
		return []*OptionError{optionError}
	}
	return nil
}

// validate reports a name that is blank or another family's built-in name, limits that
// are negative or above their ceilings, and options that do not apply to the named
// dialect.
//
// Takes name (string) which is the known dialect name.
// Takes options (Options) which are the options to check.
//
// Returns error which joins an *OptionError for each problem found, or nil.
func validate(name string, options Options) error {
	var problems []error
	if nameRefused(name, options.Name) {
		problems = append(problems, &OptionError{Option: "Name", Dialect: name, Text: options.Name, AppliesTo: nil, Value: 0, Maximum: 0})
	}
	limits := []struct {
		field   string
		value   int
		maximum int
	}{
		{field: "MaxParseDepth", value: options.MaxParseDepth, maximum: dialectkit.MaxParseDepthCeiling},
		{field: "MaxTokensPerStatement", value: options.MaxTokensPerStatement, maximum: dialectkit.MaxTokensPerStatementCeiling},
		{field: "MaxTypeParseDepth", value: options.MaxTypeParseDepth, maximum: sqlmodel.MaxTypeDepth},
	}
	for _, limit := range limits {
		switch {
		case limit.value < 0:
			problems = append(problems, &OptionError{Option: limit.field, Dialect: name, Text: "", AppliesTo: nil, Value: limit.value, Maximum: 0})
		case limit.value > limit.maximum:
			problems = append(problems, &OptionError{Option: limit.field, Dialect: name, Text: "", AppliesTo: nil, Value: limit.value,
				Maximum: limit.maximum})
		}
	}
	for _, scope := range optionScopes {
		if scope.isSet(options) && !slices.Contains(scope.dialects, name) {
			problems = append(problems, &OptionError{Option: scope.field, Dialect: name, Text: "", AppliesTo: slices.Clone(scope.dialects),
				Value: 0, Maximum: 0})
		}
	}
	return errors.Join(problems...)
}

// nameRefused reports whether a dialect may not report a name: one that is blank, or that
// names a built-in dialect of another family, matched regardless of case and surrounding
// space. An empty name keeps the dialect's own and is never refused.
//
// Takes name (string) which is the known dialect name.
// Takes custom (string) which is the name the options give, or empty.
//
// Returns bool which is true when the name is refused.
func nameRefused(name, custom string) bool {
	if custom == "" {
		return false
	}
	trimmed := strings.TrimSpace(custom)
	if trimmed == "" {
		return true
	}
	for builtinName, builtin := range builtins {
		if strings.EqualFold(trimmed, builtinName) && builtin.family != builtins[name].family {
			return true
		}
	}
	return false
}

// listedNames lists names in prose: one alone, two joined by "and", more separated by
// commas with "and" before the last.
//
// Takes names ([]string) which are listed.
//
// Returns string which is the list.
func listedNames(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// postgresOptions translates Options for the postgres family.
//
// Takes options (Options) which are the validated options.
//
// Returns []postgres.Option which applies them.
func postgresOptions(options Options) []postgres.Option {
	var result []postgres.Option
	if options.Name != "" {
		result = append(result, postgres.WithName(options.Name))
	}
	if len(options.SearchPath) > 0 {
		result = append(result, postgres.WithSearchPath(options.SearchPath...))
	}
	return append(result,
		postgres.WithMaxParseDepth(options.MaxParseDepth),
		postgres.WithMaxTokensPerStatement(options.MaxTokensPerStatement),
	)
}

// mysqlOptions translates Options for the mysql family.
//
// Takes options (Options) which are the validated options.
//
// Returns []mysql.Option which applies them.
func mysqlOptions(options Options) []mysql.Option {
	var result []mysql.Option
	if options.Name != "" {
		result = append(result, mysql.WithName(options.Name))
	}
	if options.Returning != nil {
		result = append(result, mysql.WithReturning(returningStatements(*options.Returning)))
	}
	if options.ANSIQuotes != nil {
		result = append(result, mysql.WithANSIQuotes(*options.ANSIQuotes))
	}
	return append(result,
		mysql.WithMaxParseDepth(options.MaxParseDepth),
		mysql.WithMaxTokensPerStatement(options.MaxTokensPerStatement),
	)
}

// sqliteOptions translates Options for sqlite.
//
// Takes options (Options) which are the validated options.
//
// Returns []sqlite.Option which applies them.
func sqliteOptions(options Options) []sqlite.Option {
	var result []sqlite.Option
	if options.Name != "" {
		result = append(result, sqlite.WithName(options.Name))
	}
	return append(result,
		sqlite.WithMaxParseDepth(options.MaxParseDepth),
		sqlite.WithMaxTokensPerStatement(options.MaxTokensPerStatement),
	)
}

// duckdbOptions translates Options for duckdb.
//
// Takes options (Options) which are the validated options.
//
// Returns []duckdb.Option which applies them.
func duckdbOptions(options Options) []duckdb.Option {
	var result []duckdb.Option
	if options.Name != "" {
		result = append(result, duckdb.WithName(options.Name))
	}
	return append(result,
		duckdb.WithMaxParseDepth(options.MaxParseDepth),
		duckdb.WithMaxTokensPerStatement(options.MaxTokensPerStatement),
	)
}

// clickhouseOptions translates Options for clickhouse.
//
// Takes options (Options) which are the validated options.
//
// Returns []clickhouse.Option which applies them.
func clickhouseOptions(options Options) []clickhouse.Option {
	var result []clickhouse.Option
	if options.Name != "" {
		result = append(result, clickhouse.WithName(options.Name))
	}
	if options.JoinUseNulls != nil {
		result = append(result, clickhouse.WithJoinUseNulls(*options.JoinUseNulls))
	}
	if options.GroupByUseNulls != nil {
		result = append(result, clickhouse.WithGroupByUseNulls(*options.GroupByUseNulls))
	}
	return append(result,
		clickhouse.WithMaxParseDepth(options.MaxParseDepth),
		clickhouse.WithMaxTokensPerStatement(options.MaxTokensPerStatement),
		clickhouse.WithMaxTypeParseDepth(options.MaxTypeParseDepth),
	)
}

// returningStatements turns the on-or-off Returning option into the set of statements
// that accept a RETURNING clause.
//
// Takes enabled (bool) which is the option's value.
//
// Returns ast.DataChange which is INSERT, UPDATE and DELETE when enabled, and the empty
// set otherwise.
func returningStatements(enabled bool) ast.DataChange {
	if !enabled {
		return 0
	}
	return ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete
}
