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

package cockroachdb

import (
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// crdbInternalSchema is the schema under which CockroachDB's crdb_internal.* builtins
	// are registered so a schema-qualified call resolves to them.
	crdbInternalSchema = "crdb_internal"

	// engineNameInt8 is the 64-bit integer engine type that CockroachDB's INT / INTEGER /
	// SERIAL family maps to, unlike PostgreSQL's 32-bit int4.
	engineNameInt8 = "int8"

	// widthInt8 is the width in bits of CockroachDB's INT family.
	widthInt8 = 64

	// engineNameFloat8 names the FLOAT8 type CockroachDB computes floating-point values in.
	engineNameFloat8 = "float8"

	// widthFloat8 is the width in bits of a FLOAT8.
	widthFloat8 = 64

	// hiddenRowIDColumn is the column CockroachDB adds to a table created without a primary
	// key: rowid INT8 NOT NULL DEFAULT unique_rowid(), left out of SELECT *.
	hiddenRowIDColumn = "rowid"

	// upsertKeyword opens CockroachDB's UPSERT, an INSERT that replaces the row with the
	// same primary key instead of failing on it.
	upsertKeyword = "UPSERT"
)

var (
	// cockroachDBTypes holds the CockroachDB type names that map onto the PostgreSQL
	// engine's structured SQLType values: STRING and BYTES, and the INT family, which is
	// 64-bit in CockroachDB.
	//
	// Registered as extra types, they reach both the type catalogue and the type normaliser,
	// so a column declared STRING NOT NULL types as text.
	cockroachDBTypes = map[string]sqlmodel.SQLType{
		"string":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		"bytes":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "bytea"),
		"int":     sqlmodel.NewIntegerType(engineNameInt8, widthInt8, false),
		"integer": sqlmodel.NewIntegerType(engineNameInt8, widthInt8, false),
		"int64":   sqlmodel.NewIntegerType(engineNameInt8, widthInt8, false),
		"serial":  sqlmodel.NewIntegerType(engineNameInt8, widthInt8, false),
	}

	// postgresOnlyFunctions lists PostgreSQL functions rejected by CockroachDB 25.1.
	postgresOnlyFunctions = []string{
		"any_value", "array_dims", "array_fill", "array_ndims", "bit_xor", "date_bin",
		"factorial", "gcd", "isfinite", "jsonb_path_exists", "jsonb_path_match",
		"jsonb_path_query_array", "jsonb_path_query_first", "jsonb_set_lax", "justify_days",
		"justify_hours", "justify_interval", "lcm", "log10", "make_interval", "make_time", "mode",
		"normalize", "numnode", "pg_advisory_lock", "pg_advisory_lock_shared",
		"pg_advisory_xact_lock", "pg_advisory_xact_lock_shared", "pg_notify", "pg_size_pretty",
		"pg_table_size", "pg_total_relation_size", "pg_try_advisory_lock_shared",
		"pg_try_advisory_xact_lock", "pg_try_advisory_xact_lock_shared", "querytree",
		"regexp_count", "regexp_instr", "regexp_like", "regexp_match", "regexp_matches",
		"regexp_substr", "setweight", "starts_with", "strict_word_similarity", "strip", "to_date",
		"to_number", "ts_headline", "ts_rank_cd", "websearch_to_tsquery", "word_similarity",
		"int4range", "int8range", "numrange", "tsrange", "tstzrange", "daterange", "isempty",
		"lower_inc", "upper_inc", "lower_inf", "upper_inf", "range_merge", "range_agg",
		"date_add", "date_subtract", "jsonb_path_query", "pg_current_xact_id", "pg_current_xact_id_if_assigned",
		"pg_database_size", "pg_indexes_size", "pg_relation_size", "pg_size_bytes", "point", "trim_array", "txid_current",
		"txid_current_if_assigned",
	}

	// retypedFunctions names the PostgreSQL built-in functions CockroachDB 25.1 has with
	// another result type, which registerRetypedFunctions registers again as CockroachDB has
	// them.
	retypedFunctions = []string{
		"sha224", "sha256", "sha384", "sha512", "num_nulls", "num_nonnulls", "pg_sleep",
		"abs", "mod", "power", "pow", "sign", "cbrt",
	}
)

