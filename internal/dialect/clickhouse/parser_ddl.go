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

package clickhouse

import (
	"cmp"
	"fmt"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// engineClauseTTL is the EngineSpecific key for the table TTL clause.
	engineClauseTTL = "TTL"

	// engineClauseComment is the EngineSpecific key for the table COMMENT clause.
	engineClauseComment = "COMMENT"

	// engineClauseEngine is the EngineSpecific key for the ENGINE clause.
	engineClauseEngine = "ENGINE"

	// engineClauseOnCluster is the EngineSpecific key for the ON CLUSTER clause.
	engineClauseOnCluster = "ON_CLUSTER"

	// engineKeyMergeTreeVersionColumn captures the version-column parameter of
	// ReplacingMergeTree(version_column [, is_deleted_column]).
	engineKeyMergeTreeVersionColumn = "MERGETREE_VERSION_COLUMN"

	// engineKeyMergeTreeIsDeletedColumn captures the optional is_deleted column of
	// ReplacingMergeTree(version, is_deleted).
	engineKeyMergeTreeIsDeletedColumn = "MERGETREE_IS_DELETED_COLUMN"

	// engineKeyMergeTreeSignColumn captures the sign column of CollapsingMergeTree(sign) and
	// VersionedCollapsingMergeTree(sign, version).
	engineKeyMergeTreeSignColumn = "MERGETREE_SIGN_COLUMN"

	// engineKeyMergeTreeSummingColumns captures the optional summed-column list of
	// SummingMergeTree([(col, ...)]).
	engineKeyMergeTreeSummingColumns = "MERGETREE_SUMMING_COLUMNS"

	// engineKeyMergeTreeZooPath captures the Keeper/ZooKeeper path of
	// ReplicatedMergeTree(zoo_path, replica_name).
	engineKeyMergeTreeZooPath = "MERGETREE_ZOO_PATH"

	// engineKeyMergeTreeReplicaName captures the replica name of
	// ReplicatedMergeTree(zoo_path, replica_name).
	engineKeyMergeTreeReplicaName = "MERGETREE_REPLICA_NAME"
)

var (
	// mergeTreeFamilyHandlers dispatches each known MergeTree-family engine name to its
	// argument-extraction function. Engines outside the table (Log, Memory, plain MergeTree,
	// JOIN/SET) are no-ops; the catch-all ENGINE key populated by the caller still records
	// the full text for those engines.
	mergeTreeFamilyHandlers = map[string]func(*ast.CatalogueMutation, string, []string){
		"replacingmergetree":           applyReplacingMergeTreeArgs,
		"collapsingmergetree":          applyCollapsingMergeTreeArgs,
		"versionedcollapsingmergetree": applyVersionedCollapsingMergeTreeArgs,
		"summingmergetree":             applySummingMergeTreeArgs,
		"replicatedmergetree":          applyReplicatedMergeTreeArgs,
		"replicatedreplacingmergetree": applyReplicatedReplacingMergeTreeArgs,
	}
)

// createTableEntry carries the result of one CREATE TABLE body entry. The IsColumn and
// IsPrimaryKey flags identify whether Columns or PrimaryKey is populated; both are false
// for a skipped secondary declaration.
type createTableEntry struct {
	// PrimaryKey holds the table-level primary key columns when IsPrimaryKey is true.
	PrimaryKey []string

	// Columns holds the parsed column definition when IsColumn is true, or the columns a
	// Nested column is stored as.
	Columns []sqlmodel.Column

	// IsColumn reports whether Columns carries a parsed column definition.
	IsColumn bool

	// IsPrimaryKey reports whether PrimaryKey carries a table-level primary key.
	IsPrimaryKey bool
}

// parseCreateTable handles a ClickHouse CREATE TABLE statement and its engine clauses.
//
// The grammar covers `CREATE [OR REPLACE] [TEMPORARY] TABLE [IF NOT EXISTS]
// [database.]table (column_list) ENGINE = ... PARTITION BY ... ORDER BY ... SETTINGS ...
// TTL ...`. The engine declaration and modifier clauses are reconstructed from tokens
// into EngineSpecific for callers to inspect; original formatting and quoting are not
// retained.
//
// Returns *ast.CatalogueMutation with Kind=MutationCreateTable and the parsed column
// metadata.
// Returns error on malformed input.
func (p *parser) parseCreateTable() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("CREATE"); err != nil {
		return nil, err
	}
	p.skipCreatePrefixesInParser()
	if !p.MatchKeyword("TABLE") {
		return nil, dialectkit.NewParseError(p.Current(), "expected TABLE keyword")
	}
	ifNotExists := p.matchIfNotExists()

	database, name, nameError := p.parseDatabaseQualifiedName()
	if nameError != nil {
		return nil, fmt.Errorf("parsing table name: %w", nameError)
	}

	mutation := ast.NewCatalogueMutation(
		ast.MutationCreateTable,
		database,
		name,
		ast.WithEngineSpecific(map[string]string{}),
		ast.WithIfNotExists(ifNotExists),
	)

	if cluster := p.matchOnCluster(); cluster != "" {
		mutation.EngineSpecific[engineClauseOnCluster] = cluster
	}

	if p.Current().Kind != tokenLeftParen {
		if err := p.parseCreateTableWithoutColumnList(mutation); err != nil {
			return nil, err
		}
		return mutation, nil
	}

	columns, primaryKey, parseErr := p.parseCreateTableBody()
	if parseErr != nil {
		return nil, parseErr
	}
	mutation.Columns = columns
	mutation.PrimaryKey = primaryKey

	if err := p.parseTableEngineClauses(mutation); err != nil {
		return nil, err
	}
	if p.MatchKeyword("AS") {
		p.consumeRemainder()
	}

	return mutation, nil
}

// parseCreateTableWithoutColumnList handles the CREATE TABLE forms that take their
// columns from an AS clause instead of a parenthesised column list.
//
// Recognises both `CREATE TABLE t AS source [ENGINE ...]` and `CREATE TABLE t ENGINE =
// ... [ORDER BY ...] [EMPTY] AS SELECT ...`, in which the engine clauses precede the AS
// body. The engine clauses are captured into the mutation's EngineSpecific map either
// way.
//
// Takes mutation (*ast.CatalogueMutation) which receives the columns and engine clauses.
//
// Returns error when no AS clause follows the engine clauses or the AS body is malformed.
func (p *parser) parseCreateTableWithoutColumnList(mutation *ast.CatalogueMutation) error {
	if err := p.parseTableEngineClauses(mutation); err != nil {
		return err
	}
	if p.MatchKeyword("EMPTY") {
		mutation.EngineSpecific["EMPTY"] = "true"
	}
	if !p.MatchKeyword("AS") {
		return dialectkit.NewParseError(p.Current(), "expected '(' or AS")
	}
	return p.populateCTASColumns(mutation)
}

