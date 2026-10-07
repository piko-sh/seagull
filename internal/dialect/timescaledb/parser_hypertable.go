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

package timescaledb

import (
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
)

// hypertableCallArguments aggregates the structured information extracted from a SELECT
// create_hypertable(...) call. The dimension builder is the optional `by_range` /
// `by_hash` form introduced in TS 2.13+; when absent the legacy
// second-positional-argument shape captures a literal column name only.
type hypertableCallArguments struct {
	// table is the targeted table name, optionally schema-qualified.
	table string

	// timeColumn is the partitioning time column extracted from the dimension argument.
	timeColumn string

	// dimensionBuilder is the `by_range` or `by_hash` builder name, empty for the legacy
	// positional form.
	dimensionBuilder string

	// extras is the opaque tail of remaining call arguments.
	extras string
}

// dimensionArgument captures the structured form of the second positional argument of a
// create_hypertable / add_dimension call. The builder field is non-empty only when the
// argument used the `by_range(...)` / `by_hash(...)` form; otherwise the column field
// holds the legacy bare column reference.
type dimensionArgument struct {
	// column is the partitioning column reference.
	column string

	// builder is the `by_range` or `by_hash` builder name, empty for the legacy bare column
	// form.
	builder string
}

// parseCreateHypertable parses the keyword form of a CREATE HYPERTABLE statement.
//
// The recognised shape is:
//
//	CREATE HYPERTABLE [IF NOT EXISTS] [schema.]name (
//	    column_name type [column_constraint],
//	    ...
//	) [PARTITION BY ...] [WITH (...)]
//
// The column and constraint list is parsed exactly as CREATE TABLE parses it, so each
// column's type, nullability and default, and the table's primary key, constraints and
// LIKE copies, are what the same list gives a plain table. Trailing clauses (PARTITION
// BY, WITH, INHERITS) are kept as written under TIMESCALE_TRAILING.
//
// The clone form `CREATE HYPERTABLE name FROM source` and any other shape lacking a `(`
// column list is kept as written under TIMESCALE_BODY, through
// captureHypertableCloneBody.
//
// Takes p (pgext.ParserContext) which drives the parse.
//
// Returns *ast.CatalogueMutation which is the parsed mutation with Kind =
// MutationCreateTable and EngineSpecific entries marking it as a hypertable.
// Returns error which is non-nil when the qualified name or column list fails to parse.
func parseCreateHypertable(p pgext.ParserContext) (*ast.CatalogueMutation, error) {
	if err := p.ExpectKeyword("CREATE"); err != nil {
		return nil, err
	}

	ifNotExists := p.MatchIfNotExists()
	if err := p.ExpectKeyword("HYPERTABLE"); err != nil {
		return nil, err
	}
	if !ifNotExists && p.MatchIfNotExists() {
		ifNotExists = true
	}

	schema, name, qualifiedErr := p.ParseQualifiedName()
	if qualifiedErr != nil {
		return nil, fmt.Errorf("hypertable name: %w", qualifiedErr)
	}

	if p.CurrentToken().Kind != pgext.TokenLeftParen {
		mutation := ast.NewCatalogueMutation(ast.MutationCreateTable, schema, name)
		markHypertable(mutation, ifNotExists)
		captureHypertableCloneBody(p, mutation)
		return mutation, nil
	}

	mutation, err := p.ParseTableElements(schema, name)
	if err != nil {
		return nil, fmt.Errorf("hypertable %q: %w", name, err)
	}
	markHypertable(mutation, ifNotExists)
	captureHypertableTrailingClauses(p, mutation)
	return mutation, nil
}

// markHypertable marks a CREATE HYPERTABLE mutation as creating a hypertable, and as
// changing nothing when the table exists already when the statement said IF NOT EXISTS.
//
// Takes mutation (*ast.CatalogueMutation) which receives the markers.
// Takes ifNotExists (bool) which is true when the statement said IF NOT EXISTS.
func markHypertable(mutation *ast.CatalogueMutation, ifNotExists bool) {
	if mutation.EngineSpecific == nil {
		mutation.EngineSpecific = map[string]string{}
	}
	mutation.EngineSpecific["TIMESCALE_HYPERTABLE"] = literalTrue
	if ifNotExists {
		mutation.IfNotExists = true
		mutation.EngineSpecific["TIMESCALE_IF_NOT_EXISTS"] = literalTrue
	}
}