// New creates a CockroachDB dialect using the PostgreSQL parser.
//
// It applies CockroachDB's types, functions and schema rules, including hidden columns,
// and rejects unsupported PostgreSQL syntax.
//
// Takes options (...postgres.Option) which apply after the CockroachDB wiring and add to
// it, as postgres.Option describes: a caller's extra functions, types and hooks join
// CockroachDB's rather than replacing them. Use postgres.WithMaxTokensPerStatement or
// postgres.WithMaxParseDepth to tune the parser limits.
//
// Returns *postgres.Dialect which is the CockroachDB dialect.
func New(options ...postgres.Option) *postgres.Dialect {
	return postgres.New(append([]postgres.Option{
		postgres.WithName("cockroachdb"),
		postgres.WithExtraTypes(cockroachDBTypes),
		postgres.WithoutBuiltinFunctions(postgresOnlyFunctions...),
		postgres.WithoutBuiltinFunctions(retypedFunctions...),
		postgres.WithExtraFunctions(registerCockroachDBFunctions),
		postgres.WithPostParseHook(rejectPartitionStatements),
		postgres.WithPostParseHook(addHiddenRowID),
		postgres.WithPostParseHook(alterPrimaryKey),
		postgres.WithPostParseHook(addHashShardColumn),
		postgres.WithInsertKeywords(upsertKeyword),
		postgres.WithRowValueTypeHook(rowValueType),
		postgres.WithAliasNamesSingleColumnFunctions(),
		postgres.WithDecimalIntegerDivision(),
		postgres.WithInlineIndexElements(),
		postgres.WithComputedColumnShorthand(),
		postgres.WithNotVisibleColumns(),
		postgres.WithAsOfSystemTime(),
		postgres.WithParametersTypedFromContext(),
		postgres.WithPromoteTypeHook(widenRealAcrossCategories),
	}, options...)...)
}

// rejectPartitionStatements fails a CREATE TABLE ...
//
// PARTITION OF, ALTER TABLE ... ATTACH PARTITION or ALTER TABLE ... DETACH PARTITION,
// which PostgreSQL has and CockroachDB rejects as a syntax error: CockroachDB partitions
// a table with the PARTITION BY clause of the table itself, which creates no table per
// partition.
//
// Takes p (pgext.ParserContext) which exposes the statement's tokens, to locate the
// error.
// Takes mutation (*ast.CatalogueMutation) which is the parsed statement's mutation.
//
// Returns error which is a dialectkit.ParseError for a partition statement, else nil.
func rejectPartitionStatements(p pgext.ParserContext, _ pgext.StatementKind, mutation *ast.CatalogueMutation) error {
	if mutation == nil {
		return nil
	}
	for _, each := range append([]*ast.CatalogueMutation{mutation}, mutation.AdditionalMutations...) {
		if each.PartitionOf != nil {
			return dialectkit.NewParseError(partitionToken(p.Tokens()),
				"CockroachDB has no PARTITION OF, ATTACH PARTITION or DETACH PARTITION; partition the table with its PARTITION BY clause")
		}
	}
	return nil
}

// partitionToken returns the PARTITION keyword of a partition statement, for the error to
// point at, or the first token when there is none.
//
// Takes tokens ([]pgext.Token) which are the statement's tokens.
//
// Returns pgext.Token which is the token to report the error at.
func partitionToken(tokens []pgext.Token) pgext.Token {
	for _, token := range tokens {
		if token.IsKeyword(pgext.TokenIdentifier, "PARTITION") {
			return token
		}
	}
	if len(tokens) == 0 {
		return pgext.Token{}
	}
	return tokens[0]
}

// addHiddenRowID adds the hidden primary-key column CockroachDB creates for tables
// without a primary key; queries can name it, but SELECT * excludes it. Its name is
// rowid, or the first available rowid_N when a declared column already uses that name.
//
// Takes mutation (*ast.CatalogueMutation) which is the parsed statement's mutation; one
// that creates no table, or a table with a primary key, is left alone.
//
// Returns error which is always nil.
func addHiddenRowID(_ pgext.ParserContext, _ pgext.StatementKind, mutation *ast.CatalogueMutation) error {
	if mutation == nil || mutation.Kind != ast.MutationCreateTable || mutation.PartitionOf != nil ||
		len(mutation.InheritsTables) > 0 || len(mutation.PrimaryKey) > 0 {
		return nil
	}
	declared := make(map[string]struct{}, len(mutation.Columns))
	for index := range mutation.Columns {
		declared[strings.ToLower(mutation.Columns[index].Name)] = struct{}{}
	}
	name := hiddenRowIDColumn
	for suffix := 1; isDeclared(declared, name); suffix++ {
		name = hiddenRowIDColumn + "_" + strconv.Itoa(suffix)
	}
	rowID := sqlmodel.NewColumn(name, sqlmodel.NewIntegerType(engineNameInt8, widthInt8, false), false)
	rowID.HasDefault = true
	rowID.Hidden = true
	mutation.Columns = append(mutation.Columns, rowID)
	mutation.PrimaryKey = []string{name}
	return nil
}

