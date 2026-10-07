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
	// keywordColumn is the COLUMN keyword used in ALTER TABLE ... COLUMN.
	keywordColumn = "COLUMN"

	// keywordCreate is the CREATE keyword used to introduce a wide range of object kinds
	// (DICTIONARY, FUNCTION, ...).
	keywordCreate = "CREATE"

	// keywordDictionary is the DICTIONARY keyword used in CREATE DICTIONARY / DROP
	// DICTIONARY.
	keywordDictionary = "DICTIONARY"

	// engineKeyExchangeTarget is the EngineSpecific key used by EXCHANGE TABLES to record
	// the right-hand-side table identifier (the swap target). The primary mutation's
	// SchemaName/TableName pair carries the left-hand-side; consumers read both halves to
	// re-emit or analyse the swap.
	engineKeyExchangeTarget = "EXCHANGE_TARGET"

	// engineKeyAsyncBody is the EngineSpecific key marking an asynchronous mutation (ALTER
	// TABLE ... UPDATE / DELETE).
	//
	// The DDL CatalogueMutation stores the reconstructed UPDATE or DELETE body under this
	// key. Query analysis records asynchronous mutations separately in the dialect's
	// analysis detail.
	engineKeyAsyncBody = "ASYNC_BODY"

	// dependsOnSeparator is the joiner used to flatten the DEPENDS ON table list captured
	// under EngineSpecific[MV_REFRESH_DEPENDS_ON].
	dependsOnSeparator = ", "

	// mvBooleanTrue is the canonical truthy string written into the MV_REFRESH_* /
	// MATERIALIZED / POPULATE EngineSpecific keys.
	mvBooleanTrue = "true"

	// keywordPopulate marks the POPULATE modifier in a CREATE MATERIALIZED VIEW body.
	keywordPopulate = "POPULATE"

	// keywordAs is the AS keyword that introduces a view body.
	keywordAs = "AS"

	// keywordEmpty marks the EMPTY modifier in a refreshable materialised view declaration.
	keywordEmpty = "EMPTY"

	// keywordDrop is the DROP keyword used by partition operations originating from `DROP
	// PARTITION` / `DROP DETACHED PARTITION`.
	keywordDrop = "DROP"

	// engineKeyMaterializedTarget is the EngineSpecific key under which `CREATE MATERIALIZED
	// VIEW ... TO target` captures the qualified destination table name.
	engineKeyMaterializedTarget = "MATERIALIZED_TARGET"
)

// parseAlterTable handles every ClickHouse `ALTER TABLE [db.]table ...` variant.
//
// ALTER UPDATE and ALTER DELETE are asynchronous mutation forms. The parser dispatches on
// the action keyword after the table name and emits one mutation per action. Multi-action
// ALTER (separated by commas) emits the primary mutation plus AdditionalMutations
// entries.
//
// Returns *ast.CatalogueMutation which is the primary ALTER mutation.
// Returns error when the table name or any action cannot be parsed.
func (p *parser) parseAlterTable() (mutation *ast.CatalogueMutation, err error) {
	if err := p.expectKeywordSequence("ALTER", "TABLE"); err != nil {
		return nil, err
	}
	ifExists := p.matchIfExists()

	database, name, nameError := p.parseDatabaseQualifiedName()
	if nameError != nil {
		return nil, nameError
	}

	cluster := p.matchOnCluster()
	defer func() {
		markIfExists(mutation, ifExists)
	}()

	primary, primaryErr := p.parseSingleAlterAction(database, name)
	if primaryErr != nil {
		return nil, primaryErr
	}

	for p.Current().Kind == tokenComma {
		p.Advance()
		additional, additionalErr := p.parseSingleAlterAction(database, name)
		if additionalErr != nil {
			return nil, additionalErr
		}
		if additional == nil {
			continue
		}
		if primary == nil {
			primary = additional
			continue
		}
		primary.AdditionalMutations = append(primary.AdditionalMutations, additional)
	}

	if cluster != "" && primary != nil {
		if primary.EngineSpecific == nil {
			primary.EngineSpecific = map[string]string{}
		}
		primary.EngineSpecific[engineClauseOnCluster] = cluster
	}

	return primary, nil
}

// parseSingleAlterAction reads one ALTER TABLE action and returns the corresponding
// CatalogueMutation.
//
// Recognised actions cover ADD COLUMN with optional DEFAULT and AFTER, DROP COLUMN,
// MODIFY COLUMN, RENAME COLUMN ... TO, MATERIALIZE COLUMN, MODIFY TTL, MODIFY ORDER BY,
// MODIFY SETTING, UPDATE ... WHERE, and DELETE WHERE. Unknown actions return a non-nil
// error so the caller can surface a diagnostic; the cursor is left at the offending
// token.
//
// Takes database (string) which is the schema name parsed from the table name.
// Takes table (string) which is the table name the action applies to.
//
// Returns *ast.CatalogueMutation which is the parsed action mutation.
// Returns error when the action keyword is unrecognised or its body is malformed.
func (p *parser) parseSingleAlterAction(database, table string) (*ast.CatalogueMutation, error) {
	switch {
	case p.MatchKeyword("ADD"):
		return p.parseAlterAddAction(database, table)
	case p.MatchKeyword(keywordDrop):
		return p.parseAlterDropAction(database, table)
	case p.MatchKeyword("MODIFY"):
		return p.parseAlterModifyAction(database, table)
	case p.MatchKeyword("RENAME"):
		return p.parseAlterRenameAction(database, table)
	case p.MatchKeyword("MATERIALIZE"):
		return p.parseAlterMaterializeAction(database, table)
	case p.MatchKeyword("UPDATE"):
		return p.parseAlterUpdateAction(database, table)
	case p.MatchKeyword("DELETE"):
		return p.parseAlterDeleteAction(database, table)
	case p.MatchKeyword("CLEAR"):

		p.consumeUntilTopLevelComma()
		return nil, nil
	case p.MatchKeyword("COMMENT"):

		p.consumeUntilTopLevelComma()
		return nil, nil
	case p.MatchKeyword("FREEZE"):
		return p.parseAlterPartitionOperation(database, table, "FREEZE", false)
	case p.MatchKeyword("UNFREEZE"):
		return p.parseAlterPartitionOperation(database, table, "UNFREEZE", false)
	case p.MatchKeyword("ATTACH"):
		return p.parseAlterPartitionOperation(database, table, keywordAttach, false)
	case p.MatchKeyword("DETACH"):
		return p.parseAlterPartitionOperation(database, table, "DETACH", false)
	case p.MatchKeyword("MOVE"):
		return p.parseAlterPartitionOperation(database, table, "MOVE", false)
	case p.MatchKeyword("REPLACE"):
		return p.parseAlterPartitionOperation(database, table, "REPLACE", false)
	case p.MatchKeyword("FETCH"):
		return p.parseAlterPartitionOperation(database, table, "FETCH", false)
	default:
		return nil, dialectkit.NewParseError(p.Current(), "unexpected ALTER TABLE action")
	}
}

