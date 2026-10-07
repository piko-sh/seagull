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

package config

const (
	// CurrentVersion is the configuration schema version the config package reads.
	CurrentVersion = 1

	// MaxFileBytes is the largest configuration file Load accepts.
	MaxFileBytes = 1 << 20

	// MaxDatabases is the most databases one configuration may declare.
	MaxDatabases = 256

	// DefaultNamespace prefixes the tables and locks migrations persist.
	DefaultNamespace = "sgl"

	// DefaultDriver is the driver API generated code targets when none is set.
	DefaultDriver = "database/sql"

	// DefaultMigrationsDirectory is the migrations directory when none is set.
	DefaultMigrationsDirectory = "db/migrations"

	// DefaultQueriesDirectory is the queries directory when none is set.
	DefaultQueriesDirectory = "db/queries"

	// DefaultOutputDirectory is the generated-code directory when none is set.
	DefaultOutputDirectory = "internal/store"

	// DefaultPackage is the generated-code package name when none is set.
	DefaultPackage = "store"

	// DefaultQueryNamesFile is the file the query-name lookup is written to.
	DefaultQueryNamesFile = "query_names.go"

	// DefaultQueryNamesFunction is the name of the generated query-name lookup function.
	DefaultQueryNamesFunction = "QueryName"

	// DefaultInterfaceName is the name of the generated interface of the query methods.
	DefaultInterfaceName = "Querier"
)

// Config is the whole of sgl.yaml.
type Config struct {
	// Databases holds one entry per database.
	Databases []Database `json:"databases" yaml:"databases"`

	// Schema names the JSON Schema an editor validates the file against, such as
	// https://seagull.sh/sgl.schema.json; sgl itself ignores it.
	Schema string `json:"$schema,omitempty" yaml:"$schema,omitempty"`

	// Directives configures the comment directive prefixes.
	Directives Directives `json:"directives" yaml:"directives"`

	// Version is the configuration schema version.
	Version int `json:"version" yaml:"version"`
}

// Directives configures which comment prefixes mark directives.
type Directives struct {
	// Prefixes lists the accepted prefixes; the first is canonical.
	Prefixes []string `json:"prefixes,omitempty" yaml:"prefixes,omitempty"`

	// RequireCanonical reports a hint wherever an alias prefix is used.
	RequireCanonical bool `json:"require_canonical,omitempty" yaml:"require_canonical,omitempty"`
}

// Database configures one database.
type Database struct {
	// Migrate configures the migration run time.
	Migrate Migrate `json:"migrate" yaml:"migrate"`

	// Name identifies the database in output and --database.
	Name string `json:"name" yaml:"name"`

	// Dialect names the SQL dialect.
	Dialect string `json:"dialect" yaml:"dialect"`

	// Migrations is the migration directory.
	Migrations string `json:"migrations,omitempty" yaml:"migrations,omitempty"`

	// Queries is the query directory.
	Queries string `json:"queries,omitempty" yaml:"queries,omitempty"`

	// Seeds is the seed directory.
	Seeds string `json:"seeds,omitempty" yaml:"seeds,omitempty"`

	// SchemaFrom builds the catalogue from a live database instead of migrations.
	SchemaFrom string `json:"schema_from,omitempty" yaml:"schema_from,omitempty"`

	// CustomFunctions declares SQL functions the analyser cannot discover.
	CustomFunctions []CustomFunction `json:"custom_functions,omitempty" yaml:"custom_functions,omitempty"`

	// Go configures the generated code.
	Go Go `json:"go" yaml:"go"`

	// DialectOptions tunes the dialect's parser.
	DialectOptions DialectOptions `json:"dialect_options" yaml:"dialect_options"`
}

// DialectOptions configures a dialect's SQL features, session settings and parser limits.
type DialectOptions struct {
	// Returning enables RETURNING support for the MySQL dialect.
	Returning *bool `json:"returning,omitempty" yaml:"returning,omitempty"`

	// JoinUseNulls is the join_use_nulls setting the ClickHouse server runs with, which
	// decides whether an outer join fills an unmatched row with NULL (true) or with each
	// column's default value (false, the server's default).
	JoinUseNulls *bool `json:"join_use_nulls,omitempty" yaml:"join_use_nulls,omitempty"`

	// GroupByUseNulls is the group_by_use_nulls setting the ClickHouse server runs with,
	// which decides whether a ROLLUP, CUBE or GROUPING SETS row fills the keys it leaves out
	// with NULL (true) or with each key's default value (false, the server's default).
	GroupByUseNulls *bool `json:"group_by_use_nulls,omitempty" yaml:"group_by_use_nulls,omitempty"`

	// ANSIQuotes says whether the MySQL or MariaDB server's sql_mode holds ANSI_QUOTES,
	// which reads "..." as a quoted identifier (true) rather than a string (false, the
	// server's default).
	ANSIQuotes *bool `json:"ansi_quotes,omitempty" yaml:"ansi_quotes,omitempty"`

	// SearchPath lists schemas used to resolve unqualified names in PostgreSQL and its
	// flavours. Unqualified objects are created in the first existing schema; empty uses
	// public alone.
	SearchPath []string `json:"search_path,omitempty" yaml:"search_path,omitempty"`

	// MaxParseDepth limits expression nesting.
	MaxParseDepth int `json:"max_parse_depth,omitempty" yaml:"max_parse_depth,omitempty"`

	// MaxTokensPerStatement limits the size of one statement.
	MaxTokensPerStatement int `json:"max_tokens_per_statement,omitempty" yaml:"max_tokens_per_statement,omitempty"`

	// MaxTypeParseDepth limits type nesting in ClickHouse.
	MaxTypeParseDepth int `json:"max_type_parse_depth,omitempty" yaml:"max_type_parse_depth,omitempty"`
}