// captureHypertableCloneBody handles the early-return branch of parseCreateHypertable:
// when the token following the qualified name is not `(`, the statement is treated as a
// clone-form or similar non-column-list shape. The remainder is preserved verbatim under
// TIMESCALE_BODY so downstream consumers can replay the original SQL.
//
// Takes p (pgext.ParserContext) which is positioned at the first post-name token.
// Takes mutation (*ast.CatalogueMutation) which receives the TIMESCALE_BODY entry.
func captureHypertableCloneBody(p pgext.ParserContext, mutation *ast.CatalogueMutation) {
	mutation.EngineSpecific["TIMESCALE_BODY"] = p.ConsumeRemainderAsText()
}

// captureHypertableTrailingClauses captures trailing clauses that may follow the
// close-paren of the column list (PARTITION BY, WITH, INHERITS, etc.) into
// TIMESCALE_TRAILING for downstream consumers. When the cursor already sits at EOF the
// entry is omitted so the EngineSpecific map stays minimal.
//
// Takes p (pgext.ParserContext) which is positioned just after the column-list
// close-paren.
// Takes mutation (*ast.CatalogueMutation) which receives the optional TIMESCALE_TRAILING
// entry.
func captureHypertableTrailingClauses(p pgext.ParserContext, mutation *ast.CatalogueMutation) {
	if p.AtEnd() {
		return
	}
	mutation.EngineSpecific["TIMESCALE_TRAILING"] = p.ConsumeRemainderAsText()
}

// parseCreateHypertableCall parses the function-call form of create_hypertable.
//
// The recognised shape is:
//
//	SELECT create_hypertable('table'::regclass, 'column'::name [, ...])
//
// The first two arguments name the table and time column; subsequent positional or named
// arguments configure partitioning and chunk intervals. The resulting CatalogueMutation
// references the targeted table by name (extracted from the first argument's literal
// text) and carries TIMESCALE_HYPERTABLE = "true" in EngineSpecific.
//
// Because the targeted table is created by a prior CREATE TABLE statement, this mutation
// is treated as an annotation: the catalogue should not need to add the columns again.
// The annotation arrives as a Kind=MutationAlterTableAlterColumn mutation with empty
// Columns and EngineSpecific markers so downstream consumers can detect "this existing
// table is now a hypertable" without changing schema state. The bulk of the work is
// delegated to extractHypertableCallArguments (positional argument extraction) and
// buildHypertableCallMutation (mutation assembly).
//
// Takes p (pgext.ParserContext) which drives the parse.
//
// Returns the parsed annotation mutation.
// Returns error on parse failure.
func parseCreateHypertableCall(p pgext.ParserContext) (*ast.CatalogueMutation, error) {
	if err := p.ExpectKeyword("SELECT"); err != nil {
		return nil, err
	}
	if !p.MatchKeyword(funcNameCreateHypertable) {
		return nil, dialectkit.NewParseError(p.CurrentToken(), "expected create_hypertable")
	}
	if p.CurrentToken().Kind != pgext.TokenLeftParen {
		return nil, dialectkit.NewParseError(p.CurrentToken(), "expected '(' after create_hypertable")
	}
	openParen := p.Advance()

	arguments, argsErr := extractHypertableCallArguments(p, openParen)
	if argsErr != nil {
		return nil, argsErr
	}

	if err := expectCallEnd(p, funcNameCreateHypertable); err != nil {
		return nil, err
	}
	return buildHypertableCallMutation(arguments), nil
}

// extractHypertableCallArguments reads the two required positional arguments of a
// create_hypertable call (table and dimension) and the remaining opaque tail.
//
// The dimension argument can be either a literal column reference (legacy positional
// form) or a `by_range('column', INTERVAL '1 day')` / `by_hash('column', N)` builder; the
// helper decodes the latter so the inner column name surfaces on TIMESCALE_TIME_COLUMN
// while the builder name is recorded separately. The opening paren has already been
// consumed; an unterminated call is reported at it.
//
// Takes p (pgext.ParserContext) which is positioned just after the call's opening paren.
// Takes openParen (pgext.Token) which is the `(` that started the call.
//
// Returns hypertableCallArguments which carries the structured fields.
// Returns error which is non-nil when the table or dimension argument is malformed or the
// call is unterminated.
func extractHypertableCallArguments(
	p pgext.ParserContext,
	openParen pgext.Token,
) (hypertableCallArguments, error) {
	tableArgument, tableErr := extractStringArgument(p)
	if tableErr != nil {
		return hypertableCallArguments{}, fmt.Errorf("create_hypertable: table argument: %w", tableErr)
	}
	if !consumeArgumentSeparator(p) {
		return hypertableCallArguments{}, dialectkit.NewParseError(p.CurrentToken(), "expected ',' after table argument")
	}

	dimension, dimensionErr := extractDimensionArgument(p)
	if dimensionErr != nil {
		return hypertableCallArguments{}, fmt.Errorf("create_hypertable: dimension argument: %w", dimensionErr)
	}

	extrasText := ""
	if consumeArgumentSeparator(p) {
		captured, extrasErr := captureRemainingCallArguments(p, funcNameCreateHypertable, openParen)
		if extrasErr != nil {
			return hypertableCallArguments{}, extrasErr
		}
		extrasText = captured
	} else if closeErr := expectCallClose(p, funcNameCreateHypertable, openParen); closeErr != nil {
		return hypertableCallArguments{}, closeErr
	}

	return hypertableCallArguments{
		table:            tableArgument,
		timeColumn:       dimension.column,
		dimensionBuilder: dimension.builder,
		extras:           extrasText,
	}, nil
}