// alterPrimaryKey reads the primary key an ALTER TABLE gives a table as CockroachDB does:
// ADD PRIMARY KEY and ALTER PRIMARY KEY USING COLUMNS (a, b), which the postgres parser
// reads as adding the key, replace the key the table had, dropping the hidden rowid of a
// table created without one.
//
// Takes mutation (*ast.CatalogueMutation) which is the parsed statement's mutation; each
// of its actions that adds a primary key replaces the hidden one.
//
// Returns error which is always nil.
func alterPrimaryKey(_ pgext.ParserContext, _ pgext.StatementKind, mutation *ast.CatalogueMutation) error {
	if mutation == nil {
		return nil
	}
	for _, action := range append([]*ast.CatalogueMutation{mutation}, mutation.AdditionalMutations...) {
		if action.Kind == ast.MutationAlterTableAddConstraint && len(action.PrimaryKey) > 0 {
			action.ReplacesHiddenKey = true
		}
	}
	return nil
}

// registerCockroachDBFunctions adds CockroachDB-specific functions to the shared
// PostgreSQL catalogue.
//
// Takes builder (*postgres.FunctionCatalogueBuilder) which receives the extra function
// signatures.
func registerCockroachDBFunctions(builder *postgres.FunctionCatalogueBuilder) {
	integerType := builder.Bigint()
	textType := builder.Text()
	byteaType := builder.Bytea()
	numericType := builder.Numeric()
	uuidType := builder.UUID()
	timestamptzType := builder.Timestamptz()

	builder.NeverNull("unique_rowid", nil, integerType)
	builder.NeverNull("cluster_logical_timestamp", nil, numericType)
	builder.NeverNull("gateway_region", nil, textType)
	builder.NeverNull("rehome_row", nil, textType)

	builder.NullOnNull("cluster_id", nil, uuidType).Schema = crdbInternalSchema
	builder.NeverNull("node_id", nil, integerType).Schema = crdbInternalSchema
	builder.NullOnNull("locality_value", builder.Args(postgres.Arg{Name: "key", Type: textType}), textType).Schema = crdbInternalSchema
	builder.NullOnNull("from_ip", builder.Args(postgres.Arg{Name: "value", Type: byteaType}), textType)
	builder.NullOnNull("to_ip", builder.Args(postgres.Arg{Name: "address", Type: textType}), byteaType)
	builder.NullOnNull("experimental_strftime", builder.Args(postgres.Arg{Name: "input", Type: timestamptzType}, postgres.Arg{Name: "format", Type: textType}), textType)
	builder.NullOnNull("experimental_strptime", builder.Args(postgres.Arg{Name: "input", Type: textType}, postgres.Arg{Name: "format", Type: textType}), timestamptzType)
	registerIdentifierFunctions(builder)
	registerFuzzyMatchingFunctions(builder)
	registerRetypedFunctions(builder)
	arrays := builder.CalledOnNull("array_cat_agg", builder.Args(postgres.Arg{Name: "arrays", Type: builder.Any()}), builder.Any())
	arrays.IsAggregate = true
	arrays.ReturnTypeFromArgument = 1
}

// registerRetypedFunctions registers the functions retypedFunctions names as CockroachDB
// 25.1 has them: the SHA digests take strings or bytes, any number of them, and return
// the digest as hexadecimal text where PostgreSQL returns bytea; num_nulls and
// num_nonnulls count into an INT8; and pg_sleep returns a boolean rather than void.
//
// Takes builder (*postgres.FunctionCatalogueBuilder) which receives the signatures.
func registerRetypedFunctions(builder *postgres.FunctionCatalogueBuilder) {
	value := builder.Args(postgres.Arg{Name: "value", Type: builder.Any()})
	for _, name := range []string{"sha224", "sha256", "sha384", "sha512"} {
		digest := builder.NullOnNull(name, value, builder.Text())
		digest.IsVariadic = true
		digest.MinArguments = 1
	}
	for _, name := range []string{"num_nulls", "num_nonnulls"} {
		counted := builder.NeverNull(name, value, builder.Bigint())
		counted.IsVariadic = true
		counted.MinArguments = 1
	}
	builder.NullOnNull("pg_sleep", builder.Args(postgres.Arg{Name: "seconds", Type: builder.Float8()}), builder.Boolean())
	registerRetypedMathematicalFunctions(builder)
}