// populateCTASColumns lifts a CREATE TABLE AS body into the mutation's column list.
//
// The projection of a `CREATE TABLE x AS SELECT ...` body becomes the column list. When
// the body instead names a source table (the clone form `CREATE TABLE x AS source`), the
// source table identifier is captured under the EngineSpecific `CTAS_SOURCE_TABLE` (and
// `CTAS_SOURCE_SCHEMA` when qualified). Trailing engine clauses are recorded and any
// remaining tokens are consumed. Failure to detect a SELECT body is not treated as an
// error because the source-table form is also valid.
//
// Takes mutation (*ast.CatalogueMutation) which receives the lifted columns.
//
// Returns error when the SELECT body, source table name, or trailing engine clauses
// cannot be parsed.
func (p *parser) populateCTASColumns(mutation *ast.CatalogueMutation) error {
	if p.IsKeyword("SELECT") || p.IsKeyword("WITH") {
		return p.populateCTASFromSelect(mutation)
	}
	if p.Current().Kind == tokenIdentifier {
		schema, name, err := p.parseDatabaseQualifiedName()
		if err != nil {
			return err
		}
		mutation.EngineSpecific["CTAS_SOURCE_TABLE"] = name
		if schema != "" {
			mutation.EngineSpecific["CTAS_SOURCE_SCHEMA"] = schema
		}
	}
	if err := p.parseTableEngineClauses(mutation); err != nil {
		return err
	}
	p.consumeRemainder()
	return nil
}

// populateCTASFromSelect analyses the remaining tokens as a SELECT through a child parser
// and lifts the analyser's output columns into the mutation's column list as nullable
// catalogue columns of unknown type.
//
// The remainder of the statement is consumed so the caller continues from a known
// position.
//
// Takes mutation (*ast.CatalogueMutation) which receives the analysed columns.
//
// Returns error when the SELECT body is malformed.
func (p *parser) populateCTASFromSelect(mutation *ast.CatalogueMutation) error {
	nested := p.newChildParser(p.Tokens[p.Position:])
	analysis, analyseErr := nested.analyseSelect()
	p.consumeRemainder()
	if analyseErr != nil {
		return fmt.Errorf("analysing CREATE TABLE AS body: %w", analyseErr)
	}
	mutation.Columns = columnsFromCTASAnalysis(analysis)
	return nil
}

// skipCreatePrefixesInParser advances past OR REPLACE / TEMPORARY / TEMP modifiers if
// present. Symmetric to skipCreatePrefixes used by classifyStatement but operates on the
// live parser cursor.
func (p *parser) skipCreatePrefixesInParser() {
	for {
		if p.MatchKeyword("OR") {
			if !p.MatchKeyword("REPLACE") {
				return
			}
			continue
		}
		if p.MatchKeyword("TEMPORARY") || p.MatchKeyword("TEMP") {
			continue
		}
		return
	}
}

// matchIfNotExists optionally consumes `IF NOT EXISTS`.
//
// Returns bool which is true when the clause was present.
func (p *parser) matchIfNotExists() bool {
	if !p.MatchKeyword("IF") {
		return false
	}
	p.MatchKeyword("NOT")
	p.MatchKeyword("EXISTS")
	return true
}

// matchIfExists optionally consumes `IF EXISTS`.
//
// Returns bool which is true when the clause was present.
func (p *parser) matchIfExists() bool {
	if !p.MatchKeyword("IF") {
		return false
	}
	p.MatchKeyword("EXISTS")
	return true
}

// parsePlacement consumes an optional `AFTER name` or `FIRST` column placement.
//
// Returns ast.Placement which says where the column goes, the default when neither is
// present.
// Returns error when AFTER names no column.
func (p *parser) parsePlacement() (ast.Placement, error) {
	if p.MatchKeyword(keywordAfter) {
		anchor, err := p.parseColumnPath()
		if err != nil {
			return ast.Placement{}, err
		}
		return ast.Placement{Anchor: anchor, Kind: ast.PlacementAfter}, nil
	}
	if p.MatchKeyword("FIRST") {
		return ast.Placement{Anchor: "", Kind: ast.PlacementFirst}, nil
	}
	return ast.Placement{Anchor: "", Kind: ast.PlacementDefault}, nil
}

// matchOnCluster optionally consumes an `ON CLUSTER <name>` clause.
//
// Returns string which is the cluster name, or empty when no clause was present.
func (p *parser) matchOnCluster() string {
	saved := p.Position
	if !p.MatchKeyword("ON") {
		return ""
	}
	if !p.MatchKeyword("CLUSTER") {
		p.Position = saved
		return ""
	}
	if p.Current().Kind == tokenIdentifier {
		name := p.Current().Value
		p.Advance()
		return name
	}
	if p.Current().Kind == tokenString {
		name := p.Current().Value
		p.Advance()
		return name
	}

	p.Position = saved
	return ""
}

// parseCreateTableBody reads the parenthesised column list.
//
// ClickHouse column constraints differ from postgres: there is no PRIMARY KEY / UNIQUE /
// REFERENCES / CHECK at the column level; instead columns accept DEFAULT, MATERIALIZED,
// ALIAS, EPHEMERAL, CODEC, TTL, and COMMENT. PRIMARY KEY is declared as a table-level
// clause or implicitly via the table-engine ORDER BY.
//
// Returns []sqlmodel.Column which are the parsed column definitions.
// Returns []string which are the table-level primary key columns, or nil when none.
// Returns error which is non-nil on malformed input.
func (p *parser) parseCreateTableBody() ([]sqlmodel.Column, []string, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, nil, dialectkit.NewParseError(p.Current(), "expected '('")
	}
	p.Advance()

	var columns []sqlmodel.Column
	var primaryKey []string

	for {
		if p.Current().Kind == tokenRightParen {
			p.Advance()
			return columns, primaryKey, nil
		}

		entry, err := p.parseCreateTableEntry()
		if err != nil {
			return nil, nil, err
		}
		if entry.IsColumn {
			columns = append(columns, entry.Columns...)
		}
		if entry.IsPrimaryKey {
			primaryKey = entry.PrimaryKey
		}

		switch p.Current().Kind {
		case tokenComma:
			p.Advance()
			continue
		case tokenRightParen:
			p.Advance()
			return columns, primaryKey, nil
		default:
			return nil, nil, dialectkit.NewParseError(p.Current(), "expected ',' or ')'")
		}
	}
}

