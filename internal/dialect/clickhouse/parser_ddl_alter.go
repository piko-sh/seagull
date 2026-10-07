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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// keywordIndex marks the INDEX sub-target of an ALTER TABLE action.
	keywordIndex = "INDEX"

	// keywordProjection marks the PROJECTION sub-target of an ALTER TABLE action.
	keywordProjection = "PROJECTION"

	// keywordStatistics marks the STATISTICS sub-target of an ALTER TABLE action.
	keywordStatistics = "STATISTICS"

	// keywordConstraint marks the CONSTRAINT sub-target of an ALTER TABLE action.
	keywordConstraint = "CONSTRAINT"

	// keywordType marks the TYPE keyword that precedes an index or statistic type.
	keywordType = "TYPE"

	// keywordRefresh marks the REFRESH sub-target of a MODIFY action.
	keywordRefresh = "REFRESH"

	// keywordEvery marks the EVERY interval keyword of a refresh policy.
	keywordEvery = "EVERY"

	// keywordAfter marks the AFTER interval keyword of a refresh policy.
	keywordAfter = "AFTER"

	// keywordSQL marks the SQL keyword used by the SQL SECURITY clause.
	keywordSQL = "SQL"

	// keywordDefiner marks the DEFINER sub-target of a MODIFY action.
	keywordDefiner = "DEFINER"

	// keywordPartition marks the PARTITION keyword of a partition operation.
	keywordPartition = "PARTITION"

	// keywordPart marks the PART keyword of a part-level partition operation.
	keywordPart = "PART"

	// keywordFrom marks the FROM keyword of a partition move or fetch operation.
	keywordFrom = "FROM"

	// keywordTo marks the TO keyword of a partition move operation.
	keywordTo = "TO"

	// keywordAttach marks the ATTACH keyword of a partition operation.
	keywordAttach = "ATTACH"

	// keywordDetach marks the DETACH keyword of a control operation (DETACH TABLE / VIEW /
	// DICTIONARY / DATABASE).
	keywordDetach = "DETACH"

	// keywordGranularity marks the GRANULARITY tail clause of an ADD INDEX action.
	//
	// It appears in `ALTER TABLE ... ADD INDEX ... TYPE ... GRANULARITY n`.
	keywordGranularity = "GRANULARITY"

	// keywordName marks the NAME keyword used by the `WITH NAME 'x'` backup-name modifier on
	// FREEZE / UNFREEZE partition operations.
	keywordName = "NAME"

	// keywordDetached marks the DETACHED prefix on `DROP DETACHED PARTITION` / `DROP
	// DETACHED PART`.
	keywordDetached = "DETACHED"

	// keywordWith marks the WITH keyword used by the `WITH NAME 'x'` backup-name tail and
	// `WITH FILL` LIMIT modifier.
	keywordWith = "WITH"

	// engineKeyProjectionName is the EngineSpecific key holding a projection name.
	engineKeyProjectionName = "PROJECTION_NAME"

	// engineKeyProjectionSelect is the EngineSpecific key holding a projection body.
	engineKeyProjectionSelect = "PROJECTION_SELECT"

	// engineKeyIndexName is the EngineSpecific key holding a skipping-index name.
	engineKeyIndexName = "INDEX_NAME"

	// engineKeyIndexExpr is the EngineSpecific key holding a skipping-index expression.
	engineKeyIndexExpr = "INDEX_EXPR"

	// engineKeyIndexType is the EngineSpecific key holding a skipping-index type.
	engineKeyIndexType = "INDEX_TYPE"

	// engineKeyIndexGranularity is the EngineSpecific key holding an index granularity.
	engineKeyIndexGranularity = "INDEX_GRANULARITY"

	// engineKeyConstraintName is the EngineSpecific key holding a constraint name.
	engineKeyConstraintName = "CONSTRAINT_NAME"

	// engineKeyConstraintCheck is the EngineSpecific key holding a constraint check body.
	engineKeyConstraintCheck = "CONSTRAINT_CHECK"

	// engineKeyStatsColumns is the EngineSpecific key holding a statistic column list.
	engineKeyStatsColumns = "STATS_COLUMNS"

	// engineKeyStatsTypes is the EngineSpecific key holding a statistic type list.
	engineKeyStatsTypes = "STATS_TYPES"

	// engineKeyColumnRemove is the EngineSpecific key holding a removed column property.
	engineKeyColumnRemove = "COLUMN_REMOVE"

	// engineKeyColumnModifyCmt is the EngineSpecific key holding a column comment.
	engineKeyColumnModifyCmt = "COLUMN_MODIFY_COMMENT"

	// engineKeyColumnResetSet is the EngineSpecific key holding a reset setting name.
	engineKeyColumnResetSet = "COLUMN_RESET_SETTING"

	// engineKeyMaterializePart is the EngineSpecific key holding a materialise partition.
	engineKeyMaterializePart = "MATERIALIZE_PARTITION"

	// engineKeyModifyRefreshKind is the EngineSpecific key holding a MODIFY refresh kind.
	engineKeyModifyRefreshKind = "MODIFY_REFRESH_KIND"

	// engineKeyMVRefreshKind is the EngineSpecific key holding a view refresh kind.
	engineKeyMVRefreshKind = "MV_REFRESH_KIND"

	// engineKeyMVRefreshInterval is the EngineSpecific key holding a refresh interval.
	engineKeyMVRefreshInterval = "MV_REFRESH_INTERVAL"

	// engineKeyMVRefreshOffset is the EngineSpecific key holding a view refresh offset.
	engineKeyMVRefreshOffset = "MV_REFRESH_OFFSET"

	// engineKeyMVRefreshAfter is the EngineSpecific key holding a view AFTER interval.
	engineKeyMVRefreshAfter = "MV_REFRESH_AFTER"

	// engineKeyMVSQLSecurity is the EngineSpecific key holding a view SQL security mode.
	engineKeyMVSQLSecurity = "MV_SQL_SECURITY"

	// engineKeyMVDefiner is the EngineSpecific key holding a view definer.
	engineKeyMVDefiner = "MV_DEFINER"

	// engineKeyNewQuery is the EngineSpecific key holding a replacement view query.
	engineKeyNewQuery = "NEW_QUERY"

	// engineKeyColumnName is the EngineSpecific key holding a column name.
	engineKeyColumnName = "COLUMN_NAME"

	// engineKeyMaterializeTarget is the EngineSpecific key holding a materialise target.
	engineKeyMaterializeTarget = "MATERIALIZE_TARGET"

	// engineKeyStatementBody is the EngineSpecific key holding a raw statement body.
	engineKeyStatementBody = "STATEMENT_BODY"

	// statsListSeparator joins captured statistic column / type identifier lists when they
	// are flattened into a single EngineSpecific value.
	statsListSeparator = ","
)