// registerRetypedMathematicalFunctions registers abs, mod, power, pow, sign and cbrt as
// CockroachDB computes them, in INT8, FLOAT8 or DECIMAL. FLOAT8 comes first, as in
// PostgreSQL's catalogue, so a placeholder argument reads as one.
//
// Takes builder (*postgres.FunctionCatalogueBuilder) which receives the signatures.
func registerRetypedMathematicalFunctions(builder *postgres.FunctionCatalogueBuilder) {
	numbers := []sqlmodel.SQLType{builder.Float8(), builder.Bigint(), builder.Numeric()}
	for index := range numbers {
		number := &numbers[index]
		for _, name := range []string{"abs", "sign"} {
			builder.NullOnNull(name, builder.Args(postgres.Arg{Name: "x", Type: *number}), *number)
		}
		for _, name := range []string{"mod", "power", "pow"} {
			builder.NullOnNull(name, builder.Args(postgres.Arg{Name: "x", Type: *number}, postgres.Arg{Name: "y", Type: *number}), *number)
		}
		if number.Category != sqlmodel.TypeCategoryInteger {
			builder.NullOnNull("cbrt", builder.Args(postgres.Arg{Name: "x", Type: *number}), *number)
		}
	}
}

// widenRealAcrossCategories widens a REAL to a FLOAT8 where it meets a number of another
// category, as CockroachDB does in a CASE, COALESCE or GREATEST.
//
// Takes left (sqlmodel.SQLType) which is one operand's type.
// Takes right (sqlmodel.SQLType) which is the other's.
//
// Returns *sqlmodel.SQLType which is FLOAT8, or nil to leave the pair to PostgreSQL's
// rule.
func widenRealAcrossCategories(left, right sqlmodel.SQLType) *sqlmodel.SQLType {
	promoted, across := dialectkit.PromoteAcrossNumericCategories(left, right)
	if !across || promoted.Category != sqlmodel.TypeCategoryFloat || promoted.Width == widthFloat8 {
		return nil
	}
	widened := sqlmodel.NewFloatType(engineNameFloat8, widthFloat8)
	return &widened
}

// registerIdentifierFunctions registers CockroachDB's identifier generators and
// converters, gen_random_ulid, unordered_unique_rowid, uuid_v4, uuid_generate_v4,
// ulid_to_uuid and uuid_to_ulid, and follower_read_timestamp, the time a follower read
// can use.
//
// Takes builder (*postgres.FunctionCatalogueBuilder) which receives the signatures.
func registerIdentifierFunctions(builder *postgres.FunctionCatalogueBuilder) {
	builder.NeverNull("gen_random_ulid", nil, builder.UUID())
	builder.NeverNull("unordered_unique_rowid", nil, builder.Bigint())
	builder.NeverNull("uuid_v4", nil, builder.Bytea())
	builder.NeverNull("uuid_generate_v4", nil, builder.UUID())
	builder.NeverNull("follower_read_timestamp", nil, builder.Timestamptz())
	builder.NullOnNull("ulid_to_uuid", builder.Args(postgres.Arg{Name: "value", Type: builder.Text()}), builder.UUID())
	builder.NullOnNull("uuid_to_ulid", builder.Args(postgres.Arg{Name: "value", Type: builder.UUID()}), builder.Text())
}

// registerFuzzyMatchingFunctions registers CockroachDB's fuzzy string matching functions:
// levenshtein, with or without the insertion, deletion and substitution costs, soundex,
// metaphone and difference.
//
// Takes builder (*postgres.FunctionCatalogueBuilder) which receives the signatures.
func registerFuzzyMatchingFunctions(builder *postgres.FunctionCatalogueBuilder) {
	source := postgres.Arg{Name: "source", Type: builder.Text()}
	target := postgres.Arg{Name: "target", Type: builder.Text()}
	builder.NullOnNull("levenshtein", builder.Args(source, target), builder.Bigint())
	builder.NullOnNull("levenshtein", builder.Args(source, target,
		postgres.Arg{Name: "ins_cost", Type: builder.Bigint()},
		postgres.Arg{Name: "del_cost", Type: builder.Bigint()},
		postgres.Arg{Name: "sub_cost", Type: builder.Bigint()}), builder.Bigint())
	builder.NullOnNull("soundex", builder.Args(source), builder.Text())
	builder.NullOnNull("metaphone", builder.Args(source, postgres.Arg{Name: "max_output_length", Type: builder.Bigint()}), builder.Text())
	builder.NullOnNull("difference", builder.Args(source, target), builder.Bigint())
}

// isDeclared reports whether a CREATE TABLE declares a column of a name.
//
// Takes declared (map[string]struct{}) which holds the lower-cased names of the table's
// columns.
// Takes name (string) which is the lower-case name.
//
// Returns bool which is true when one of its columns has the name.
func isDeclared(declared map[string]struct{}, name string) bool {
	_, found := declared[name]
	return found
}