// parseCreateTableEntry handles one entry inside a CREATE TABLE body.
//
// An entry is a secondary declaration (INDEX / CONSTRAINT / PROJECTION), a table-level
// PRIMARY KEY, or a column definition. The returned entry's IsColumn / IsPrimaryKey flags
// tell the caller which slot is populated; secondary declarations leave both flags false
// and the caller skips them silently.
//
// Returns createTableEntry which is the captured entry, with IsColumn true when Columns
// is populated and IsPrimaryKey true when PrimaryKey is populated.
// Returns error which is non-nil for malformed input.
func (p *parser) parseCreateTableEntry() (createTableEntry, error) {
	if p.IsKeyword("INDEX", "CONSTRAINT", "PROJECTION") {
		p.skipUntilCommaOrCloseParen()
		return createTableEntry{}, nil
	}
	parsedKeys, isPrimaryKey, keyErr := p.tryParsePrimaryKeyClause()
	if keyErr != nil {
		return createTableEntry{}, keyErr
	}
	if isPrimaryKey {
		return createTableEntry{
			PrimaryKey:   parsedKeys,
			IsPrimaryKey: true,
			Columns:      nil,
			IsColumn:     false,
		}, nil
	}
	parsedColumns, columnErr := p.parseTableColumns()
	if columnErr != nil {
		return createTableEntry{}, columnErr
	}
	return createTableEntry{Columns: parsedColumns, IsColumn: true, PrimaryKey: nil, IsPrimaryKey: false}, nil
}

// tryParsePrimaryKeyClause checks for a table-level PRIMARY KEY declaration.
//
// Returns []string which are the primary key columns, or nil when the cursor is not on a
// PRIMARY KEY clause so the caller can fall through to column parsing.
// Returns bool which is true when a PRIMARY KEY clause was consumed.
// Returns error which is non-nil on malformed input within the clause.
func (p *parser) tryParsePrimaryKeyClause() ([]string, bool, error) {
	if !p.IsKeyword("PRIMARY") {
		return nil, false, nil
	}
	saved := p.Position
	p.Advance()
	if !p.MatchKeyword("KEY") {
		p.Position = saved
		return nil, false, nil
	}
	if p.Current().Kind != tokenLeftParen {
		columnName, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, false, err
		}
		return []string{columnName}, true, nil
	}
	p.Advance()
	var columns []string
	for {
		columnName, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, false, err
		}
		columns = append(columns, columnName)
		if p.Current().Kind == tokenComma {
			p.Advance()
			continue
		}
		if p.Current().Kind == tokenRightParen {
			p.Advance()
			return columns, true, nil
		}
		return nil, false, dialectkit.NewParseError(p.Current(), "expected ',' or ')' in PRIMARY KEY")
	}
}

// parseClickHouseColumn reads one column definition.
//
// The column shape is "<name> <type> [NULL | NOT NULL] [DEFAULT <expr> | MATERIALIZED
// <expr> | ALIAS <expr> | EPHEMERAL [<expr>]] [CODEC(...)] [TTL <expr>] [COMMENT
// <string>]". The parser captures the name, type, nullability, default-presence and
// generated-column flags, visibility, and comments. Modifier expressions and storage
// settings are consumed without retaining their bodies.
//
// ClickHouse permits the postgres-style `col Int32 NOT NULL` and `col Int32 NULL`
// suffixes alongside the wrapper-form `Nullable(Int32)`. NULL on a non-Nullable inner
// type promotes the column to nullable. The parser consumes NOT NULL without changing the
// type's nullability or rejecting a conflicting Nullable wrapper.
//
// Returns sqlmodel.Column which is the parsed column definition.
// Returns error when the column name or type cannot be parsed.
func (p *parser) parseClickHouseColumn() (sqlmodel.Column, error) {
	column, _, err := p.parseColumnDefinition()
	return column, err
}

// parseColumnDefinition reads one column definition as parseClickHouseColumn describes,
// and reports whether its type was declared as Nested, which the parsed type, an array of
// tuples, no longer tells apart.
//
// Returns sqlmodel.Column which is the parsed column definition.
// Returns bool which is true when the type was declared as Nested(...).
// Returns error when the column name or type cannot be parsed.
func (p *parser) parseColumnDefinition() (sqlmodel.Column, bool, error) {
	columnName, err := p.parseColumnPath()
	if err != nil {
		return sqlmodel.Column{}, false, fmt.Errorf("parsing column name: %w", err)
	}
	typeName, err := p.readClickHouseTypeText()
	if err != nil {
		return sqlmodel.Column{}, false, fmt.Errorf("reading column %q type: %w", columnName, err)
	}
	typeResult, parseErr := p.types.parse(typeName)
	if parseErr != nil {
		return sqlmodel.Column{}, false, fmt.Errorf("parsing column %q type %q: %w", columnName, typeName, parseErr)
	}

	column := sqlmodel.Column{}
	column.Name = columnName
	column.SQLType = typeResult.SQLType
	column.Nullable = typeResult.Nullable

	p.applyColumnNullSuffix(&column)

	for p.applyColumnModifier(&column) {
	}
	return column, declaresNested(typeName), nil
}

// applyColumnNullSuffix consumes an optional `NULL` / `NOT NULL` suffix after a type.
//
// NULL promotes the column to nullable. NOT NULL is consumed without changing the
// column's existing nullability. The cursor is left unchanged when no suffix is present.
//
// Takes column (*sqlmodel.Column) whose nullability is updated in place.
func (p *parser) applyColumnNullSuffix(column *sqlmodel.Column) {
	if p.MatchKeyword("NOT") {
		if !p.MatchKeyword("NULL") {
			p.Position--
		}
		return
	}
	if p.MatchKeyword("NULL") {
		column.Nullable = true
		if !column.SQLType.Nullable {
			column.SQLType.Nullable = true
		}
	}
}

// applyColumnModifier matches one column-modifier clause (DEFAULT, MATERIALIZED, ALIAS,
// EPHEMERAL, CODEC, TTL, COMMENT) and updates the column accordingly. A MATERIALIZED,
// ALIAS or EPHEMERAL column is hidden: SELECT * and an INSERT without a column list leave
// it out, while a query may still name a MATERIALIZED or ALIAS column.
//
// Takes column (*sqlmodel.Column) which is updated in place from the matched modifier.
//
// Returns bool which is false when the cursor is no longer on a recognised modifier,
// signalling the caller to stop iterating.
func (p *parser) applyColumnModifier(column *sqlmodel.Column) bool {
	switch {
	case p.MatchKeyword("DEFAULT"):
		column.HasDefault = true
		p.skipColumnModifierExpression()
	case p.MatchKeyword("MATERIALIZED"):
		column.IsGenerated = true
		column.GeneratedKind = sqlmodel.GeneratedKindStored
		column.Hidden = true
		p.skipColumnModifierExpression()
	case p.MatchKeyword("ALIAS"):
		column.IsGenerated = true
		column.GeneratedKind = sqlmodel.GeneratedKindVirtual
		column.Hidden = true
		p.skipColumnModifierExpression()
	case p.MatchKeyword("EPHEMERAL"):
		column.HasDefault = true
		column.Hidden = true
		if p.Current().Kind != tokenComma && p.Current().Kind != tokenRightParen {
			p.skipColumnModifierExpression()
		}
	case p.MatchKeyword("CODEC"):
		if err := p.skipParenthesised(); err != nil {
			p.recordSyntaxError(err)
		}
	case p.MatchKeyword("TTL"):
		p.skipColumnModifierExpression()
	case p.MatchKeyword("COMMENT"):
		if p.Current().Kind == tokenString {
			column.Comment = p.Current().Value
			p.Advance()
		}
	default:
		return false
	}
	return true
}