// parseAlterAddAction handles `ALTER TABLE x ADD COLUMN ...` and the less-common sibling
// forms (ADD INDEX / ADD CONSTRAINT / ADD PROJECTION / ADD STATISTICS). Each sibling form
// is captured into its own mutation kind with its name and declaration metadata.
//
// Takes database (string) which is the schema name the column is added to.
// Takes table (string) which is the table name the column is added to.
//
// Returns *ast.CatalogueMutation which is the ADD action mutation.
// Returns error when the column definition or sibling form is malformed.
func (p *parser) parseAlterAddAction(database, table string) (*ast.CatalogueMutation, error) {
	switch {
	case p.MatchKeyword(keywordProjection):
		return p.parseAlterAddProjection(database, table)
	case p.MatchKeyword(keywordIndex):
		return p.parseAlterAddIndex(database, table)
	case p.MatchKeyword(keywordConstraint):
		return p.parseAlterAddConstraint(database, table)
	case p.MatchKeyword(keywordStatistics):
		return p.parseAlterAddStatistics(database, table)
	}
	p.MatchKeyword(keywordColumn)
	ifNotExists := p.matchIfNotExists()

	columns, err := p.parseTableColumns()
	if err != nil {
		return nil, err
	}

	placement, placementErr := p.parsePlacement()
	if placementErr != nil {
		return nil, placementErr
	}

	return ast.NewCatalogueMutation(
		ast.MutationAlterTableAddColumn,
		database,
		table,
		ast.WithColumns(columns),
		ast.WithIfNotExists(ifNotExists),
		ast.WithPlacement(placement.Kind, placement.Anchor),
	), nil
}

// parseAlterDropAction handles `ALTER TABLE x DROP COLUMN <name>` and its sibling forms
// (DROP INDEX / DROP CONSTRAINT / DROP PROJECTION / DROP STATISTICS / DROP PARTITION /
// DROP DETACHED PARTITION).
//
// Takes database (string) which is the schema name the drop applies to.
// Takes table (string) which is the table name the drop applies to.
//
// Returns *ast.CatalogueMutation which is the DROP action mutation.
// Returns error when the column name or sibling form is malformed.
func (p *parser) parseAlterDropAction(database, table string) (*ast.CatalogueMutation, error) {
	switch {
	case p.MatchKeyword(keywordProjection):
		return p.parseAlterDropProjection(database, table)
	case p.MatchKeyword(keywordIndex):
		return p.parseAlterDropIndex(database, table)
	case p.MatchKeyword(keywordConstraint):
		return p.parseAlterDropConstraint(database, table)
	case p.MatchKeyword(keywordStatistics):
		return p.parseAlterDropStatistics(database, table)
	case p.MatchKeyword(keywordPartition):
		return p.parseAlterPartitionOperation(database, table, keywordDrop, false)
	case p.MatchKeyword(keywordDetached):
		return p.parseAlterDropDetached(database, table)
	}
	p.MatchKeyword(keywordColumn)
	ifColumnExists := p.matchIfExists()

	columnName, err := p.parseColumnPath()
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(
		ast.MutationAlterTableDropColumn,
		database,
		table,
		ast.WithColumnName(columnName),
		ast.WithDroppedColumnPrefix(columnName+nestedFieldSeparator),
	)
	mutation.IfColumnExists = ifColumnExists
	return mutation, nil
}

// parseAlterModifyAction handles `MODIFY COLUMN ...` (type change) plus `MODIFY TTL`,
// `MODIFY ORDER BY`, `MODIFY SETTING`, `MODIFY QUERY` (for materialised views), `MODIFY
// REFRESH / DEFINER / SQL SECURITY` (refreshable materialised views), `MODIFY
// STATISTICS`, and other sub-targets.
//
// Takes database (string) which is the schema name the modify applies to.
// Takes table (string) which is the table name the modify applies to.
//
// Returns *ast.CatalogueMutation which is the MODIFY action mutation.
// Returns error when the MODIFY target is unrecognised or its body is malformed.
func (p *parser) parseAlterModifyAction(database, table string) (*ast.CatalogueMutation, error) {
	switch {
	case p.MatchKeyword(keywordColumn):
		return p.parseAlterModifyColumnAction(database, table)
	case p.MatchKeyword("QUERY"):
		return p.parseAlterModifyQuery(database, table)
	case p.MatchKeyword(keywordRefresh):
		return p.parseAlterModifyRefresh(database, table, keywordRefresh)
	case p.MatchKeyword(keywordSQL):
		if !p.MatchKeyword("SECURITY") {
			return nil, dialectkit.NewParseError(p.Current(), "expected SECURITY after MODIFY SQL")
		}
		return p.parseAlterModifyRefresh(database, table, "SQL_SECURITY")
	case p.MatchKeyword(keywordDefiner):
		return p.parseAlterModifyRefresh(database, table, keywordDefiner)
	case p.MatchKeyword(keywordStatistics):
		return p.parseAlterModifyStatistics(database, table)
	case p.IsKeyword("TTL", "ORDER", "SETTING", "SAMPLE", "REMOVE", "RESET"):
		p.consumeUntilTopLevelComma()
		return nil, nil
	default:
		return nil, dialectkit.NewParseError(p.Current(), "unexpected MODIFY target")
	}
}

// parseAlterRenameAction handles `RENAME COLUMN <old> TO <new>`.
//
// Takes database (string) which is the schema name the rename applies to.
// Takes table (string) which is the table name the rename applies to.
//
// Returns *ast.CatalogueMutation which is the RENAME COLUMN mutation.
// Returns error when COLUMN, the names, or TO are missing or malformed.
func (p *parser) parseAlterRenameAction(database, table string) (*ast.CatalogueMutation, error) {
	if !p.MatchKeyword(keywordColumn) {
		return nil, dialectkit.NewParseError(p.Current(), "expected COLUMN after RENAME")
	}
	ifColumnExists := p.matchIfExists()
	oldName, err := p.parseColumnPath()
	if err != nil {
		return nil, err
	}
	if !p.MatchKeyword("TO") {
		return nil, dialectkit.NewParseError(p.Current(), "expected TO after RENAME COLUMN")
	}
	newName, err := p.parseColumnPath()
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(
		ast.MutationAlterTableRenameColumn,
		database,
		table,
		ast.WithColumnName(oldName),
		ast.WithNewName(newName),
	)
	mutation.IfColumnExists = ifColumnExists
	return mutation, nil
}

// parseAlterMaterializeAction handles the MATERIALIZE family of ALTER actions and emits a
// dedicated mutation per sub-target.
//
// The recognised sub-targets are MATERIALIZE COLUMN, MATERIALIZE INDEX, MATERIALIZE
// PROJECTION, MATERIALIZE TTL, and MATERIALIZE STATISTICS, each accepting an optional IN
// PARTITION expression.
//
// Takes database (string) which is the schema name the action applies to.
// Takes table (string) which is the table name the action applies to.
//
// Returns *ast.CatalogueMutation which is the MATERIALIZE mutation.
// Returns error when the sub-target is unrecognised or its body is malformed.
func (p *parser) parseAlterMaterializeAction(database, table string) (*ast.CatalogueMutation, error) {
	switch {
	case p.MatchKeyword(keywordProjection):
		return p.parseAlterMaterializeNamed(database, table,
			ast.MutationAlterTableMaterializeProjection, engineKeyProjectionName)
	case p.MatchKeyword(keywordIndex):
		return p.parseAlterMaterializeNamed(database, table,
			ast.MutationAlterTableMaterializeIndex, engineKeyIndexName)
	case p.MatchKeyword(keywordStatistics):
		return p.parseAlterMaterializeStatistics(database, table)
	case p.MatchKeyword(keywordColumn):
		return p.parseAlterMaterializeNamed(database, table,
			ast.MutationAlterTableMaterializeColumn, engineKeyColumnName)
	case p.MatchKeyword("TTL"):
		return p.parseAlterMaterializeTTL(database, table)
	default:
		return nil, dialectkit.NewParseError(p.Current(), "expected COLUMN / INDEX / PROJECTION / TTL / STATISTICS after MATERIALIZE")
	}
}