// extractDimensionArgument reads the dimension argument of a create_hypertable or
// add_dimension call.
//
// When the cursor sits on a bare identifier or string literal it is treated as the legacy
// column reference. When the cursor sits on an identifier immediately followed by `(` and
// the identifier matches a known dimension builder (`by_range` / `by_hash`), the inner
// column literal is extracted and returned alongside the builder name.
//
// Takes p (pgext.ParserContext) which is positioned at the dimension argument.
//
// Returns dimensionArgument which is the decoded dimension, with column populated and
// builder set for the by_range / by_hash form.
// Returns error which is non-nil when the argument shape is not a recognised literal,
// identifier, or builder call.
func extractDimensionArgument(p pgext.ParserContext) (dimensionArgument, error) {
	current := p.CurrentToken()
	if current.Kind == pgext.TokenIdentifier && isKnownDimensionBuilder(current.Value) && p.Peek().Kind == pgext.TokenLeftParen {
		return extractDimensionBuilderArgument(p)
	}
	if current.Kind != pgext.TokenString && current.Kind != pgext.TokenIdentifier {
		return dimensionArgument{}, dialectkit.NewParseError(current, "expected string literal or identifier")
	}
	p.Advance()

	if castErr := consumeOptionalCast(p); castErr != nil {
		return dimensionArgument{}, castErr
	}
	return dimensionArgument{column: current.Value, builder: ""}, nil
}

// isKnownDimensionBuilder reports whether name is one of the TimescaleDB dimension
// constructor function names. The membership list lives next to the call site so the
// recognised set is defined in one place; the comparison is case-insensitive.
//
// Takes name (string) which is the candidate identifier text.
//
// Returns bool which is true when name names a dimension builder.
func isKnownDimensionBuilder(name string) bool {
	switch strings.ToLower(name) {
	case "by_range", "by_hash":
		return true
	}
	return false
}

// extractDimensionBuilderArgument decodes a `by_range('column', INTERVAL '1 day')` /
// `by_hash('column', N)` form. The first inner argument is required (the partitioning
// column); subsequent inner arguments are consumed opaquely because they describe
// partitioning specifics the catalogue does not interpret.
//
// Takes p (pgext.ParserContext) which is positioned at the builder identifier.
//
// Returns dimensionArgument carrying the column and builder name.
// Returns error on a malformed builder body.
func extractDimensionBuilderArgument(p pgext.ParserContext) (dimensionArgument, error) {
	builderToken := p.Advance()
	builderName := strings.ToLower(builderToken.Value)
	openToken := p.CurrentToken()
	if openToken.Kind != pgext.TokenLeftParen {
		return dimensionArgument{}, dialectkit.NewParseError(openToken, fmt.Sprintf("expected '(' after %s", builderName))
	}
	p.Advance()

	column, columnErr := extractStringArgument(p)
	if columnErr != nil {
		return dimensionArgument{}, fmt.Errorf("%s: column argument: %w", builderName, columnErr)
	}
	if consumeArgumentSeparator(p) {
		if _, drainErr := captureRemainingCallArguments(p, builderName, openToken); drainErr != nil {
			return dimensionArgument{}, drainErr
		}
		return dimensionArgument{column: column, builder: builderName}, nil
	}
	if p.CurrentToken().Kind != pgext.TokenRightParen {
		return dimensionArgument{}, dialectkit.NewParseError(p.CurrentToken(), fmt.Sprintf("%s: expected ')'", builderName))
	}
	p.Advance()
	return dimensionArgument{column: column, builder: builderName}, nil
}