// readClickHouseTypeText collects the raw text of a column type, including nested wrapper
// parens. The text is then handed to parseClickHouseType for structured parsing.
//
// The body is split into readNestedTypeBody which handles the inner paren-balanced scan;
// readClickHouseTypeText itself just loops over chained parenthesised groups so that
// types like `Decimal(P,S)(modifier)` are captured intact.
//
// Returns the raw type text and advances the cursor past the type.
func (p *parser) readClickHouseTypeText() (string, error) {
	var builder strings.Builder
	first := p.Current()
	if first.Kind != tokenIdentifier {
		return "", dialectkit.NewParseError(first, "expected type identifier")
	}
	builder.WriteString(first.Value)
	p.Advance()

	for p.Current().Kind == tokenLeftParen {
		builder.WriteByte('(')
		p.Advance()
		p.readNestedTypeBody(&builder)
	}

	return builder.String(), nil
}

// readNestedTypeBody scans a balanced parenthesised body, writing each token's text.
//
// The cursor is left positioned past the closing `)`. Used by readClickHouseTypeText to
// collect everything between the outermost parens of a type modifier.
//
// Takes builder (*strings.Builder) which is the accumulator for the type's text.
func (p *parser) readNestedTypeBody(builder *strings.Builder) {
	depth := 1

	previous := tokenLeftParen
	for depth > 0 && !p.AtEnd() {
		current := p.Current()
		switch current.Kind {
		case tokenLeftParen:
			depth++
			builder.WriteByte('(')
		case tokenRightParen:
			depth--
			builder.WriteByte(')')
			if depth == 0 {
				p.Advance()
				return
			}
		case tokenComma:
			builder.WriteString(", ")
		case tokenString:
			if isValueLikeToken(previous) {
				builder.WriteByte(' ')
			}
			builder.WriteByte('\'')
			builder.WriteString(escapeClickHouseStringBody(current.Value))
			builder.WriteByte('\'')
		case tokenOperator, tokenNumber, tokenIdentifier:
			if current.Kind != tokenOperator && isValueLikeToken(previous) {
				builder.WriteByte(' ')
			}
			builder.WriteString(current.Value)
		case tokenDot:
			builder.WriteByte('.')
		default:
		}
		previous = current.Kind
		p.Advance()
	}
}

// skipColumnModifierExpression advances past a column-modifier expression (DEFAULT, TTL).
//
// The expression is parsed only for forward-progress: the parser consumes until it hits a
// top-level comma or close paren. Nested parens are honoured.
func (p *parser) skipColumnModifierExpression() {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && (current.Kind == tokenComma || current.Kind == tokenRightParen) {
			return
		}
		if p.IsKeyword("DEFAULT", "MATERIALIZED", "ALIAS", "EPHEMERAL", "CODEC", "TTL", "COMMENT") && depth == 0 {
			return
		}
		switch current.Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		default:
		}
		p.Advance()
	}
}

// skipUntilCommaOrCloseParen advances past tokens until reaching a top-level comma or
// close paren. Used to skip INDEX / CONSTRAINT / PROJECTION declarations inside a CREATE
// TABLE body that we do not track in the catalogue.
func (p *parser) skipUntilCommaOrCloseParen() {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && (current.Kind == tokenComma || current.Kind == tokenRightParen) {
			return
		}
		switch current.Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		default:
		}
		p.Advance()
	}
}

// parseTableEngineClauses captures the engine-tail clauses into EngineSpecific.
//
// The clauses are `ENGINE = MergeTree(...) PARTITION BY ... ORDER BY ... SAMPLE BY ...
// PRIMARY KEY ... TTL ... SETTINGS ...`. Each clause is reconstructed from tokens and
// stored under its keyword. The loop stops without consuming the first token that starts
// no clause (an AS body, for example), leaving the caller to handle it.
//
// Takes mutation (*ast.CatalogueMutation) whose EngineSpecific map is populated.
//
// Returns error when a clause keyword is followed by malformed content (for example
// SETTINGS without a value list).
func (p *parser) parseTableEngineClauses(mutation *ast.CatalogueMutation) error {
	for !p.AtEnd() {
		handled, err := p.parseSingleEngineClause(mutation)
		if err != nil {
			return err
		}
		if !handled {
			return nil
		}
	}
	return nil
}

// parseSingleEngineClause matches one engine-tail clause and writes it into the
// mutation's EngineSpecific map. Reports handled=false when the cursor is no longer on a
// recognised clause, which signals the outer loop to stop.
//
// Takes mutation (*ast.CatalogueMutation), the in-flight mutation to populate.
//
// Returns handled (bool), true when one clause was consumed.
// Returns error when the matched clause is malformed.
func (p *parser) parseSingleEngineClause(mutation *ast.CatalogueMutation) (handled bool, err error) {
	switch {
	case p.MatchKeyword(engineClauseEngine):
		return p.captureClauseInto(mutation, engineClauseEngine)
	case p.IsKeyword("PARTITION"):
		return p.captureByClauseInto(mutation, "PARTITION", "PARTITION_BY")
	case p.IsKeyword("ORDER"):
		if captureErr := p.captureOrderByClause(mutation); captureErr != nil {
			return false, captureErr
		}
		return true, nil
	case p.IsKeyword("PRIMARY"):
		return p.capturePrimaryKeyClause(mutation)
	case p.IsKeyword("SAMPLE"):
		return p.captureByClauseInto(mutation, "SAMPLE", "SAMPLE_BY")
	case p.MatchKeyword("TTL"):
		mutation.EngineSpecific[engineClauseTTL] = p.captureExpressionUntilClauseKeyword()
		return true, nil
	case p.MatchKeyword("SETTINGS"):
		mutation.EngineSpecific["SETTINGS"] = p.captureExpressionUntilClauseKeyword()
		return true, nil
	case p.MatchKeyword("COMMENT"):
		if p.Current().Kind == tokenString {
			mutation.EngineSpecific[engineClauseComment] = p.Current().Value
			mutation.Comment = p.Current().Value
			p.Advance()
		}
		return true, nil
	default:
		return false, nil
	}
}

// captureClauseInto reads the value of an `<keyword> = <expression>` clause and stores it
// under the given EngineSpecific key.
//
// Takes mutation (*ast.CatalogueMutation), the mutation to populate.
// Takes engineKey (string), the EngineSpecific map key.
//
// Returns handled (bool), true on success.
// Returns error which is non-nil if the clause cannot be captured.
func (p *parser) captureClauseInto(mutation *ast.CatalogueMutation, engineKey string) (bool, error) {
	value := p.captureClauseValue()
	mutation.EngineSpecific[engineKey] = value
	if engineKey == engineClauseEngine {
		extractMergeTreeFamilyParams(mutation, value)
	}
	return true, nil
}