// parseAlterUpdateAction handles `ALTER TABLE x UPDATE col = expr [, ...] WHERE
// predicate`, ClickHouse's asynchronous mutation form.
//
// The catalogue records a MutationAsyncDataUpdate with the SET / WHERE clause body
// reconstructed into EngineSpecific. The mutation does not change the table's schema
// view, so type resolution and column lookups against the table remain stable across the
// migration.
//
// Takes database (string) which is the schema name the update applies to.
// Takes table (string) which is the table name the update applies to.
//
// Returns *ast.CatalogueMutation which is the async update mutation.
// Returns error when the update body cannot be captured.
func (p *parser) parseAlterUpdateAction(database, table string) (*ast.CatalogueMutation, error) {
	body := p.consumeUntilTopLevelCommaAsText()
	return ast.NewCatalogueMutation(ast.MutationAsyncDataUpdate, database, table, ast.WithEngineSpecific(map[string]string{
		engineKeyAsyncBody: body,
	})), nil
}

// parseAlterDeleteAction handles `ALTER TABLE x DELETE WHERE predicate`. Like ALTER
// UPDATE, this is asynchronous; the DELETE / WHERE body is reconstructed into
// EngineSpecific as ASYNC_BODY.
//
// Takes database (string) which is the schema name the delete applies to.
// Takes table (string) which is the table name the delete applies to.
//
// Returns *ast.CatalogueMutation which is the async delete mutation.
// Returns error when the delete body cannot be captured.
func (p *parser) parseAlterDeleteAction(database, table string) (*ast.CatalogueMutation, error) {
	body := p.consumeUntilTopLevelCommaAsText()
	return ast.NewCatalogueMutation(ast.MutationAsyncDataDelete, database, table, ast.WithEngineSpecific(map[string]string{
		engineKeyAsyncBody: body,
	})), nil
}

// parseCreateView handles `CREATE [OR REPLACE] VIEW [IF NOT EXISTS] [db.]view [AS] SELECT
// ...`. The view's SELECT body is structurally analysed and stored on the mutation;
// column types resolve later against the catalogue.
//
// Returns *ast.CatalogueMutation which is the CREATE VIEW mutation.
// Returns error when the view name or body cannot be parsed.
func (p *parser) parseCreateView() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCreate); err != nil {
		return nil, err
	}
	p.skipCreatePrefixesInParser()
	if _, err := p.ExpectKeyword("VIEW"); err != nil {
		return nil, err
	}
	ifNotExists := p.matchIfNotExists()

	database, name, err := p.parseDatabaseQualifiedName()
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(ast.MutationCreateView, database, name, ast.WithIfNotExists(ifNotExists))
	if cluster := p.matchOnCluster(); cluster != "" {
		mutation.EngineSpecific = map[string]string{engineClauseOnCluster: cluster}
	}

	declaredColumns := p.tryParseViewColumnList()
	p.MatchKeyword("AS")
	mutation.ViewDefinition = p.analyseViewBody(declaredColumns)
	if mutation.ViewDefinition != nil {
		mutation.Columns = dialectkit.ColumnsFromNames(declaredColumns)
	}
	return mutation, nil
}

// tryParseViewColumnList consumes an optional parenthesised column list following a view
// name (`CREATE VIEW v (a, b, c) AS SELECT ...`).
//
// Returns []string which are the declared column names, or nil when the next token is not
// a `(`.
func (p *parser) tryParseViewColumnList() []string {
	if p.Current().Kind != tokenLeftParen {
		return nil
	}
	p.Advance()
	var names []string
	for !p.AtEnd() {
		if p.Current().Kind != tokenIdentifier {
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
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	return names
}

// analyseViewBody runs analyseSelect over the remaining tokens through a child parser.
//
// The result is nil when the body is empty, does not start with a SELECT/WITH keyword, or
// cannot be analysed; the caller then keeps the view without a typed definition. The
// remainder of the statement is always consumed.
//
// Takes declaredColumns ([]string) which are the explicit view column names that override
// the analyser's output names when present.
//
// Returns *ast.RawQueryAnalysis which is the analysed SELECT body, or nil when the body
// is empty, not a SELECT, or cannot be analysed; a body that fails only because it nests
// too deeply is recorded as the parser's syntax error rather than dropped.
func (p *parser) analyseViewBody(declaredColumns []string) *ast.RawQueryAnalysis {
	remainingTokens := p.Tokens[p.Position:]
	if len(remainingTokens) == 0 {
		p.consumeRemainder()
		return nil
	}

	viewParser := p.newChildParser(remainingTokens)
	if !viewParser.IsKeyword("SELECT") && !viewParser.IsKeyword("WITH") {
		p.consumeRemainder()
		return nil
	}
	viewAnalysis, analyseError := viewParser.analyseSelect()
	if analyseError != nil || viewAnalysis == nil {
		p.absorbChildFailure(analyseError)
		p.consumeRemainder()
		return nil
	}
	p.consumeRemainder()

	if len(declaredColumns) > 0 {
		dialectkit.OverlayViewColumnNames(viewAnalysis, declaredColumns, dialectkit.UndeclaredColumnsKept)
	}
	return viewAnalysis
}

// parseCreateMaterializedView handles the full `CREATE MATERIALIZED VIEW` grammar.
//
// It accepts the optional IF NOT EXISTS guard, a column list, the refresh clauses
// (REFRESH, RANDOMIZE FOR, DEPENDS ON, APPEND or EMPTY), a storage target (TO
// target_table or an inline ENGINE / PARTITION BY / ORDER BY declaration), the trailing
// modifiers (POPULATE, DEFINER, SQL SECURITY, COMMENT), and the AS SELECT body.
// Refreshable materialised view clauses are captured into EngineSpecific under MV_* keys.
//
// Returns *ast.CatalogueMutation which is the CREATE VIEW mutation.
// Returns error when any clause or the SELECT body cannot be parsed.
func (p *parser) parseCreateMaterializedView() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCreate); err != nil {
		return nil, err
	}
	p.skipCreatePrefixesInParser()
	if err := p.expectKeywordSequence("MATERIALIZED", "VIEW"); err != nil {
		return nil, err
	}
	ifNotExists := p.matchIfNotExists()

	database, name, err := p.parseDatabaseQualifiedName()
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(ast.MutationCreateView, database, name, ast.WithEngineSpecific(map[string]string{
		"MATERIALIZED": mvBooleanTrue,
	}), ast.WithIfNotExists(ifNotExists))
	if cluster := p.matchOnCluster(); cluster != "" {
		mutation.EngineSpecific[engineClauseOnCluster] = cluster
	}
	declaredColumns := p.tryParseViewColumnList()
	p.parseMaterializedViewRefreshClauses(mutation)
	if storageErr := p.parseMaterializedViewStorageClauses(mutation); storageErr != nil {
		return nil, storageErr
	}
	if modifierErr := p.parseMaterializedViewTrailingModifiers(mutation); modifierErr != nil {
		return nil, modifierErr
	}
	return p.parseMaterializedViewBody(mutation, declaredColumns), nil
}

