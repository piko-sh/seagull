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

package pgext

import (
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// StatementKind classifies a top-level statement. The postgres dialect's built-in kinds
// are unexported; an extension names its own kinds from StatementKindExtensionBase up.
type StatementKind uint16

const (
	// StatementKindExtensionBase is the lowest StatementKind an extension may claim. The
	// dialect's built-in kinds sit below it; extensions use the reserved range
	// [StatementKindExtensionBase, StatementKindExtensionBase + 1000).
	//
	// When two extensions claim the same statement, the one registered first with
	// postgres.WithStatementExtensions wins. A kind below this base is rejected at
	// classification time, so an extension can never take over a built-in handler by
	// accident.
	StatementKindExtensionBase StatementKind = 1000

	// EngineSpecificHashShardBuckets is the key under which the EngineSpecific map of a
	// CREATE TABLE mutation records that the table's primary key is hash-sharded, as USING
	// HASH declares where the dialect reads index options.
	//
	// Its value is the bucket_count the key's WITH (...) storage parameters give, as
	// written, or empty when they give none.
	EngineSpecificHashShardBuckets = "HASH_SHARD_BUCKETS"

	// EngineSpecificStorageParameterPrefix opens the keys under which the EngineSpecific map
	// of a CREATE TABLE mutation records the storage parameters the statement's WITH (...)
	// clause gives, each followed by the parameter's lower-cased name, such as
	// STORAGE_PARAMETER:fillfactor. The value is the parameter's value as
	// ParserContext.ParseReloptionList gives it.
	EngineSpecificStorageParameterPrefix = "STORAGE_PARAMETER:"
)

// PostParseHook runs after successful DDL dispatch, including extension handlers and
// statements with no handler, so a flavour can attach its own metadata, such as lifting
// the timescaledb.compress keys of an ALTER TABLE SET body into mutation.EngineSpecific.
//
// Hooks run in registration order; a returned error stops the chain and fails the
// statement, wrapped with the hook's place in that order, counted from 0, as in
// "post-parse hook 0: ..." for the first. The mutation is nil when no handler produced
// one, which a hook should tolerate.
type PostParseHook func(p ParserContext, kind StatementKind, mutation *ast.CatalogueMutation) error

// ParserContext is what a StatementExtensionPort or PostParseHook uses to drive the
// postgres parser without re-implementing its helpers.
//
// Keyword matching ignores case and never matches a double-quoted identifier, so a column
// named "select" stays a name. Text-capturing methods slice the statement's source
// between token offsets, so the captured text keeps its quoting, spacing and literals
// exactly as written.
//
// A PostParseHook must not advance the cursor: dispatch has finished, so only the
// read-only methods (CurrentToken, Peek, AtEnd, Tokens, EngineSpecificFromTokens) are
// meaningful there. The cursor-moving methods are for StatementExtensionPort.Parse.
type ParserContext interface {
	// ExpectKeyword consumes the current token, requiring it to be the named keyword.
	//
	// A mismatch is recorded as the statement's syntax error, so the statement fails even
	// when the extension does not propagate the returned error.
	//
	// Takes name (string) which is the keyword the token must match.
	//
	// Returns error when the token is not the keyword.
	ExpectKeyword(name string) error

	// MatchKeyword consumes the current token only when it is the named keyword.
	//
	// Takes name (string) which is the keyword to match, ignoring case.
	//
	// Returns bool which reports whether the keyword was consumed.
	MatchKeyword(name string) bool

	// MatchIfNotExists consumes an IF NOT EXISTS clause when one is next.
	//
	// Returns bool which reports whether the clause was consumed.
	MatchIfNotExists() bool

	// MatchIfExists consumes an IF EXISTS clause when one is next.
	//
	// Returns bool which reports whether the clause was consumed.
	MatchIfExists() bool

	// CurrentToken returns the token at the cursor without consuming it.
	//
	// Returns Token which is the current token, or an end-of-statement token past the end.
	CurrentToken() Token

	// Peek returns the token after the cursor without consuming anything.
	//
	// Returns Token which is the next token, or an end-of-statement token.
	Peek() Token

	// Advance consumes and returns the current token.
	//
	// Returns Token which is the consumed token.
	Advance() Token

	// AtEnd reports whether the cursor has reached the end of the statement.
	//
	// Returns bool which is true at the end of the statement.
	AtEnd() bool

	// ParseQualifiedName parses an optionally schema-qualified name, schema.name or name.
	//
	// Returns string which is the schema, empty when the name has none.
	// Returns string which is the object name.
	// Returns error when the name cannot be parsed.
	ParseQualifiedName() (schema string, name string, err error)

	// ParseColumnList parses a parenthesised list of column names. The opening parenthesis
	// must be at the cursor; the closing one is consumed.
	//
	// Returns []string which holds the column names.
	// Returns error when the list cannot be parsed.
	ParseColumnList() ([]string, error)

	// ParseColumnType parses and normalises the column type at the cursor, consuming its
	// tokens: multi-word names, precision and length modifiers, time zone qualifiers and
	// array suffixes, resolved through the dialect's types exactly as CREATE TABLE resolves
	// them.
	//
	// Returns sqlmodel.SQLType which is the normalised type.
	// Returns int which is the array dimension count, 0 for a non-array type.
	ParseColumnType() (sqlmodel.SQLType, int)

	// ParseTableElements parses the parenthesised column and constraint list of a CREATE
	// TABLE at the cursor exactly as the dialect's CREATE TABLE does, consuming the closing
	// parenthesis, and returns the table the list declares.
	//
	// Each column is typed, and its nullability and default read from its constraints, as
	// CREATE TABLE reads them: a serial or identity column is NOT NULL with a default, and a
	// primary key makes its columns NOT NULL. LIKE elements, table constraints and the index
	// options the dialect allows are read too. A malformed element fails the statement
	// whether or not the extension returns the error.
	//
	// Takes schema (string) which is the table's schema, empty when the name has none.
	// Takes name (string) which is the table's name.
	//
	// Returns *ast.CatalogueMutation which is a MutationCreateTable of the table holding its
	// columns, primary key, constraints and copied tables, with the changes its key makes to
	// columns the list does not declare as its additional mutations.
	// Returns error when the list is missing or malformed.
	ParseTableElements(schema, name string) (*ast.CatalogueMutation, error)

	// SkipParenthesised consumes the parenthesised group at the cursor, the groups nested in
	// it included, and leaves the cursor just after its closing parenthesis.
	//
	// Returns error when the cursor is not on an opening parenthesis or the group is not
	// closed before the end of the statement. An unclosed group fails the statement whether
	// or not the extension returns the error.
	SkipParenthesised() error

	// CaptureUntilBoundary consumes the tokens from the cursor up to the next comma or
	// closing parenthesis outside the groups they open, or up to the end of the statement,
	// and returns their source text exactly as written.
	//
	// Returns string which is the captured text, empty when the cursor is already at such a
	// boundary.
	// Returns error when a group the captured tokens open is not closed before the end of
	// the statement. Such a group fails the statement whether or not the extension returns
	// the error.
	CaptureUntilBoundary() (string, error)

	// ParseReloptionList parses a parenthesised list of key = value storage options.
	//
	// The opening parenthesis must be at the cursor; the closing one is consumed. String
	// values lose their quotes, other values are their source text, keys keep their case,
	// and a key without a value is a boolean option set to true.
	//
	// Returns map[string]string which maps each key to its value.
	// Returns error when the list cannot be parsed.
	ParseReloptionList() (map[string]string, error)

	// ConsumeRemainder advances the cursor to the end of the statement, for an extension
	// that captures the rest of the statement opaquely.
	ConsumeRemainder()

	// ConsumeRemainderAsText returns the source text from the cursor to the end of the
	// statement, exactly as written, and advances to the end.
	//
	// Returns string which is the remaining source text.
	ConsumeRemainderAsText() string

	// EngineSpecificFromTokens returns the source text spanning the tokens from startIndex
	// up to but excluding endIndex, exactly as written.
	//
	// Takes startIndex (int) which is the index of the first token.
	// Takes endIndex (int) which is one past the index of the last token.
	//
	// Returns string which is the source text, or empty for an invalid range.
	EngineSpecificFromTokens(startIndex, endIndex int) string

	// Tokens returns a copy of the statement's tokens, for a hook that inspects the
	// statement's shape after the built-in handler has consumed it.
	//
	// Returns []Token which is a copy the caller may keep or change.
	Tokens() []Token

	// AnalyseViewBody analyses the query body at the cursor without consuming it and returns
	// its analysis, so an extension can type a view-like definition exactly as CREATE VIEW
	// does and then consume the body itself.
	//
	// A body the dialect does not analyse, such as one that is no query or one nested beyond
	// the parse depth limit, gives no analysis and no error: the mutation the extension
	// returns reports it as an unanalysed part, and the view's columns are unknown. A
	// malformed body fails the statement whether or not the extension returns the error.
	//
	// Takes columnNames ([]string) which overlay the inferred column names, as in CREATE
	// VIEW v (a, b) AS ..., or nil to keep them.
	//
	// Returns *ast.RawQueryAnalysis which is the analysed body, or nil when it was not
	// analysed.
	// Returns error when the body is missing or malformed, or its analysis stops, such as
	// when the context is done.
	AnalyseViewBody(columnNames []string) (*ast.RawQueryAnalysis, error)
}