// captureByClauseInto handles the family of `<keyword> BY <expression>` clauses
// (PARTITION BY, SAMPLE BY). It advances past the leading keyword, requires BY, then
// captures the expression body into the supplied EngineSpecific key.
//
// Takes mutation (*ast.CatalogueMutation), the mutation to populate.
// Takes keywordName (string), the leading clause keyword name (used for the error
// message).
// Takes engineKey (string), the EngineSpecific map key.
//
// Returns handled (bool), true when the clause was consumed.
// Returns error when BY did not follow the leading keyword.
func (p *parser) captureByClauseInto(mutation *ast.CatalogueMutation, keywordName string, engineKey string) (bool, error) {
	p.Advance()
	if !p.MatchKeyword("BY") {
		return false, dialectkit.NewParseError(p.Current(), "expected BY after "+keywordName)
	}
	mutation.EngineSpecific[engineKey] = p.captureExpressionUntilClauseKeyword()
	return true, nil
}

// captureOrderByClause is a specialisation of captureByClauseInto for ORDER BY, which
// additionally infers the primary key from the order expression when no explicit PRIMARY
// KEY clause has been seen.
//
// Takes mutation (*ast.CatalogueMutation) whose EngineSpecific and PrimaryKey are
// populated.
//
// Returns error when BY did not follow ORDER.
func (p *parser) captureOrderByClause(mutation *ast.CatalogueMutation) error {
	p.Advance()
	if !p.MatchKeyword("BY") {
		return dialectkit.NewParseError(p.Current(), "expected BY after ORDER")
	}
	value := p.captureExpressionUntilClauseKeyword()
	mutation.EngineSpecific["ORDER_BY"] = value
	if len(mutation.PrimaryKey) == 0 {
		mutation.PrimaryKey = inferPrimaryKeyFromOrderBy(value)
	}
	return nil
}

// capturePrimaryKeyClause matches the optional table-level PRIMARY KEY clause that
// follows ORDER BY in ClickHouse engine declarations. When PRIMARY is not followed by
// KEY, the parser rewinds and reports the loop should stop, because PRIMARY without KEY
// is not a valid clause keyword in this grammar position.
//
// Takes mutation (*ast.CatalogueMutation) whose EngineSpecific map is populated.
//
// Returns bool which is true when a PRIMARY KEY clause was consumed.
// Returns error when the clause is malformed.
func (p *parser) capturePrimaryKeyClause(mutation *ast.CatalogueMutation) (bool, error) {
	saved := p.Position
	p.Advance()
	if !p.MatchKeyword("KEY") {
		p.Position = saved
		return false, nil
	}
	mutation.EngineSpecific["PRIMARY_KEY"] = p.captureExpressionUntilClauseKeyword()
	return true, nil
}

// captureClauseValue captures the value of a clause like `ENGINE = MergeTree()`. Reads
// the `=`, then collects everything up to the next top-level clause keyword.
//
// Returns string which is the captured clause value text.
func (p *parser) captureClauseValue() string {
	if p.Current().Kind == tokenOperator && p.Current().Value == "=" {
		p.Advance()
	}
	return p.captureExpressionUntilClauseKeyword()
}

// captureExpressionUntilClauseKeyword collects tokens into a flat textual representation
// until it reaches a top-level clause keyword (PARTITION, ORDER, PRIMARY, SAMPLE, TTL,
// SETTINGS, COMMENT, AS) or end-of-input. Nested parens are honoured.
//
// Returns string which is the captured expression text up to the clause boundary.
func (p *parser) captureExpressionUntilClauseKeyword() string {
	var builder strings.Builder
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && current.Kind == tokenIdentifier && isClauseKeyword(current) {
			return builder.String()
		}
		depth = writeClauseToken(&builder, current, depth)
		p.Advance()
	}
	return builder.String()
}

// consumeRemainder advances the cursor to end-of-input. Used for CTAS tails and other
// constructs the parser does not materially analyse.
func (p *parser) consumeRemainder() {
	for !p.AtEnd() {
		p.Advance()
	}
}

// consumeRemainderAsText is the text-capturing variant of consumeRemainder. It consumes
// every remaining token in the statement and returns the concatenated source text with
// identifier quoting preserved.
//
// LOSSY ROUND-TRIP: tokens are joined with single-space separators, so qualified-name
// forms such as `a.b` re-emit as `a . b` (the dot is its own token and is sandwiched
// between spaces). Callers that require lossless reconstruction must re-parse the
// original SQL rather than relying on the captured text.
//
// Returns string which is the concatenated remaining source text, trimmed of surrounding
// whitespace.
func (p *parser) consumeRemainderAsText() string {
	var builder strings.Builder
	for !p.AtEnd() {
		writeTokenAsSourceText(&builder, p.Current())
		builder.WriteByte(' ')
		p.Advance()
	}
	return strings.TrimSpace(builder.String())
}

// consumeTextUntilReturning consumes the rest of the statement up to a top-level
// RETURNING keyword, or its end, and returns the consumed tokens as source text.
//
// Returns string which is the consumed text, trimmed.
func (p *parser) consumeTextUntilReturning() string {
	var builder strings.Builder
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && current.IsKeyword(tokenIdentifier, kwReturning) {
			break
		}
		writeTokenAsSourceText(&builder, current)
		builder.WriteByte(' ')
		depth = nestingAfter(current, depth)
		p.Advance()
	}
	return strings.TrimSpace(builder.String())
}

// consumeUntilTopLevelComma advances the cursor to the next top-level comma or input end.
//
// The top-level comma (depth 0 with respect to parentheses) is left on the stream for the
// caller to handle. Used by sub-parsers that operate within a comma-separated list (for
// example multi-action ALTER TABLE statements) where consumeRemainder would inadvertently
// consume sibling clauses.
func (p *parser) consumeUntilTopLevelComma() {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && current.Kind == tokenComma {
			return
		}
		switch current.Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			if depth == 0 {
				return
			}
			depth--
		default:
		}
		p.Advance()
	}
}

// consumeUntilTopLevelCommaAsText is the text variant of consumeUntilTopLevelComma.
//
// It consumes tokens up to the next top-level comma with identifier quoting preserved.
// The trailing comma is left on the stream so the outer multi-action loop continues. A
// labelled break exits the outer loop from inside the per-token switch. A bare `break`
// inside the switch would only fall through to the next statement after the switch (the
// writer and advance calls), which would consume the stopping token and drive the cursor
// past the boundary the caller is meant to see.
//
// Returns string which is the captured source text up to the comma, trimmed of any
// surrounding whitespace.
func (p *parser) consumeUntilTopLevelCommaAsText() string {
	var builder strings.Builder
	depth := 0
scan:
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && current.Kind == tokenComma {
			break scan
		}
		switch current.Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			if depth == 0 {
				break scan
			}
			depth--
		default:
		}
		writeTokenAsSourceText(&builder, current)
		builder.WriteByte(' ')
		p.Advance()
	}
	return strings.TrimSpace(builder.String())
}