// parseMaterializedViewStorageClauses recognises the optional `TO target` clause or the
// inline storage declaration (ENGINE / PARTITION / ORDER / etc.) following the refresh
// clauses.
//
// Takes mutation (*ast.CatalogueMutation) which receives the captured storage metadata in
// its EngineSpecific map.
//
// Returns error when the TO target or an inline engine clause is malformed.
func (p *parser) parseMaterializedViewStorageClauses(mutation *ast.CatalogueMutation) error {
	if p.MatchKeyword(keywordTo) {
		_, target, targetErr := p.parseDatabaseQualifiedName()
		if targetErr != nil {
			return targetErr
		}
		mutation.EngineSpecific[engineKeyMaterializedTarget] = target
		return nil
	}
	if p.IsKeyword(keywordPopulate, keywordEmpty, keywordAs, keywordDefiner, keywordSQL, engineClauseComment) {
		return nil
	}
	return p.parseTableEngineClauses(mutation)
}

// parseMaterializedViewTrailingModifiers recognises the POPULATE / EMPTY / DEFINER / SQL
// SECURITY / COMMENT modifiers placed between the storage clause and the AS SELECT body.
//
// Takes mutation (*ast.CatalogueMutation) which receives the captured modifier metadata
// in its EngineSpecific map.
//
// Returns error when a recognised security clause has a malformed body.
func (p *parser) parseMaterializedViewTrailingModifiers(mutation *ast.CatalogueMutation) error {
	if p.MatchKeyword(keywordPopulate) {
		mutation.EngineSpecific[keywordPopulate] = mvBooleanTrue
	}
	if p.MatchKeyword(keywordEmpty) {
		mutation.EngineSpecific["MV_REFRESH_EMPTY"] = mvBooleanTrue
	}
	if securityErr := p.parseMaterializedViewSecurityClauses(mutation); securityErr != nil {
		return securityErr
	}
	if p.MatchKeyword("COMMENT") && p.Current().Kind == tokenString {
		mutation.EngineSpecific[engineClauseComment] = p.Current().Value
		p.Advance()
	}
	return nil
}

// parseMaterializedViewBody analyses the optional `AS SELECT ...` body. The declared
// column list (when present) overrides the analyser's output column names so the
// materialised view's projection matches the explicit declaration.
//
// Takes mutation (*ast.CatalogueMutation) which receives the analysed view definition and
// columns.
// Takes declaredColumns ([]string) which override the analyser's output names.
//
// Returns *ast.CatalogueMutation which is the populated mutation.
func (p *parser) parseMaterializedViewBody(mutation *ast.CatalogueMutation, declaredColumns []string) *ast.CatalogueMutation {
	if !p.MatchKeyword(keywordAs) {
		p.consumeRemainder()
		return mutation
	}
	mutation.ViewDefinition = p.analyseViewBody(declaredColumns)
	if mutation.ViewDefinition != nil {
		mutation.Columns = dialectkit.ColumnsFromNames(declaredColumns)
	}
	return mutation
}

// parseMaterializedViewRefreshClauses recognises the refreshable MV REFRESH / RANDOMIZE
// FOR / DEPENDS ON / APPEND clauses.
//
// Takes mutation (*ast.CatalogueMutation) which receives the captured refresh metadata in
// its EngineSpecific map.
func (p *parser) parseMaterializedViewRefreshClauses(mutation *ast.CatalogueMutation) {
	for {
		switch {
		case p.MatchKeyword(keywordRefresh):
			p.captureRefreshClause(mutation)
		case p.MatchKeyword("RANDOMIZE"):
			if p.MatchKeyword("FOR") {
				mutation.EngineSpecific["MV_REFRESH_RANDOMIZE"] = p.captureIntervalText()
			}
		case p.MatchKeyword("DEPENDS"):
			if p.MatchKeyword("ON") {
				mutation.EngineSpecific["MV_REFRESH_DEPENDS_ON"] = p.captureDependsOnList()
			}
		case p.MatchKeyword("APPEND"):
			mutation.EngineSpecific["MV_REFRESH_APPEND"] = mvBooleanTrue
		default:
			return
		}
	}
}

// captureRefreshClause reads the body of a REFRESH clause.
//
// Takes mutation (*ast.CatalogueMutation) which receives the captured refresh kind and
// interval in its EngineSpecific map.
func (p *parser) captureRefreshClause(mutation *ast.CatalogueMutation) {
	switch {
	case p.MatchKeyword(keywordEvery):
		mutation.EngineSpecific[engineKeyMVRefreshKind] = keywordEvery
		mutation.EngineSpecific[engineKeyMVRefreshInterval] = p.captureIntervalText()
		if p.MatchKeyword("OFFSET") {
			mutation.EngineSpecific[engineKeyMVRefreshOffset] = p.captureIntervalText()
		}
	case p.MatchKeyword(keywordAfter):
		mutation.EngineSpecific[engineKeyMVRefreshKind] = keywordAfter
		mutation.EngineSpecific[engineKeyMVRefreshAfter] = p.captureIntervalText()
	}
}

// captureIntervalText reads an interval-shaped expression up to a recognised tail keyword
// or top-level boundary.
//
// The terminating set is the recognised MV boundary keyword family plus a top-level
// comma; statement splitting strips the terminators before parsing so the helper does not
// need to guard against them inside a single statement's token stream.
//
// Returns string which is the trimmed interval expression text.
func (p *parser) captureIntervalText() string {
	var builder strings.Builder
	for !p.AtEnd() {
		current := p.Current()
		if current.Kind == tokenIdentifier && isMaterializedViewBoundaryKeyword(current) {
			break
		}
		if current.Kind == tokenComma {
			break
		}
		writeTokenAsSourceText(&builder, current)
		builder.WriteByte(' ')
		p.Advance()
	}
	return strings.TrimSpace(builder.String())
}

// captureDependsOnList reads the comma-separated table identifier list following `DEPENDS
// ON`.
//
// Returns string which is the table list flattened with the dependsOnSeparator.
func (p *parser) captureDependsOnList() string {
	var entries []string
	for !p.AtEnd() {
		_, name, parseErr := p.parseDatabaseQualifiedName()
		if parseErr != nil {
			return strings.Join(entries, dependsOnSeparator)
		}
		entries = append(entries, name)
		if p.Current().Kind == tokenComma {
			p.Advance()
			continue
		}
		break
	}
	return strings.Join(entries, dependsOnSeparator)
}