// parseAlterAddProjection captures the projection name and body of an `ALTER TABLE x ADD
// PROJECTION [IF NOT EXISTS] name (...)` clause.
//
// The body is reconstructed from tokens into EngineSpecific[PROJECTION_SELECT]; it is
// SELECT-shaped but is not analysed structurally.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the added projection.
// Returns error when the projection name or body cannot be parsed.
func (p *parser) parseAlterAddProjection(database, table string) (*ast.CatalogueMutation, error) {
	p.matchIfNotExists()
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	body := ""
	if p.Current().Kind == tokenLeftParen {
		captured, captureErr := p.captureParenthesisedBodyAsText()
		if captureErr != nil {
			return nil, captureErr
		}
		body = captured
	}
	return ast.NewCatalogueMutation(ast.MutationAlterTableAddProjection, database, table, ast.WithEngineSpecific(map[string]string{
		engineKeyProjectionName:   name,
		engineKeyProjectionSelect: body,
	})), nil
}

// parseAlterAddIndex captures the name, expression, type, and granularity of an `ALTER
// TABLE x ADD INDEX [IF NOT EXISTS] name expr TYPE indextype [GRANULARITY n]` clause.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the added skipping index.
// Returns error when the index name cannot be parsed.
func (p *parser) parseAlterAddIndex(database, table string) (*ast.CatalogueMutation, error) {
	p.matchIfNotExists()
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	indexExpression := p.captureIndexExpressionText()
	indexType, indexGranularity := p.captureIndexTypeAndGranularity()
	specific := map[string]string{
		engineKeyIndexName: name,
		engineKeyIndexExpr: indexExpression,
	}
	if indexType != "" {
		specific[engineKeyIndexType] = indexType
	}
	if indexGranularity != "" {
		specific[engineKeyIndexGranularity] = indexGranularity
	}
	return ast.NewCatalogueMutation(
		ast.MutationAlterTableAddSkippingIndex,
		database,
		table,
		ast.WithEngineSpecific(specific),
	), nil
}