// parseDropTable handles `DROP TABLE [IF EXISTS] [db.]table`.
//
// It produces a CatalogueMutation with MutationDropTable kind; the catalogue builder uses
// this to remove the table from the schema.
//
// Returns *ast.CatalogueMutation with Kind=MutationDropTable.
// Returns error on malformed input.
func (p *parser) parseDropTable() (*ast.CatalogueMutation, error) {
	return p.parseDropQualifiedObject("TABLE", ast.MutationDropTable)
}

// parseDropView handles `DROP VIEW [IF EXISTS] [db.]view [ON CLUSTER c]`.
//
// Returns *ast.CatalogueMutation with Kind=MutationDropView.
// Returns error on malformed input.
func (p *parser) parseDropView() (*ast.CatalogueMutation, error) {
	return p.parseDropQualifiedObject("VIEW", ast.MutationDropView)
}

// parseDropQualifiedObject is the shared body of parseDropTable and parseDropView.
// ClickHouse drops both TABLE and VIEW with the same grammar (`DROP {TABLE|VIEW} [IF
// EXISTS] [db.]name [ON CLUSTER c] [SYNC]`), so the only difference between the two
// callers is the object keyword and the resulting mutation kind.
//
// Takes objectKeyword (string), the keyword that follows DROP (TABLE / VIEW).
// Takes mutationKind (ast.MutationKind), the kind to stamp onto the produced mutation.
//
// Returns the parsed mutation, with EngineSpecific populated when an ON CLUSTER clause
// was present and IfExists set when the statement said IF EXISTS.
func (p *parser) parseDropQualifiedObject(
	objectKeyword string,
	mutationKind ast.MutationKind,
) (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence("DROP", objectKeyword); err != nil {
		return nil, err
	}
	ifExists := p.matchIfExists()
	database, name, err := p.parseDatabaseQualifiedName()
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(mutationKind, database, name, ast.WithIfExists(ifExists))
	if cluster := p.matchOnCluster(); cluster != "" {
		mutation.EngineSpecific = map[string]string{engineClauseOnCluster: cluster}
	}
	p.MatchKeyword("SYNC")
	return mutation, nil
}

// parseDropDatabase handles `DROP DATABASE [IF EXISTS] name [ON CLUSTER c]`. Modelled as
// a DropSchema mutation since ClickHouse databases map onto the catalogue's schema
// concept.
//
// Returns *ast.CatalogueMutation with Kind=MutationDropSchema.
// Returns error on malformed input.
func (p *parser) parseDropDatabase() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence("DROP", "DATABASE"); err != nil {
		return nil, err
	}
	ifExists := p.matchIfExists()
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(ast.MutationDropSchema, name, "", ast.WithIfExists(ifExists))
	if cluster := p.matchOnCluster(); cluster != "" {
		mutation.EngineSpecific = map[string]string{engineClauseOnCluster: cluster}
	}
	p.MatchKeyword("SYNC")
	return mutation, nil
}

// parseCreateDatabase handles `CREATE DATABASE [IF NOT EXISTS] name [ON CLUSTER c]
// [ENGINE = ...]`.
//
// Returns *ast.CatalogueMutation with Kind=MutationCreateSchema.
// Returns error on malformed input.
func (p *parser) parseCreateDatabase() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("CREATE"); err != nil {
		return nil, err
	}
	p.skipCreatePrefixesInParser()
	if _, err := p.ExpectKeyword("DATABASE"); err != nil {
		return nil, err
	}
	p.matchIfNotExists()
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(ast.MutationCreateSchema, name, "")
	if cluster := p.matchOnCluster(); cluster != "" {
		mutation.EngineSpecific = map[string]string{engineClauseOnCluster: cluster}
	}

	if p.MatchKeyword(engineClauseEngine) {
		if mutation.EngineSpecific == nil {
			mutation.EngineSpecific = map[string]string{}
		}
		mutation.EngineSpecific[engineClauseEngine] = p.captureClauseValue()
	}
	return mutation, nil
}

// columnsFromCTASAnalysis converts the analyser's output columns into catalogue columns.
//
// Each column's nullability defaults to true because the parsed projection has not been
// resolved against the source tables.
//
// Takes analysis (*ast.RawQueryAnalysis) which holds the analysed output columns.
//
// Returns []sqlmodel.Column which are the catalogue columns, each nullable by default.
func columnsFromCTASAnalysis(analysis *ast.RawQueryAnalysis) []sqlmodel.Column {
	columns := make([]sqlmodel.Column, 0, len(analysis.OutputColumns))
	for _, output := range analysis.OutputColumns {
		name := cmp.Or(output.Name, output.ColumnName)
		columns = append(columns, sqlmodel.NewColumn(name, sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true))
	}
	return columns
}

// isValueLikeToken reports whether kind is a value-bearing token (identifier, number, or
// string literal) that must be separated from an adjacent value-bearing token by a space
// when re-emitting a type body. Operators, dots, commas, and parentheses are not
// value-like, so adjacency with them never forces a space.
//
// Takes kind (tokenKind) which is the token kind to classify.
//
// Returns bool which is true for identifier/number/string tokens.
func isValueLikeToken(kind tokenKind) bool {
	return kind == tokenIdentifier || kind == tokenNumber || kind == tokenString
}

// extractMergeTreeFamilyParams lifts MergeTree-family positional parameters into keys.
//
// When the engine is one of the MergeTree-family forms with positional parameters, each
// parameter is lifted into a dedicated EngineSpecific key. The recognised forms are
// ReplacingMergeTree(ver_col [, is_deleted_col]), CollapsingMergeTree(sign),
// VersionedCollapsingMergeTree(sign, version), SummingMergeTree([(col, col, ...)]),
// ReplicatedMergeTree(zoo_path, replica_name), and ReplicatedReplacingMergeTree(zoo_path,
// replica_name [, version_column]). Leading `=` whitespace and any case variant of the
// engine name are tolerated. Engines outside this set are left alone; the caller's
// catch-all ENGINE key still carries the full text.
//
// Takes mutation (*ast.CatalogueMutation) whose EngineSpecific map is populated.
// Takes engineText (string) which is the captured engine clause text.
func extractMergeTreeFamilyParams(mutation *ast.CatalogueMutation, engineText string) {
	name, arguments := splitEngineNameAndArgs(engineText)
	if handler, found := mergeTreeFamilyHandlers[strings.ToLower(name)]; found {
		handler(mutation, name, arguments)
	}
}