// parseCreateDictionary handles `CREATE DICTIONARY [IF NOT EXISTS] [db.]dict
// (column_list) PRIMARY KEY (cols) SOURCE(name(...)) LAYOUT(name(...)) LIFETIME(MIN n MAX
// m | seconds) RANGE(MIN col MAX col) SETTINGS(k = v, ...) COMMENT 'text'`.
//
// Dictionaries are ClickHouse's key-value lookup tables. The mutation captures their
// column shape and records SOURCE / LAYOUT / LIFETIME / RANGE clause bodies under
// dedicated EngineSpecific keys.
//
// Returns a CatalogueMutation of kind MutationCreateDictionary with columns, primary key
// list, and engine-specific metadata. Returns a non-nil error when a malformed clause is
// encountered.
func (p *parser) parseCreateDictionary() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCreate); err != nil {
		return nil, err
	}
	p.skipCreatePrefixesInParser()
	if _, err := p.ExpectKeyword(keywordDictionary); err != nil {
		return nil, err
	}
	p.matchIfNotExists()

	database, name, err := p.parseDatabaseQualifiedName()
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(ast.MutationCreateDictionary, database, name, ast.WithEngineSpecific(map[string]string{
		keywordDictionary: "true",
	}))
	if cluster := p.matchOnCluster(); cluster != "" {
		mutation.EngineSpecific[engineClauseOnCluster] = cluster
	}

	if p.Current().Kind == tokenLeftParen {
		columns, columnErr := p.parseDictionaryColumnList()
		if columnErr != nil {
			return nil, columnErr
		}
		mutation.Columns = columns
	}

	for {
		handled, clauseErr := p.parseSingleDictionaryClause(mutation)
		if clauseErr != nil {
			return nil, clauseErr
		}
		if !handled {
			break
		}
	}
	p.consumeRemainder()
	return mutation, nil
}

// parseDictionaryColumnList reads the parenthesised column list of a CREATE DICTIONARY
// body. Each entry is read with parseClickHouseColumn, retaining the same column metadata
// and accepting the same modifiers as that helper.
//
// Returns []sqlmodel.Column which are the captured dictionary columns.
// Returns error when the list is not parenthesised or a column is malformed.
func (p *parser) parseDictionaryColumnList() ([]sqlmodel.Column, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected '('")
	}
	p.Advance()
	var columns []sqlmodel.Column
	for {
		if p.Current().Kind == tokenRightParen {
			p.Advance()
			return columns, nil
		}
		column, err := p.parseClickHouseColumn()
		if err != nil {
			return nil, err
		}
		columns = append(columns, column)
		switch p.Current().Kind {
		case tokenComma:
			p.Advance()
			continue
		case tokenRightParen:
			p.Advance()
			return columns, nil
		default:
			return nil, dialectkit.NewParseError(p.Current(), "expected ',' or ')' in dictionary columns")
		}
	}
}

// parseSingleDictionaryClause matches one trailing clause of a CREATE DICTIONARY body and
// writes its captured value into the mutation's EngineSpecific map.
//
// The recognised clauses are PRIMARY KEY for the key columns, SOURCE for the data source
// descriptor, LAYOUT for the in-memory layout descriptor, LIFETIME for the refresh
// schedule, RANGE for ranged dictionary bounds, SETTINGS for engine settings, and COMMENT
// for the descriptive comment.
//
// Takes mutation (*ast.CatalogueMutation) which receives the captured clause value in its
// EngineSpecific map.
//
// Returns bool which is true when a clause was consumed and false when the outer loop
// should stop.
// Returns error when a clause body is malformed.
func (p *parser) parseSingleDictionaryClause(mutation *ast.CatalogueMutation) (bool, error) {
	if p.IsKeyword("PRIMARY") {
		return p.captureDictionaryPrimaryKey(mutation)
	}
	if p.MatchKeyword("SOURCE") {
		return p.captureNamedDictionaryClause(mutation, "DICTIONARY_SOURCE")
	}
	if p.MatchKeyword("LAYOUT") {
		return p.captureNamedDictionaryClause(mutation, "DICTIONARY_LAYOUT")
	}
	if p.MatchKeyword("LIFETIME") {
		return p.captureDictionaryLifetimeClause(mutation)
	}
	if p.MatchKeyword("RANGE") {
		return p.captureParenthesisedDictionaryClause(mutation, "DICTIONARY_RANGE")
	}
	if p.MatchKeyword("SETTINGS") {
		return p.captureDictionarySettingsClause(mutation)
	}
	if p.MatchKeyword("COMMENT") {
		p.captureDictionaryCommentClause(mutation)
		return true, nil
	}
	return false, nil
}

// captureNamedDictionaryClause reads a `name(body)` clause (SOURCE, LAYOUT) and writes
// the reconstructed text under the supplied EngineSpecific key. The leading clause
// keyword has already been consumed.
//
// Takes mutation (*ast.CatalogueMutation) which receives the captured clause body.
// Takes engineKey (string) which is the EngineSpecific key to store the body under.
//
// Returns bool which is true when the clause was consumed.
// Returns error when the named paren body is malformed.
func (p *parser) captureNamedDictionaryClause(mutation *ast.CatalogueMutation, engineKey string) (bool, error) {
	body, err := p.captureNamedParenBody()
	if err != nil {
		return false, err
	}
	mutation.EngineSpecific[engineKey] = body
	return true, nil
}

// captureDictionaryLifetimeClause delegates to captureDictionaryLifetime and stores the
// result on the mutation under the canonical DICTIONARY_LIFETIME_SECONDS key.
//
// Takes mutation (*ast.CatalogueMutation) which receives the captured lifetime value.
//
// Returns bool which is true when the clause was consumed.
// Returns error when the LIFETIME body is malformed.
func (p *parser) captureDictionaryLifetimeClause(mutation *ast.CatalogueMutation) (bool, error) {
	seconds, err := p.captureDictionaryLifetime()
	if err != nil {
		return false, err
	}
	mutation.EngineSpecific["DICTIONARY_LIFETIME_SECONDS"] = seconds
	return true, nil
}

// captureParenthesisedDictionaryClause reads a parenthesised body (RANGE, SETTINGS) and
// writes the captured text under the supplied EngineSpecific key.
//
// Takes mutation (*ast.CatalogueMutation) which receives the captured clause body.
// Takes engineKey (string) which is the EngineSpecific key to store the body under.
//
// Returns bool which is true when the clause was consumed.
// Returns error when the parenthesised body is malformed.
func (p *parser) captureParenthesisedDictionaryClause(mutation *ast.CatalogueMutation, engineKey string) (bool, error) {
	body, err := p.captureParenthesisedBodyAsText()
	if err != nil {
		return false, err
	}
	mutation.EngineSpecific[engineKey] = body
	return true, nil
}

// captureDictionarySettingsClause handles the SETTINGS clause body. Unlike
// captureParenthesisedDictionaryClause it tolerates the settings-keyword form without a
// body (where the cursor is left past the keyword without a leading paren).
//
// Takes mutation (*ast.CatalogueMutation) which receives the captured settings body.
//
// Returns bool which is true when the clause was consumed.
// Returns error when the settings body is malformed.
func (p *parser) captureDictionarySettingsClause(mutation *ast.CatalogueMutation) (bool, error) {
	if p.Current().Kind != tokenLeftParen {
		return true, nil
	}
	body, err := p.captureParenthesisedBodyAsText()
	if err != nil {
		return false, err
	}
	mutation.EngineSpecific["SETTINGS"] = body
	return true, nil
}