// CustomFunction declares a SQL function the analyser cannot discover.
type CustomFunction struct {
	// Name is the function name.
	Name string `json:"name" yaml:"name"`

	// Returns is the SQL return type.
	Returns string `json:"returns" yaml:"returns"`

	// Nullable is "null_on_null" (the default), "never_null" or "called_on_null".
	Nullable string `json:"nullable,omitempty" yaml:"nullable,omitempty"`

	// DataAccess is "read_only" (the default) or "modifies_data", which sends a query
	// calling the function to the writer.
	DataAccess string `json:"data_access,omitempty" yaml:"data_access,omitempty"`

	// Args lists the argument SQL types.
	Args []string `json:"args,omitempty" yaml:"args,omitempty"`

	// MinArgs is the minimum argument count for optional trailing arguments.
	MinArgs int `json:"min_args,omitempty" yaml:"min_args,omitempty"`

	// Aggregate reports whether the function aggregates rows.
	Aggregate bool `json:"aggregate,omitempty" yaml:"aggregate,omitempty"`

	// Variadic reports whether the last argument repeats.
	Variadic bool `json:"variadic,omitempty" yaml:"variadic,omitempty"`
}

// Go configures generated code.
type Go struct {
	// QueryNames configures the query-name lookup.
	QueryNames QueryNames `json:"query_names" yaml:"query_names"`

	// Package is the generated package name.
	Package string `json:"package,omitempty" yaml:"package,omitempty"`

	// Out is the output directory.
	Out string `json:"out,omitempty" yaml:"out,omitempty"`

	// Driver names a stock emitter ("database/sql", "pgx" or "clickhouse") or one registered
	// by a custom sgl binary.
	Driver string `json:"driver,omitempty" yaml:"driver,omitempty"`

	// LegacyHeaders lists earlier generated-file headers that mark a file as prunable.
	LegacyHeaders []string `json:"legacy_headers,omitempty" yaml:"legacy_headers,omitempty"`

	// StructTags lists, by key, the struct tags every generated field holding a column
	// carries besides json, such as db: "{name}", where {name} stands for the column's name
	// as the json tag is made from it.
	StructTags map[string]string `json:"struct_tags,omitempty" yaml:"struct_tags,omitempty"`

	// Interface configures the generated interface of the query methods.
	Interface Interface `json:"interface" yaml:"interface"`

	// JSONTags shapes the json tag of every generated field holding a column.
	JSONTags JSONTags `json:"json_tags" yaml:"json_tags"`

	// Types configures type mapping.
	Types Types `json:"types" yaml:"types"`

	// Prepared emits a prepared-statement cache.
	Prepared bool `json:"prepared,omitempty" yaml:"prepared,omitempty"`

	// KeepStars writes SELECT * and RETURNING * in the generated SQL as the queries write
	// them, rather than naming the columns a star stands for.
	KeepStars bool `json:"keep_stars,omitempty" yaml:"keep_stars,omitempty"`

	// EmptySlices makes a many method return an empty slice rather than nil when no row
	// matches, so it encodes to JSON as [] rather than null.
	EmptySlices bool `json:"empty_slices,omitempty" yaml:"empty_slices,omitempty"`

	// DocComments writes doc comments from the SQL: a query's description on its method, and
	// a table's or column's comment on its model or field.
	DocComments bool `json:"doc_comments,omitempty" yaml:"doc_comments,omitempty"`

	// ReuseModels reads the rows of every query whose rows are a table's model into the
	// model rather than a Row struct of the query's own.
	ReuseModels bool `json:"reuse_models,omitempty" yaml:"reuse_models,omitempty"`

	// WrapErrors wraps every error a generated method returns with a label naming the
	// method, so an error says which query met it while errors.Is and errors.As still find
	// the error beneath, such as sql.ErrNoRows.
	WrapErrors bool `json:"wrap_errors,omitempty" yaml:"wrap_errors,omitempty"`
}