// applyReplacingMergeTreeArgs populates the ReplacingMergeTree-specific EngineSpecific
// keys from the parsed argument list. The first argument is the version column; the
// optional second argument is the is_deleted column.
//
// Takes mutation (*ast.CatalogueMutation) whose EngineSpecific map is populated.
// Takes arguments ([]string) which are the parsed engine arguments in order.
func applyReplacingMergeTreeArgs(mutation *ast.CatalogueMutation, _ string, arguments []string) {
	if len(arguments) >= 1 {
		mutation.EngineSpecific[engineKeyMergeTreeVersionColumn] = arguments[0]
	}
	if len(arguments) >= 2 {
		mutation.EngineSpecific[engineKeyMergeTreeIsDeletedColumn] = arguments[1]
	}
}

// applyCollapsingMergeTreeArgs populates the sign column for the CollapsingMergeTree
// engine variant.
//
// Takes mutation (*ast.CatalogueMutation) whose EngineSpecific map is populated.
// Takes arguments ([]string) which are the parsed engine arguments in order.
func applyCollapsingMergeTreeArgs(mutation *ast.CatalogueMutation, _ string, arguments []string) {
	if len(arguments) >= 1 {
		mutation.EngineSpecific[engineKeyMergeTreeSignColumn] = arguments[0]
	}
}

// applyVersionedCollapsingMergeTreeArgs captures both the sign and version columns of
// VersionedCollapsingMergeTree.
//
// Takes mutation (*ast.CatalogueMutation) whose EngineSpecific map is populated.
// Takes arguments ([]string) which are the parsed engine arguments in order.
func applyVersionedCollapsingMergeTreeArgs(mutation *ast.CatalogueMutation, _ string, arguments []string) {
	if len(arguments) >= 1 {
		mutation.EngineSpecific[engineKeyMergeTreeSignColumn] = arguments[0]
	}
	if len(arguments) >= 2 {
		mutation.EngineSpecific[engineKeyMergeTreeVersionColumn] = arguments[1]
	}
}

// applySummingMergeTreeArgs joins the SummingMergeTree column list into a single
// comma-separated string for the EngineSpecific key.
//
// Takes mutation (*ast.CatalogueMutation) whose EngineSpecific map is populated.
// Takes arguments ([]string) which are the parsed engine arguments in order.
func applySummingMergeTreeArgs(mutation *ast.CatalogueMutation, _ string, arguments []string) {
	if len(arguments) > 0 {
		mutation.EngineSpecific[engineKeyMergeTreeSummingColumns] = strings.Join(arguments, ", ")
	}
}

// applyReplicatedMergeTreeArgs captures the Keeper / ZooKeeper path plus replica name of
// ReplicatedMergeTree.
//
// Takes mutation (*ast.CatalogueMutation) whose EngineSpecific map is populated.
// Takes arguments ([]string) which are the parsed engine arguments in order.
func applyReplicatedMergeTreeArgs(mutation *ast.CatalogueMutation, _ string, arguments []string) {
	if len(arguments) >= 1 {
		mutation.EngineSpecific[engineKeyMergeTreeZooPath] = arguments[0]
	}
	if len(arguments) >= 2 {
		mutation.EngineSpecific[engineKeyMergeTreeReplicaName] = arguments[1]
	}
}

// applyReplicatedReplacingMergeTreeArgs captures the Keeper / ZooKeeper path plus replica
// name and the optional version column of ReplicatedReplacingMergeTree.
//
// Takes mutation (*ast.CatalogueMutation) whose EngineSpecific map is populated.
// Takes arguments ([]string) which are the parsed engine arguments in order.
func applyReplicatedReplacingMergeTreeArgs(mutation *ast.CatalogueMutation, _ string, arguments []string) {
	applyReplicatedMergeTreeArgs(mutation, "", arguments)
	const replicatedReplacingVersionIndex = 2
	if len(arguments) >= replicatedReplacingVersionIndex+1 {
		mutation.EngineSpecific[engineKeyMergeTreeVersionColumn] = arguments[replicatedReplacingVersionIndex]
	}
}

// splitEngineNameAndArgs decomposes an engine clause text into its name and arguments.
//
// A text such as `ReplacingMergeTree(version, is_deleted)` yields the name plus the
// trimmed positional argument list. The argument splitter is paren-aware so nested
// expressions (`(col, func(a, b))`) do not split on internal commas.
//
// Takes engineText (string) which is the captured engine clause text.
//
// Returns string which is the bare engine name.
// Returns []string which are the positional arguments, or nil when the engine has no
// parameter parens (for example `Log`).
func splitEngineNameAndArgs(engineText string) (string, []string) {
	trimmed := strings.TrimSpace(engineText)
	openIndex := indexOutsideQuotes(trimmed, '(')
	if openIndex < 0 {
		return trimmed, nil
	}
	name := strings.TrimSpace(trimmed[:openIndex])
	closeIndex := lastIndexOutsideQuotes(trimmed, ')')
	if closeIndex <= openIndex {
		return name, nil
	}
	body := trimmed[openIndex+1 : closeIndex]
	arguments := splitTopLevelArguments(body)
	return name, arguments
}

// indexOutsideQuotes returns the index of the first occurrence of target that lies
// outside a single-quoted string literal, or -1 when no such occurrence exists. Backslash
// escapes inside a literal are respected so an escaped quote does not prematurely close
// the string.
//
// Takes text (string) which is the text to scan.
// Takes target (byte) which is the character to locate.
//
// Returns int which is the byte index of the first unquoted target, or -1.
func indexOutsideQuotes(text string, target byte) int {
	inString := false
	for index := 0; index < len(text); index++ {
		character := text[index]
		if inString {
			switch character {
			case '\\':
				index++
			case '\'':
				inString = false
			}
			continue
		}
		if character == '\'' {
			inString = true
			continue
		}
		if character == target {
			return index
		}
	}
	return -1
}

// lastIndexOutsideQuotes returns the index of the last occurrence of target that lies
// outside a single-quoted string literal, or -1 when no such occurrence exists. Backslash
// escapes inside a literal are respected so an escaped quote does not prematurely close
// the string.
//
// Takes text (string) which is the text to scan.
// Takes target (byte) which is the character to locate.
//
// Returns int which is the byte index of the last unquoted target, or -1.
func lastIndexOutsideQuotes(text string, target byte) int {
	inString := false
	found := -1
	for index := 0; index < len(text); index++ {
		character := text[index]
		if inString {
			switch character {
			case '\\':
				index++
			case '\'':
				inString = false
			}
			continue
		}
		if character == '\'' {
			inString = true
			continue
		}
		if character == target {
			found = index
		}
	}
	return found
}