// captureDictionaryCommentClause consumes a trailing `COMMENT 'text'` clause and writes
// the text under the EngineSpecific COMMENT key. When the cursor is not on a string
// literal the clause is dropped silently (treating the comment as empty).
//
// Takes mutation (*ast.CatalogueMutation) which receives the captured comment text.
func (p *parser) captureDictionaryCommentClause(mutation *ast.CatalogueMutation) {
	if p.Current().Kind == tokenString {
		mutation.EngineSpecific[engineClauseComment] = p.Current().Value
		p.Advance()
	}
}

// captureDictionaryPrimaryKey consumes the `PRIMARY KEY (col, ...)` or `PRIMARY KEY col`
// form and stores the key column list onto the mutation.
//
// The list is recorded under both the catalogue's PrimaryKey field and the EngineSpecific
// DICTIONARY_PRIMARY_KEY key for ClickHouse-aware consumers. The cursor is rewound and
// handled=false is returned when PRIMARY is not followed by KEY so an unrelated
// identifier starting with PRIMARY does not get misclassified.
//
// Takes mutation (*ast.CatalogueMutation) which receives the captured primary key
// columns.
//
// Returns bool which is true when the clause was consumed.
// Returns error when the key list is malformed.
func (p *parser) captureDictionaryPrimaryKey(mutation *ast.CatalogueMutation) (bool, error) {
	saved := p.Position
	p.Advance()
	if !p.MatchKeyword("KEY") {
		p.Position = saved
		return false, nil
	}
	keys, err := p.parseDictionaryPrimaryKeyList()
	if err != nil {
		return false, err
	}
	mutation.PrimaryKey = keys
	mutation.EngineSpecific["DICTIONARY_PRIMARY_KEY"] = strings.Join(keys, ", ")
	return true, nil
}

// parseDictionaryPrimaryKeyList reads the column list after a `PRIMARY KEY` keyword
// inside a CREATE DICTIONARY body. Both the parenthesised `PRIMARY KEY (a, b)` and the
// bare `PRIMARY KEY a` forms are accepted.
//
// Returns []string which are the primary key column names.
// Returns error when an identifier or closing punctuation is missing.
func (p *parser) parseDictionaryPrimaryKeyList() ([]string, error) {
	if p.Current().Kind != tokenLeftParen {
		name, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		return []string{name}, nil
	}
	p.Advance()
	var keys []string
	for {
		name, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		keys = append(keys, name)
		if p.Current().Kind == tokenComma {
			p.Advance()
			continue
		}
		if p.Current().Kind == tokenRightParen {
			p.Advance()
			return keys, nil
		}
		return nil, dialectkit.NewParseError(p.Current(), "expected ',' or ')' in PRIMARY KEY")
	}
}

// captureNamedParenBody reads a `name(body)` construct (used by SOURCE / LAYOUT clauses)
// and returns the reconstructed text "name(body_text)". Whitespace and token quoting
// follow the capture helpers rather than the original source.
//
// Returns string which is the reconstructed "name(body)" text.
// Returns error when the opening paren, name, or closing paren is missing.
func (p *parser) captureNamedParenBody() (string, error) {
	if p.Current().Kind != tokenLeftParen {
		return "", dialectkit.NewParseError(p.Current(), "expected '('")
	}
	p.Advance()
	if p.Current().Kind != tokenIdentifier {
		return "", dialectkit.NewParseError(p.Current(), "expected name identifier")
	}
	name := p.Current().Value
	p.Advance()
	body := ""
	if p.Current().Kind == tokenLeftParen {
		captured, err := p.captureParenthesisedBodyAsText()
		if err != nil {
			return "", err
		}
		body = captured
	}
	if p.Current().Kind != tokenRightParen {
		return "", dialectkit.NewParseError(p.Current(), "expected ')'")
	}
	p.Advance()
	return name + "(" + body + ")", nil
}

// captureParenthesisedBodyAsText reads a balanced parenthesised body (cursor on the
// leading `(`) and returns the inner source text without the outer parens.
//
// Nested parens are retained while whitespace and token quoting are reconstructed. The
// dictionary clause helpers use it for SOURCE/LAYOUT/RANGE/SETTINGS bodies.
//
// Returns string which is the inner body text without the outer parens.
// Returns error when the parentheses are unbalanced.
func (p *parser) captureParenthesisedBodyAsText() (string, error) {
	if p.Current().Kind != tokenLeftParen {
		return "", dialectkit.NewParseError(p.Current(), "expected '('")
	}
	opening := p.Advance()
	var builder strings.Builder
	depth := 1
	for !p.AtEnd() && depth > 0 {
		current := p.Current()
		newDepth, finished := emitCaptureToken(&builder, current, depth)
		depth = newDepth
		if finished {
			p.Advance()
			return builder.String(), nil
		}
		p.Advance()
	}
	return "", dialectkit.NewParseError(opening, messageUnmatchedParenthesis)
}

// captureDictionaryLifetime reads the LIFETIME clause body.
//
// ClickHouse accepts the single-integer form LIFETIME(seconds) and the refresh window
// form LIFETIME(MIN n MAX m) in seconds. The returned string is either the literal
// seconds value or the formatted "MIN n MAX m" pair, depending on which form the source
// used.
//
// Returns string which is the captured lifetime value.
// Returns error when the LIFETIME parentheses or contents are malformed.
func (p *parser) captureDictionaryLifetime() (string, error) {
	if p.Current().Kind != tokenLeftParen {
		return "", dialectkit.NewParseError(p.Current(), "expected '(' after LIFETIME")
	}
	p.Advance()
	var minValue, maxValue, single string
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		switch {
		case p.MatchKeyword("MIN"):
			value, err := p.captureLifetimeNumber()
			if err != nil {
				return "", err
			}
			minValue = value
		case p.MatchKeyword("MAX"):
			value, err := p.captureLifetimeNumber()
			if err != nil {
				return "", err
			}
			maxValue = value
		case p.Current().Kind == tokenNumber:
			single = p.Current().Value
			p.Advance()
		default:
			p.Advance()
		}
	}
	if p.Current().Kind != tokenRightParen {
		return "", dialectkit.NewParseError(p.Current(), "expected ')' to close LIFETIME")
	}
	p.Advance()
	return formatLifetimeValue(minValue, maxValue, single), nil
}

// captureLifetimeNumber reads a single numeric literal used as the MIN or MAX bound of a
// LIFETIME clause.
//
// Returns string which is the literal number text.
// Returns error when the cursor is not on a number.
func (p *parser) captureLifetimeNumber() (string, error) {
	if p.Current().Kind != tokenNumber {
		return "", dialectkit.NewParseError(p.Current(), "expected integer")
	}
	value := p.Current().Value
	p.Advance()
	return value, nil
}

// parseDropDictionary handles `DROP DICTIONARY [IF EXISTS] [db.]name`.
//
// Returns *ast.CatalogueMutation which is the DROP DICTIONARY mutation.
// Returns error when the dictionary name cannot be parsed.
func (p *parser) parseDropDictionary() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence(keywordDrop, keywordDictionary); err != nil {
		return nil, err
	}
	ifExists := p.matchIfExists()
	database, name, err := p.parseDatabaseQualifiedName()
	if err != nil {
		return nil, err
	}
	mutation := ast.NewCatalogueMutation(ast.MutationDropDictionary, database, name, ast.WithIfExists(ifExists))
	if cluster := p.matchOnCluster(); cluster != "" {
		mutation.EngineSpecific = map[string]string{engineClauseOnCluster: cluster}
	}
	return mutation, nil
}