// buildHypertableCallMutation assembles the CatalogueMutation emitted for a parsed SELECT
// create_hypertable() call. The mutation uses MutationAlterTableAlterColumn so the
// catalogue updates the existing relation instead of refusing the duplicate CREATE TABLE
// (the targeted table is created by a prior CREATE TABLE statement).
//
// Takes arguments (hypertableCallArguments) which carries the structured fields extracted
// by extractHypertableCallArguments.
//
// Returns the populated CatalogueMutation.
func buildHypertableCallMutation(arguments hypertableCallArguments) *ast.CatalogueMutation {
	schema, table := splitMaybeSchemaQualified(arguments.table)
	mutation := ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, schema, table, ast.WithEngineSpecific(map[string]string{
		"TIMESCALE_HYPERTABLE":    literalTrue,
		"TIMESCALE_TIME_COLUMN":   arguments.timeColumn,
		"TIMESCALE_ANNOTATE_ONLY": literalTrue,
	}))
	if arguments.dimensionBuilder != "" {
		mutation.EngineSpecific["TIMESCALE_DIMENSION_BUILDER"] = arguments.dimensionBuilder
	}
	if arguments.extras != "" {
		mutation.EngineSpecific["TIMESCALE_CALL_EXTRAS"] = arguments.extras
	}
	return mutation
}

// extractStringArgument reads the table-argument or column-argument position of a
// create_hypertable call.
//
// TimescaleDB requires this argument to be a string literal (`'public.readings'`) or a
// bare identifier; an expression (`some_func()`) is rejected so a caller does not
// silently capture garbage as the table name. The returned text is the lexer-stripped
// token value.
//
// Takes p (pgext.ParserContext) which is positioned at the argument.
//
// Returns string which is the lexer-stripped argument value.
// Returns error when the argument is not a string literal or identifier.
func extractStringArgument(p pgext.ParserContext) (string, error) {
	current := p.CurrentToken()
	if current.Kind != pgext.TokenString && current.Kind != pgext.TokenIdentifier {
		return "", dialectkit.NewParseError(current, "expected string literal or identifier")
	}
	p.Advance()
	value := current.Value

	if current.Kind == pgext.TokenIdentifier {
		for p.CurrentToken().Kind == pgext.TokenDot {
			p.Advance()
			next := p.CurrentToken()
			if next.Kind != pgext.TokenIdentifier && next.Kind != pgext.TokenString {
				return "", dialectkit.NewParseError(next, "expected identifier after '.'")
			}
			p.Advance()
			value = value + "." + next.Value
		}
	}

	if castErr := consumeOptionalCast(p); castErr != nil {
		return "", castErr
	}
	return value, nil
}

// consumeOptionalCast consumes a trailing PostgreSQL `::type` cast when one is present.
//
// Consuming the cast keeps it from blocking the argument separator or close paren that
// follows a hypertable or policy argument. The TimescaleDB documentation (and this file's
// own godoc) presents `create_hypertable('t'::regclass, 'c'::name)` as the canonical
// form, so the extractors tolerate the cast rather than abort. A `::` not followed by a
// type name is left in place. The target type may be schema-qualified
// (`pg_catalog.regclass`) and may carry a balanced parenthesised modifier
// (`varchar(10)`); both are consumed.
//
// Takes p (pgext.ParserContext) which is positioned at the argument.
//
// Returns error when the parenthesised type modifier is not closed, and nil otherwise.
func consumeOptionalCast(p pgext.ParserContext) error {
	if p.CurrentToken().Kind != pgext.TokenCast {
		return nil
	}
	p.Advance()
	if p.CurrentToken().Kind != pgext.TokenIdentifier {
		return nil
	}
	p.Advance()
	for p.CurrentToken().Kind == pgext.TokenDot {
		p.Advance()
		if p.CurrentToken().Kind != pgext.TokenIdentifier {
			return nil
		}
		p.Advance()
	}
	if p.CurrentToken().Kind == pgext.TokenLeftParen {
		return p.SkipParenthesised()
	}
	return nil
}

// consumeArgumentSeparator consumes a `,` between arguments and returns whether one was
// present.
//
// Takes p (pgext.ParserContext) which drives the parse.
//
// Returns bool which is true when a comma separator was consumed.
func consumeArgumentSeparator(p pgext.ParserContext) bool {
	if p.CurrentToken().Kind == pgext.TokenComma {
		p.Advance()
		return true
	}
	return false
}

// splitMaybeSchemaQualified breaks a `schema.table` string into its component parts. When
// no dot is present the schema is empty.
//
// Takes name (string) which is the possibly schema-qualified name.
//
// Returns schema which is the schema part, empty when no dot is present.
// Returns table which is the table part.
func splitMaybeSchemaQualified(name string) (schema string, table string) {
	schema, table, ok := strings.Cut(name, ".")
	if !ok {
		return "", name
	}
	return schema, table
}