// Interface configures the generated interface listing the method of every query.
type Interface struct {
	// Name is the interface's name, Querier by default.
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// Enabled turns the interface on.
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

// JSONTags shapes the json tags of generated fields.
type JSONTags struct {
	// Case is the case style of the names: snake, camel or pascal, or empty for the column's
	// name as it is written.
	Case string `json:"case,omitempty" yaml:"case,omitempty"`

	// Omit leaves the json tags out.
	Omit bool `json:"omit,omitempty" yaml:"omit,omitempty"`

	// OmitEmpty adds the omitempty option to every json tag.
	OmitEmpty bool `json:"omitempty,omitempty" yaml:"omitempty,omitempty"`
}

// QueryNames configures the generated SQL-to-query-name lookup.
type QueryNames struct {
	// File is the generated file name.
	File string `json:"file,omitempty" yaml:"file,omitempty"`

	// Function is the generated function name, written as func in sgl.yaml.
	Function string `json:"func,omitempty" yaml:"func,omitempty"`

	// Enabled turns the lookup on.
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

// Types configures how SQL types map to Go types.
type Types struct {
	// Decimal is the Go type for NUMERIC and DECIMAL, such as "string".
	Decimal string `json:"decimal,omitempty" yaml:"decimal,omitempty"`

	// Money is the Go type for MONEY.
	Money string `json:"money,omitempty" yaml:"money,omitempty"`

	// BigInt is the Go type for integers wider than 64 bits.
	BigInt string `json:"bigint,omitempty" yaml:"bigint,omitempty"`

	// JSON is the Go type for JSON columns.
	JSON string `json:"json,omitempty" yaml:"json,omitempty"`

	// Interval is the Go type for intervals, lengths of time, such as "time.Duration".
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`

	// TimeOfDay is the Go type for times of day with no date, such as "string".
	TimeOfDay string `json:"time_of_day,omitempty" yaml:"time_of_day,omitempty"`

	// IPAddress is the Go type for columns holding a single IP address.
	IPAddress string `json:"ip_address,omitempty" yaml:"ip_address,omitempty"`

	// Enum is the Go type for every enum, such as "string". Empty keeps the default: the
	// named Go type the generated package declares for each enum, with a constant for each
	// value, or string where it declares none.
	Enum string `json:"enum,omitempty" yaml:"enum,omitempty"`

	// PackageNames maps an import path to the name its package declares, for a package an
	// override or a type slot names whose name its path does not imply, such as a directory
	// named apart from its package; the generated code imports the package under that name.
	PackageNames map[string]string `json:"package_names,omitempty" yaml:"package_names,omitempty"`

	// Overrides replaces the Go type for one SQL type or one column.
	Overrides []Override `json:"overrides,omitempty" yaml:"overrides,omitempty"`

	// NonNullElements declares the elements of arrays and lists, and the values of maps,
	// non-null where the engine can hold NULL among them, as in a PostgreSQL array or a
	// DuckDB list, so they are not pointers; a column or an override can still declare them
	// nullable.
	NonNullElements bool `json:"non_null_elements,omitempty" yaml:"non_null_elements,omitempty"`
}

// Override replaces the Go type for one SQL type or one column. Exactly one of SQL and
// Column is set.
type Override struct {
	// NonNullElements, when set, declares the elements of the type's or column's arrays and
	// lists, and the values of its maps, non-null (true) or nullable (false).
	NonNullElements *bool `json:"non_null_elements,omitempty" yaml:"non_null_elements,omitempty"`

	// StructTags lists, by key, struct tags every generated field holding the column
	// carries, such as validate: "required,email"; json among them replaces the json tag.
	// Only a column override takes them.
	StructTags map[string]string `json:"struct_tags,omitempty" yaml:"struct_tags,omitempty"`

	// SQL is the SQL type name, such as "public.mood".
	SQL string `json:"sql,omitempty" yaml:"sql,omitempty"`

	// Column is a "table.column" or "schema.table.column" reference.
	Column string `json:"column,omitempty" yaml:"column,omitempty"`

	// Go is the Go type, as "import/path.Name" or a built-in such as "string". It may be
	// empty when NonNullElements or StructTags is set.
	Go string `json:"go,omitempty" yaml:"go,omitempty"`
}

// Migrate configures the migration run time.
type Migrate struct {
	// DSN is the connection string; ${env:NAME} is expanded.
	DSN string `json:"dsn,omitempty" yaml:"dsn,omitempty"`

	// Namespace prefixes the persisted tables and locks.
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`

	// HistoryTable renames the migration history table.
	HistoryTable string `json:"history_table,omitempty" yaml:"history_table,omitempty"`

	// Lock is "advisory", "table" or "none".
	Lock string `json:"lock,omitempty" yaml:"lock,omitempty"`
}

// FileError is a problem with a configuration file that Load could not read, decode or
// validate. It names the file as Load was given it, so a caller can print the path its
// own way and still match the problem with errors.Is, such as ErrInvalid, ErrTooLarge or
// fs.ErrNotExist.
type FileError struct {
	// Err is the problem, which does not name the file.
	Err error

	// Path is the configuration file, as passed to Load.
	Path string
}

// Error returns the file's path and the problem.
//
// Returns string which reads "path: problem".
func (e *FileError) Error() string {
	return e.Path + ": " + e.Err.Error()
}

// Unwrap returns the problem.
//
// Returns error which is the problem.
func (e *FileError) Unwrap() error {
	return e.Err
}