// parseRenameTable handles `RENAME TABLE old TO new [, ...]` (and the DICTIONARY /
// DATABASE variants). Multiple renames in a single statement produce a primary mutation
// plus AdditionalMutations.
//
// Returns *ast.CatalogueMutation which is the primary rename mutation.
// Returns error when the RENAME keyword or any clause cannot be parsed.
func (p *parser) parseRenameTable() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("RENAME"); err != nil {
		return nil, err
	}

	if _, err := p.ExpectKeyword("TABLE", "DICTIONARY", "DATABASE"); err != nil {
		return nil, err
	}

	primary, err := p.parseSingleRename()
	if err != nil {
		return nil, err
	}
	for p.Current().Kind == tokenComma {
		p.Advance()
		additional, additionalErr := p.parseSingleRename()
		if additionalErr != nil {
			return nil, additionalErr
		}
		primary.AdditionalMutations = append(primary.AdditionalMutations, additional)
		primary.AdditionalMutations = append(primary.AdditionalMutations, additional.AdditionalMutations...)
		additional.AdditionalMutations = nil
	}
	return primary, nil
}

// parseSingleRename reads one `old [TO|AS] new` rename clause. Optional trailing `ON
// CLUSTER c` is consumed and captured into EngineSpecific.
//
// Returns *ast.CatalogueMutation which is the rename mutation.
// Returns error when the names or the TO keyword are missing.
func (p *parser) parseSingleRename() (*ast.CatalogueMutation, error) {
	oldDB, oldName, err := p.parseDatabaseQualifiedName()
	if err != nil {
		return nil, err
	}
	if !p.MatchKeyword("TO") {
		return nil, dialectkit.NewParseError(p.Current(), "expected TO")
	}
	newDB, newName, newErr := p.parseDatabaseQualifiedName()
	if newErr != nil {
		return nil, newErr
	}
	mutation := ast.NewCatalogueMutation(
		ast.MutationAlterTableRenameTable,
		oldDB,
		oldName,
		ast.WithNewName(newName),
	)
	if newDB != "" && newDB != oldDB {
		mutation.AdditionalMutations = []*ast.CatalogueMutation{ast.NewCatalogueMutation(
			ast.MutationAlterTableSetSchema,
			oldDB,
			newName,
			ast.WithNewName(newDB),
		)}
	}
	if cluster := p.matchOnCluster(); cluster != "" {
		mutation.EngineSpecific = map[string]string{engineClauseOnCluster: cluster}
	}
	return mutation, nil
}

// parseExchangeTables handles `EXCHANGE TABLES a AND b [ON CLUSTER c]`, ClickHouse's
// atomic two-table swap. The primary mutation captures the left table on its SchemaName /
// TableName pair; the right table is encoded into EngineSpecific under
// engineKeyExchangeTarget as `[db.]name` so downstream consumers can read both sides.
//
// Both halves of the swap may carry a database qualifier; the optional ON CLUSTER clause
// is captured under engineClauseOnCluster matching the convention used by
// parseSingleRename.
//
// Returns *ast.CatalogueMutation which is the EXCHANGE TABLES mutation.
// Returns error when TABLES, AND, or either table name is missing.
func (p *parser) parseExchangeTables() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("EXCHANGE"); err != nil {
		return nil, err
	}
	if !p.MatchKeyword("TABLES") {
		return nil, dialectkit.NewParseError(p.Current(), "expected TABLES after EXCHANGE")
	}
	leftDB, leftName, leftErr := p.parseDatabaseQualifiedName()
	if leftErr != nil {
		return nil, leftErr
	}
	if !p.MatchKeyword("AND") {
		return nil, dialectkit.NewParseError(p.Current(), "expected AND between exchange targets")
	}
	rightDB, rightName, rightErr := p.parseDatabaseQualifiedName()
	if rightErr != nil {
		return nil, rightErr
	}
	mutation := ast.NewCatalogueMutation(ast.MutationExchangeTables, leftDB, leftName, ast.WithEngineSpecific(map[string]string{
		engineKeyExchangeTarget: qualifiedName(rightDB, rightName),
	}))
	if cluster := p.matchOnCluster(); cluster != "" {
		mutation.EngineSpecific[engineClauseOnCluster] = cluster
	}
	return mutation, nil
}

// parseTruncateTable handles `TRUNCATE TABLE [IF EXISTS] [db.]name`. Pure runtime
// operation; no catalogue effect.
//
// Returns *ast.CatalogueMutation which is always nil for this operation.
// Returns error which is always nil because the statement is consumed wholesale.
func (p *parser) parseTruncateTable() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("TRUNCATE"); err != nil {
		return nil, err
	}
	p.MatchKeyword("TABLE")
	p.matchIfExists()
	p.consumeRemainder()
	return nil, nil
}

// parseOptimize handles the ClickHouse table-optimisation DDL (`... TABLE [db.]name
// [FINAL] [DEDUPLICATE] [BY col_list]`).
//
// This is a pure runtime operation with no catalogue effect.
//
// Returns *ast.CatalogueMutation which is always nil for this operation.
// Returns error which is always nil because the statement is consumed wholesale.
func (p *parser) parseOptimize() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("OPTIMIZE"); err != nil {
		return nil, err
	}
	p.consumeRemainder()
	return nil, nil
}

// parseSystem handles `SYSTEM FLUSH | RELOAD | START | STOP ...`. Pure runtime operation.
//
// Returns *ast.CatalogueMutation which is always nil for this operation.
// Returns error which is always nil because the statement is consumed wholesale.
func (p *parser) parseSystem() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("SYSTEM"); err != nil {
		return nil, err
	}
	p.consumeRemainder()
	return nil, nil
}

// parseUseDatabase handles `USE name`. Pure session operation; no catalogue effect.
//
// Returns *ast.CatalogueMutation which is always nil for this operation.
// Returns error which is always nil because the statement is consumed wholesale.
func (p *parser) parseUseDatabase() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("USE"); err != nil {
		return nil, err
	}
	p.consumeRemainder()
	return nil, nil
}

// parseShow handles SHOW TABLES / SHOW DATABASES / SHOW CREATE TABLE / etc. Pure
// introspection; no catalogue effect.
//
// Returns *ast.CatalogueMutation which is always nil for this operation.
// Returns error which is always nil because the statement is consumed wholesale.
func (p *parser) parseShow() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("SHOW"); err != nil {
		return nil, err
	}
	p.consumeRemainder()
	return nil, nil
}

// parseSet handles `SET name = value`. Pure session operation; no catalogue effect.
//
// Returns *ast.CatalogueMutation which is always nil for this operation.
// Returns error which is always nil because the statement is consumed wholesale.
func (p *parser) parseSet() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword("SET"); err != nil {
		return nil, err
	}
	p.consumeRemainder()
	return nil, nil
}