// captureIndexExpressionText reads the expression that follows an index name, stopping at
// TYPE, GRANULARITY, a top-level comma, or the end of input.
//
// Returns string which is the trimmed index expression text.
func (p *parser) captureIndexExpressionText() string {
	var builder strings.Builder
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && current.Kind == tokenIdentifier &&
			(current.IsKeyword(tokenIdentifier, keywordType) || current.IsKeyword(tokenIdentifier, keywordGranularity)) {
			break
		}
		if depth == 0 && current.Kind == tokenComma {
			break
		}
		switch current.Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			if depth == 0 {
				return strings.TrimSpace(builder.String())
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

// captureIndexTypeAndGranularity reads the optional `TYPE name(...)` and `GRANULARITY n`
// tail of an ALTER TABLE ADD INDEX clause.
//
// Returns indexType (string) which is the captured index type, empty when absent.
// Returns indexGranularity (string) which is the captured granularity, empty when absent.
func (p *parser) captureIndexTypeAndGranularity() (indexType string, indexGranularity string) {
	for {
		switch {
		case p.MatchKeyword(keywordType):
			indexType = p.captureIndexTypeBody()
		case p.MatchKeyword(keywordGranularity):
			if p.Current().Kind == tokenNumber {
				indexGranularity = p.Current().Value
				p.Advance()
			}
		default:
			return indexType, indexGranularity
		}
	}
}

// captureIndexTypeBody reads the `name(args)` form following the `TYPE` keyword in an ADD
// INDEX clause.
//
// Returns string which is the trimmed index type with its argument body.
func (p *parser) captureIndexTypeBody() string {
	var builder strings.Builder
	if p.Current().Kind != tokenIdentifier {
		return ""
	}
	builder.WriteString(p.Current().Value)
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return strings.TrimSpace(builder.String())
	}
	body, err := p.captureParenthesisedBodyAsText()
	if err != nil {
		return strings.TrimSpace(builder.String())
	}
	builder.WriteByte('(')
	builder.WriteString(body)
	builder.WriteByte(')')
	return strings.TrimSpace(builder.String())
}

// parseAlterAddConstraint captures the name and check body of an `ALTER TABLE x ADD
// CONSTRAINT [IF NOT EXISTS] name CHECK expr` clause.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the added constraint.
// Returns error when the constraint name cannot be parsed.
func (p *parser) parseAlterAddConstraint(database, table string) (*ast.CatalogueMutation, error) {
	p.matchIfNotExists()
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	specific := map[string]string{engineKeyConstraintName: name}
	if p.MatchKeyword("CHECK") {
		specific[engineKeyConstraintCheck] = p.consumeUntilTopLevelCommaAsText()
	}
	return ast.NewCatalogueMutation(
		ast.MutationAlterTableAddConstraint,
		database,
		table,
		ast.WithConstraintName(name),
		ast.WithEngineSpecific(specific),
	), nil
}

// parseAlterAddStatistics captures the column list and statistic type list of an `ALTER
// TABLE x ADD STATISTICS [IF NOT EXISTS] col1[, col2, ...] TYPE type1[, type2, ...]`
// clause.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the added statistics.
// Returns error when the clause cannot be parsed.
func (p *parser) parseAlterAddStatistics(database, table string) (*ast.CatalogueMutation, error) {
	p.matchIfNotExists()
	columns, types := p.parseStatisticsColumnAndTypeLists()
	return ast.NewCatalogueMutation(ast.MutationAlterTableAddStatistics, database, table, ast.WithEngineSpecific(map[string]string{
		engineKeyStatsColumns: strings.Join(columns, statsListSeparator),
		engineKeyStatsTypes:   strings.Join(types, statsListSeparator),
	})), nil
}

// parseStatisticsColumnAndTypeLists reads the `col[, col, ...] TYPE type[, type, ...]`
// body of an ADD or MODIFY STATISTICS clause.
//
// Returns columns ([]string) which are the parsed statistic column names.
// Returns types ([]string) which are the parsed statistic type names, empty when no TYPE
// clause is present.
func (p *parser) parseStatisticsColumnAndTypeLists() (columns, types []string) {
	columns = p.parseCommaSeparatedIdentifierList()
	if p.MatchKeyword(keywordType) {
		types = p.parseCommaSeparatedIdentifierList()
	}
	return columns, types
}

// parseCommaSeparatedIdentifierList reads a `name[, name, ...]` list where each entry is
// a bare identifier.
//
// Returns []string which are the parsed identifier names in order.
func (p *parser) parseCommaSeparatedIdentifierList() []string {
	var names []string
	for !p.AtEnd() {
		if p.Current().Kind != tokenIdentifier {
			break
		}
		if p.Current().IsKeyword(tokenIdentifier, keywordType) {
			break
		}
		names = append(names, p.Current().Value)
		p.Advance()
		if p.Current().Kind == tokenComma {
			p.Advance()
			continue
		}
		break
	}
	return names
}

// parseAlterDropProjection captures the name of a `DROP PROJECTION [IF EXISTS] name`
// clause.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the dropped projection.
// Returns error when the projection name cannot be parsed.
func (p *parser) parseAlterDropProjection(database, table string) (*ast.CatalogueMutation, error) {
	p.matchIfExists()
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	return ast.NewCatalogueMutation(ast.MutationAlterTableDropProjection, database, table, ast.WithEngineSpecific(map[string]string{
		engineKeyProjectionName: name,
	})), nil
}

// parseAlterDropIndex captures the name of a `DROP INDEX [IF EXISTS] name` clause.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the dropped skipping index.
// Returns error when the index name cannot be parsed.
func (p *parser) parseAlterDropIndex(database, table string) (*ast.CatalogueMutation, error) {
	p.matchIfExists()
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	return ast.NewCatalogueMutation(ast.MutationAlterTableDropSkippingIndex, database, table, ast.WithEngineSpecific(map[string]string{
		engineKeyIndexName: name,
	})), nil
}

// parseAlterDropConstraint captures the name of a `DROP CONSTRAINT [IF EXISTS] name`
// clause.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the dropped constraint.
// Returns error when the constraint name cannot be parsed.
func (p *parser) parseAlterDropConstraint(database, table string) (*ast.CatalogueMutation, error) {
	p.matchIfExists()
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	return ast.NewCatalogueMutation(
		ast.MutationAlterTableDropConstraint,
		database,
		table,
		ast.WithConstraintName(name),
		ast.WithEngineSpecific(map[string]string{engineKeyConstraintName: name}),
	), nil
}

// parseAlterDropStatistics captures the column list of a `DROP STATISTICS [IF EXISTS]
// col1[, col2, ...]` clause.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the dropped statistics.
// Returns error when the clause cannot be parsed.
func (p *parser) parseAlterDropStatistics(database, table string) (*ast.CatalogueMutation, error) {
	p.matchIfExists()
	columns := p.parseCommaSeparatedIdentifierList()
	return ast.NewCatalogueMutation(ast.MutationAlterTableDropStatistics, database, table, ast.WithEngineSpecific(map[string]string{
		engineKeyStatsColumns: strings.Join(columns, statsListSeparator),
	})), nil
}

// parseAlterDropDetached recognises `DROP DETACHED PARTITION expr` and `DROP DETACHED
// PART expr`.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the dropped detached partition or part.
// Returns error when neither PART nor PARTITION follows DROP DETACHED.
func (p *parser) parseAlterDropDetached(database, table string) (*ast.CatalogueMutation, error) {
	isPart := p.MatchKeyword(keywordPart)
	if !isPart && !p.MatchKeyword(keywordPartition) {
		return nil, dialectkit.NewParseError(p.Current(), "expected PART or PARTITION after DROP DETACHED")
	}
	mutation, err := p.parseAlterPartitionOperation(database, table, keywordDrop, isPart)
	if err != nil {
		return nil, err
	}
	if mutation != nil {
		mutation.EngineSpecific["PARTITION_DETACHED"] = "true"
	}
	return mutation, nil
}

// parseAlterModifyColumnAction handles `MODIFY COLUMN [IF EXISTS] name {type | REMOVE
// prop | MODIFY COMMENT 'text' | RESET SETTING name}` clauses.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the column modification.
// Returns error when the column name or sub-action cannot be parsed.
func (p *parser) parseAlterModifyColumnAction(database, table string) (*ast.CatalogueMutation, error) {
	ifColumnExists := p.matchIfExists()
	savedPosition := p.Position
	columnName, err := p.parseColumnPath()
	if err != nil {
		return nil, err
	}
	if mutation, handled, actionErr := p.tryParseModifyColumnSubAction(database, table, columnName); handled {
		if mutation != nil {
			mutation.IfColumnExists = ifColumnExists
		}
		return mutation, actionErr
	}
	p.Position = savedPosition
	column, err := p.parseClickHouseColumn()
	if err != nil {
		return nil, err
	}
	placement, err := p.parsePlacement()
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(
		ast.MutationAlterTableAlterColumn,
		database,
		table,
		ast.WithColumnName(column.Name),
		ast.WithColumns([]sqlmodel.Column{column}),
		ast.WithPlacement(placement.Kind, placement.Anchor),
	)
	mutation.IfColumnExists = ifColumnExists
	return mutation, nil
}

// tryParseModifyColumnSubAction handles the REMOVE, MODIFY COMMENT, and RESET SETTING
// sub-target forms of MODIFY COLUMN.
//
// Once a sub-action keyword has been consumed the handled flag is true, so the caller
// must not fall through to column-type parsing; in that case a non-nil error means the
// sub-action was malformed. A malformed sub-action surfaces as an error rather than a
// silent no-op, otherwise the whole ALTER statement is dropped without explanation.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
// Takes columnName (string) which is the name of the column being modified.
//
// Returns *ast.CatalogueMutation which describes the sub-action, nil when none was
// matched.
// Returns bool which is true once a sub-action keyword has been consumed.
// Returns error when a consumed sub-action keyword is malformed.
func (p *parser) tryParseModifyColumnSubAction(database, table, columnName string) (*ast.CatalogueMutation, bool, error) {
	switch {
	case p.MatchKeyword("REMOVE"):
		property, parseErr := p.parseIdentifierOrKeyword()
		if parseErr != nil {
			return nil, true, parseErr
		}
		return p.buildModifyColumnMutation(database, table, columnName,
			engineKeyColumnRemove, strings.ToUpper(property)), true, nil
	case p.MatchKeyword("MODIFY"):
		if !p.MatchKeyword("COMMENT") {
			return nil, true, dialectkit.NewParseError(p.Current(), "expected COMMENT after MODIFY COLUMN "+columnName+" MODIFY")
		}
		commentText := ""
		if p.Current().Kind == tokenString {
			commentText = p.Current().Value
			p.Advance()
		}
		return p.buildModifyColumnMutation(database, table, columnName,
			engineKeyColumnModifyCmt, commentText), true, nil
	case p.MatchKeyword("RESET"):
		if !p.MatchKeyword("SETTING") {
			return nil, true, dialectkit.NewParseError(p.Current(), "expected SETTING after MODIFY COLUMN "+columnName+" RESET")
		}
		settingName, settingErr := p.parseIdentifierOrKeyword()
		if settingErr != nil {
			return nil, true, settingErr
		}
		return p.buildModifyColumnMutation(database, table, columnName,
			engineKeyColumnResetSet, settingName), true, nil
	}
	return nil, false, nil
}

// buildModifyColumnMutation creates a MutationAlterTableModifyColumn with the supplied
// key and value pair populated under EngineSpecific.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
// Takes columnName (string) which is the name of the column being modified.
// Takes key (string) which is the EngineSpecific key to populate.
// Takes value (string) which is the EngineSpecific value to store under key.
//
// Returns *ast.CatalogueMutation describing the column modification.
func (*parser) buildModifyColumnMutation(database, table, columnName, key, value string) *ast.CatalogueMutation {
	return ast.NewCatalogueMutation(ast.MutationAlterTableModifyColumn, database, table, ast.WithColumnName(columnName), ast.WithEngineSpecific(map[string]string{
		key: value,
	}))
}

// parseAlterModifyQuery handles `ALTER TABLE v MODIFY QUERY <select>` for refreshable
// materialised views.
//
// Takes database (string) which is the schema name of the target view.
// Takes table (string) which is the name of the target view.
//
// Returns *ast.CatalogueMutation which describes the replacement query.
// Returns error which is always nil for this clause.
func (p *parser) parseAlterModifyQuery(database, table string) (*ast.CatalogueMutation, error) {
	newQuery := p.consumeRemainderAsText()
	return ast.NewCatalogueMutation(ast.MutationAlterTableModifyQuery, database, table, ast.WithEngineSpecific(map[string]string{
		engineKeyNewQuery: newQuery,
	})), nil
}

// parseAlterModifyRefresh handles the refresh-policy sub-targets of `ALTER TABLE v
// MODIFY` for refreshable materialised views.
//
// Takes database (string) which is the schema name of the target view.
// Takes table (string) which is the name of the target view.
// Takes kind (string) which is the MODIFY sub-target keyword being handled.
//
// Returns *ast.CatalogueMutation which describes the refresh-policy change.
// Returns error which is always nil for this clause.
func (p *parser) parseAlterModifyRefresh(database, table, kind string) (*ast.CatalogueMutation, error) {
	specific := map[string]string{engineKeyModifyRefreshKind: kind}
	switch kind {
	case keywordRefresh:
		p.captureRefreshClauseInto(specific)
	case "SQL_SECURITY":
		if security, parseErr := p.parseIdentifierOrKeyword(); parseErr == nil {
			specific[engineKeyMVSQLSecurity] = strings.ToUpper(security)
		}
	case keywordDefiner:
		if p.Current().Kind == tokenOperator && p.Current().Value == "=" {
			p.Advance()
		}
		if name, parseErr := p.parseIdentifierOrKeyword(); parseErr == nil {
			specific[engineKeyMVDefiner] = name
		}
	}
	return ast.NewCatalogueMutation(
		ast.MutationAlterTableModifyRefresh,
		database,
		table,
		ast.WithEngineSpecific(specific),
	), nil
}

// captureRefreshClauseInto reads the `EVERY <interval> [OFFSET ...]` or `AFTER
// <interval>` body following a MODIFY REFRESH keyword.
//
// Takes specific (map[string]string) which is the EngineSpecific map the captured refresh
// kind, interval, offset, or AFTER interval is written into.
func (p *parser) captureRefreshClauseInto(specific map[string]string) {
	switch {
	case p.MatchKeyword(keywordEvery):
		specific[engineKeyMVRefreshKind] = keywordEvery
		specific[engineKeyMVRefreshInterval] = p.captureIntervalText()
		if p.MatchKeyword("OFFSET") {
			specific[engineKeyMVRefreshOffset] = p.captureIntervalText()
		}
	case p.MatchKeyword(keywordAfter):
		specific[engineKeyMVRefreshKind] = keywordAfter
		specific[engineKeyMVRefreshAfter] = p.captureIntervalText()
	}
}

// parseAlterModifyStatistics handles `ALTER TABLE x MODIFY STATISTICS col1[, col2, ...]
// TYPE type1[, type2, ...]`.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the modified statistics.
// Returns error which is always nil for this clause.
func (p *parser) parseAlterModifyStatistics(database, table string) (*ast.CatalogueMutation, error) {
	columns, types := p.parseStatisticsColumnAndTypeLists()
	return ast.NewCatalogueMutation(ast.MutationAlterTableModifyStatistics, database, table, ast.WithEngineSpecific(map[string]string{
		engineKeyStatsColumns: strings.Join(columns, statsListSeparator),
		engineKeyStatsTypes:   strings.Join(types, statsListSeparator),
	})), nil
}

// parseAlterMaterializeNamed reads the named target plus the optional `IN PARTITION expr`
// clause for MATERIALIZE COLUMN, INDEX, or PROJECTION.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
// Takes kind (ast.MutationKind) which is the mutation kind to emit.
// Takes nameKey (string) which is the EngineSpecific key the target name is stored under.
//
// Returns *ast.CatalogueMutation which describes the materialise action.
// Returns error when the target name cannot be parsed.
func (p *parser) parseAlterMaterializeNamed(database, table string, kind ast.MutationKind, nameKey string) (*ast.CatalogueMutation, error) {
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(kind, database, table, ast.WithEngineSpecific(map[string]string{
		nameKey: name,
	}))
	p.captureMaterializeInPartition(mutation)
	return mutation, nil
}

// captureMaterializeInPartition consumes an optional `IN PARTITION expr` clause.
//
// Takes mutation (*ast.CatalogueMutation) which is the mutation whose EngineSpecific map
// receives the captured partition expression.
func (p *parser) captureMaterializeInPartition(mutation *ast.CatalogueMutation) {
	if !p.MatchKeyword("IN") {
		return
	}
	if !p.MatchKeyword(keywordPartition) {
		return
	}
	mutation.EngineSpecific[engineKeyMaterializePart] = p.consumeUntilTopLevelCommaAsText()
}

// parseAlterMaterializeStatistics emits a mutation for `MATERIALIZE STATISTICS col_list
// [IN PARTITION expr]`.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the materialise statistics action.
// Returns error which is always nil for this clause.
func (p *parser) parseAlterMaterializeStatistics(database, table string) (*ast.CatalogueMutation, error) {
	columns := p.parseCommaSeparatedIdentifierList()
	mutation := ast.NewCatalogueMutation(ast.MutationAlterTableMaterializeStatistics, database, table, ast.WithEngineSpecific(map[string]string{
		engineKeyStatsColumns: strings.Join(columns, statsListSeparator),
	}))
	p.captureMaterializeInPartition(mutation)
	return mutation, nil
}

// parseAlterMaterializeTTL emits a runtime-only mutation for the `MATERIALIZE TTL [IN
// PARTITION expr]` form.
//
// Takes database (string) which is the schema name of the target table.
// Takes table (string) which is the name of the target table.
//
// Returns *ast.CatalogueMutation which describes the materialise TTL action.
// Returns error which is always nil for this clause.
func (p *parser) parseAlterMaterializeTTL(database, table string) (*ast.CatalogueMutation, error) {
	mutation := ast.NewCatalogueMutation(ast.MutationAlterTableMaterializeColumn, database, table, ast.WithEngineSpecific(map[string]string{
		engineKeyMaterializeTarget: "TTL",
	}))
	p.captureMaterializeInPartition(mutation)
	return mutation, nil
}