// splitTopLevelArguments splits a comma-separated argument list at the top level.
//
// Nested parens and single-quoted string literals are honoured. Each returned argument is
// whitespace-trimmed; empty entries are dropped. A comma or paren inside a single-quoted
// literal (with backslash escapes respected) is part of the literal rather than a
// delimiter.
//
// Takes body (string) which is the comma-separated argument text.
//
// Returns []string which are the trimmed top-level arguments.
func splitTopLevelArguments(body string) []string {
	var arguments []string
	var current strings.Builder
	depth := 0
	for index := 0; index < len(body); index++ {
		character := body[index]
		switch {
		case character == '\'':
			index = writeQuotedLiteral(&current, body, index)
		case character == '(':
			depth++
			current.WriteByte(character)
		case character == ')':
			depth--
			current.WriteByte(character)
		case character == ',' && depth == 0:
			if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
				arguments = append(arguments, trimmed)
			}
			current.Reset()
		default:
			current.WriteByte(character)
		}
	}
	if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
		arguments = append(arguments, trimmed)
	}
	return arguments
}

// writeQuotedLiteral copies the single-quoted literal that begins at the opening quote at
// start into builder, honouring backslash escapes, and returns the index of its closing
// quote so the caller's loop increment advances past the whole literal. An unterminated
// literal consumes to the end of input.
//
// Takes builder (*strings.Builder) which receives the copied literal bytes.
// Takes body (string) which is the text being scanned.
// Takes start (int) which is the index of the opening single quote.
//
// Returns int which is the index of the closing quote, or the final index on no close.
func writeQuotedLiteral(builder *strings.Builder, body string, start int) int {
	builder.WriteByte(body[start])
	for index := start + 1; index < len(body); index++ {
		character := body[index]
		builder.WriteByte(character)
		switch character {
		case '\\':
			if index+1 < len(body) {
				index++
				builder.WriteByte(body[index])
			}
		case '\'':
			return index
		}
	}
	return len(body) - 1
}

// isClauseKeyword reports whether the identifier text matches one of the engine-tail
// clause keywords that terminate captureExpressionUntilClauseKeyword. The comparison is
// case-insensitive.
//
// The refreshable materialised view tail keywords (POPULATE, EMPTY, DEFINER, SQL) also
// terminate the capture so MV declarations interleaved with engine clauses do not see
// their trailing modifiers eaten by the ORDER / TTL / SETTINGS body parser.
//
// Takes candidate (token) which is the token to test; a delimited identifier never
// matches.
//
// Returns bool which is true when text is an engine-tail clause keyword.
func isClauseKeyword(candidate token) bool {
	switch candidate.Keyword(tokenIdentifier) {
	case "PARTITION", "ORDER", "PRIMARY", "SAMPLE", engineClauseTTL, "SETTINGS", engineClauseComment, "AS", engineClauseEngine,
		"POPULATE", "EMPTY", "DEFINER", "SQL":
		return true
	default:
		return false
	}
}

// writeClauseToken serialises a single token into the textual representation used by the
// clause capture helpers. Returns the new paren-balance depth so the caller can track it
// across iterations.
//
// Takes builder (*strings.Builder), the target buffer.
// Takes candidate (token), the token to render.
// Takes depth (int), the current paren depth before this token.
//
// Returns the new paren depth after accounting for this token.
func writeClauseToken(builder *strings.Builder, candidate token, depth int) int {
	switch candidate.Kind {
	case tokenLeftParen:
		builder.WriteByte('(')
		return depth + 1
	case tokenRightParen:
		builder.WriteByte(')')
		return depth - 1
	case tokenComma:
		builder.WriteString(", ")
	case tokenIdentifier, tokenNumber:
		if builder.Len() > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(candidate.Value)
	case tokenString:
		if builder.Len() > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteByte('\'')
		builder.WriteString(escapeClickHouseStringBody(candidate.Value))
		builder.WriteByte('\'')
	case tokenOperator:
		builder.WriteString(candidate.Value)
	case tokenDot:
		builder.WriteByte('.')
	case tokenStar:
		builder.WriteByte('*')
	case tokenLeftBracket:
		builder.WriteByte('[')
	case tokenRightBracket:
		builder.WriteByte(']')
	case tokenCast:
		builder.WriteString("::")
	case tokenArrow:
		builder.WriteString(" -> ")
	default:
	}
	return depth
}

// writeTokenAsSourceText writes the token's source representation to builder. String
// literals are quoted (with embedded single quotes doubled); identifiers that contain
// unsafe characters are wrapped in backticks; all other token kinds are emitted verbatim.
//
// Takes builder (*strings.Builder) which receives the rendered token text.
// Takes candidate (token) which is the token to render.
func writeTokenAsSourceText(builder *strings.Builder, candidate token) {
	switch candidate.Kind {
	case tokenString:
		builder.WriteByte('\'')
		builder.WriteString(escapeClickHouseStringBody(candidate.Value))
		builder.WriteByte('\'')
	case tokenIdentifier:
		if identifierNeedsQuoting(candidate.Value) {
			builder.WriteByte('`')
			builder.WriteString(strings.ReplaceAll(candidate.Value, "`", "``"))
			builder.WriteByte('`')
		} else {
			builder.WriteString(candidate.Value)
		}
	default:
		builder.WriteString(candidate.Value)
	}
}

// identifierNeedsQuoting reports whether an identifier cannot be re-emitted bare.
//
// An identifier needs quoting when it contains a character (whitespace, punctuation, or a
// leading digit) that would prevent it from being parsed as a bare identifier on its own,
// so re-emission must wrap it in backticks to preserve fidelity. The scan walks the value
// codepoint by codepoint so multi-byte runes (Unicode letters inside identifiers) are
// classified correctly rather than rejected byte-by-byte.
//
// Takes value (string) which is the identifier to test.
//
// Returns bool which is true when the identifier must be backtick-quoted.
func identifierNeedsQuoting(value string) bool {
	if value == "" {
		return false
	}
	leading, leadingWidth := utf8.DecodeRuneInString(value)
	if !isIdentStart(leading) {
		return true
	}
	for index := leadingWidth; index < len(value); {
		character, width := utf8.DecodeRuneInString(value[index:])
		if !isIdentPart(character) {
			return true
		}
		index += width
	}
	return false
}

// inferPrimaryKeyFromOrderBy extracts the column list from a ClickHouse `ORDER BY (col1,
// col2)` or `ORDER BY col1` clause. When no explicit PRIMARY KEY is declared, ClickHouse
// treats ORDER BY as the table's primary key for indexing purposes; we mirror that.
//
// The split is paren-aware via splitTopLevelArguments so multi-argument function calls in
// the key expression (for example `ORDER BY (cityHash64(a, b), c)`) are kept intact
// rather than torn at their internal commas.
//
// Takes orderBy (string) which is the captured ORDER BY expression text.
//
// Returns []string which are the inferred primary key columns.
func inferPrimaryKeyFromOrderBy(orderBy string) []string {
	body := strings.TrimSpace(orderBy)
	body = strings.TrimPrefix(body, "(")
	body = strings.TrimSuffix(body, ")")
	var keys []string
	for _, part := range splitTopLevelArguments(body) {
		fields := strings.Fields(part)
		if len(fields) > 0 {
			keys = append(keys, fields[0])
		}
	}
	return keys
}