// markIfExists marks a mutation and each of its additional mutations as written IF EXISTS
// of the table they act on.
//
// Takes mutation (*ast.CatalogueMutation) which is the mutation, or nil.
// Takes ifExists (bool) which is true when the statement said IF EXISTS.
func markIfExists(mutation *ast.CatalogueMutation, ifExists bool) {
	if mutation == nil {
		return
	}
	mutation.IfExists = ifExists
	for _, additional := range mutation.AdditionalMutations {
		additional.IfExists = ifExists
	}
}

// isMaterializedViewBoundaryKeyword reports whether the identifier terminates an interval
// or DEPENDS-ON expression.
//
// Takes candidate (token) which is the token to test; a delimited identifier never
// matches.
//
// Returns bool which is true when the identifier ends the expression.
func isMaterializedViewBoundaryKeyword(candidate token) bool {
	switch candidate.Keyword(tokenIdentifier) {
	case "OFFSET", "RANDOMIZE", "DEPENDS", "APPEND", keywordTo, engineClauseEngine,
		"POPULATE", "EMPTY", keywordDefiner, keywordSQL, engineClauseComment, "AS",
		"PARTITION", "ORDER", "PRIMARY", "SAMPLE", engineClauseTTL, "SETTINGS":
		return true
	default:
		return false
	}
}

// emitCaptureToken writes one token's serialised form into builder (used by
// captureParenthesisedBodyAsText). Returns the updated paren depth plus finished=true
// when the matching close paren has been reached, so the caller can return the
// accumulated text.
//
// Takes builder (*strings.Builder), the destination buffer.
// Takes candidate (token), the token to emit.
// Takes depth (int), the current paren depth before this token.
//
// Returns newDepth (int), the depth after accounting for this token.
// Returns finished (bool), true when this token closed the outer parens.
func emitCaptureToken(builder *strings.Builder, candidate token, depth int) (newDepth int, finished bool) {
	switch candidate.Kind {
	case tokenLeftParen:
		builder.WriteByte('(')
		return depth + 1, false
	case tokenRightParen:
		depth--
		if depth == 0 {
			return depth, true
		}
		builder.WriteByte(')')
		return depth, false
	case tokenComma:
		builder.WriteString(", ")
	case tokenString:
		emitSpacingIfNeeded(builder)
		builder.WriteByte('\'')
		builder.WriteString(escapeClickHouseStringBody(candidate.Value))
		builder.WriteByte('\'')
	case tokenIdentifier, tokenNumber, tokenOperator:
		emitSpacingIfNeeded(builder)
		builder.WriteString(candidate.Value)
	case tokenDot:
		builder.WriteByte('.')
	default:
	}
	return depth, false
}

// escapeClickHouseStringBody re-escapes a decoded string-literal body so it can be safely
// re-wrapped in single quotes.
//
// The tokeniser stores the unescaped body (it collapses a doubled single quote and a
// backslash-escaped single quote to one single quote, and a doubled backslash to one
// backslash), so a faithful re-emit must escape the backslash first (otherwise the
// backslashes added for quotes would themselves be doubled) and then double the single
// quotes. This is the same convention the generated ClickHouse value formatter uses for
// composite literals.
//
// Takes body (string) which is the decoded string-literal contents.
//
// Returns string which is the re-escaped body without the enclosing quotes.
func escapeClickHouseStringBody(body string) string {
	escaped := strings.ReplaceAll(body, "\\", "\\\\")
	return strings.ReplaceAll(escaped, "'", "''")
}

// emitSpacingIfNeeded writes a separator space when the last emitted byte requires one
// before the next identifier-like token. Centralised so emitCaptureToken's call-sites do
// not duplicate the lastByteNeedsSpace guard.
//
// Takes builder (*strings.Builder) which is the destination buffer to space.
func emitSpacingIfNeeded(builder *strings.Builder) {
	if builder.Len() > 0 && lastByteNeedsSpace(builder.String()) {
		builder.WriteByte(' ')
	}
}

// lastByteNeedsSpace reports whether the previously emitted byte requires a separating
// space before the next identifier-like token. Used by captureParenthesisedBodyAsText to
// avoid concatenating two identifiers together when their tokens were adjacent in the
// source.
//
// Takes emitted (string) which is the text accumulated so far.
//
// Returns bool which is true when a separating space is required.
func lastByteNeedsSpace(emitted string) bool {
	if emitted == "" {
		return false
	}
	last := emitted[len(emitted)-1]
	switch last {
	case '(', ' ', '.':
		return false
	default:
		return true
	}
}

// formatLifetimeValue renders a captured LIFETIME clause body. When MIN and/or MAX bounds
// were present it emits only the bounds that actually appeared (so LIFETIME(MIN n) with
// no MAX does not produce a malformed "MIN n MAX " value with a trailing empty bound);
// otherwise it returns the single-integer form.
//
// Takes minValue (string) which is the captured MIN lifetime bound, or empty when absent.
// Takes maxValue (string) which is the captured MAX lifetime bound, or empty when absent.
// Takes single (string) which is the lifetime value used when no MIN or MAX bounds are
// present.
//
// Returns the formatted LIFETIME body.
func formatLifetimeValue(minValue, maxValue, single string) string {
	if minValue == "" && maxValue == "" {
		return single
	}
	parts := make([]string, 0, 2)
	if minValue != "" {
		parts = append(parts, "MIN "+minValue)
	}
	if maxValue != "" {
		parts = append(parts, "MAX "+maxValue)
	}
	return strings.Join(parts, " ")
}

// qualifiedName joins a database qualifier and a name into the `database.name` form,
// dropping the qualifier when empty so the output remains a single bare identifier.
//
// Takes database (string), the optional database qualifier.
// Takes name (string), the object name.
//
// Returns the qualified form, or the bare name when database is "".
func qualifiedName(database string, name string) string {
	if database == "" {
		return quoteClickHouseIdentifierIfNeeded(name)
	}
	return quoteClickHouseIdentifierIfNeeded(database) + "." + quoteClickHouseIdentifierIfNeeded(name)
}

// quoteClickHouseIdentifierIfNeeded backtick-quotes an identifier when it falls outside
// the unquoted-identifier grammar.
//
// An identifier with a leading digit, or any rune that is not a letter, digit or
// underscore, is quoted so a re-emitted qualified name remains parseable. Plain
// identifiers are returned unchanged.
//
// Takes identifier (string) which is the bare identifier to consider quoting.
//
// Returns string which is the identifier, backtick-quoted only when required.
func quoteClickHouseIdentifierIfNeeded(identifier string) string {
	if !clickHouseIdentifierNeedsQuoting(identifier) {
		return identifier
	}
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

// clickHouseIdentifierNeedsQuoting reports whether identifier needs quoting.
//
// It returns true when identifier falls outside the unquoted identifier grammar and
// therefore must be quoted to round-trip. The scan is rune-aware (delegating to
// identifierNeedsQuoting) so multi-byte Unicode letters inside an identifier are
// classified correctly rather than over-quoted byte-by-byte. An empty string is treated
// as needing quoting so it cannot collapse into an adjacent token; this differs from
// identifierNeedsQuoting, which reports an empty identifier as not needing quoting.
//
// Takes identifier (string) which is the identifier to test.
//
// Returns bool which is true when the identifier must be quoted.
func clickHouseIdentifierNeedsQuoting(identifier string) bool {
	if identifier == "" {
		return true
	}
	return identifierNeedsQuoting(identifier)
}
