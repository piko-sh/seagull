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
	"fmt"
	"strconv"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// kwWhere is the WHERE clause keyword used as a stop token.
	kwWhere = "WHERE"

	// kwGroup is the GROUP clause keyword used as a stop token.
	kwGroup = "GROUP"

	// kwHaving is the HAVING clause keyword used as a stop token.
	kwHaving = "HAVING"

	// kwOrder is the ORDER clause keyword used as a stop token.
	kwOrder = "ORDER"

	// kwLimit is the LIMIT clause keyword used as a stop token.
	kwLimit = "LIMIT"

	// kwOffset is the OFFSET clause keyword used as a stop token.
	kwOffset = "OFFSET"

	// kwSettings is the SETTINGS clause keyword used as a stop token.
	kwSettings = "SETTINGS"

	// kwFormat is the FORMAT clause keyword used as a stop token.
	kwFormat = "FORMAT"

	// kwUnion is the UNION compound-operator keyword used as a stop token.
	kwUnion = "UNION"

	// kwIntersect is the INTERSECT compound-operator keyword used as a stop token.
	kwIntersect = "INTERSECT"

	// kwExcept is the EXCEPT compound-operator keyword used as a stop token.
	kwExcept = "EXCEPT"

	// kwWith is the WITH keyword introducing a CTE list or modifier.
	kwWith = "WITH"

	// kwWindow is the WINDOW clause keyword used as a stop token.
	kwWindow = "WINDOW"

	// functionUntuple names untuple, which spreads a tuple into one value per element, as
	// the parser compares a keyword: in upper case.
	functionUntuple = "UNTUPLE"

	// kwAs is the AS alias keyword.
	kwAs = "AS"

	// kwBy is the BY keyword following GROUP, ORDER, or LIMIT.
	kwBy = "BY"

	// kwFrom is the FROM clause keyword used as a stop token.
	kwFrom = "FROM"

	// kwOn is the ON join-predicate keyword.
	kwOn = "ON"

	// kwFinal is the FINAL table-modifier keyword.
	kwFinal = "FINAL"

	// kwPrewhere is the PREWHERE clause keyword used as a stop token.
	kwPrewhere = "PREWHERE"

	// kwJoin is the JOIN keyword used as a stop token.
	kwJoin = "JOIN"

	// kwInner is the INNER join-kind keyword.
	kwInner = "INNER"

	// kwLeft is the LEFT join-kind keyword.
	kwLeft = "LEFT"

	// kwRight is the RIGHT join-kind keyword.
	kwRight = "RIGHT"

	// kwFull is the FULL join-kind keyword.
	kwFull = "FULL"

	// kwCross is the CROSS join-kind keyword.
	kwCross = "CROSS"

	// kwAsof is the ASOF join-strictness keyword.
	kwAsof = "ASOF"

	// kwSemi is the SEMI join-strictness keyword.
	kwSemi = "SEMI"

	// kwAnti is the ANTI join-strictness keyword.
	kwAnti = "ANTI"

	// kwArray is the ARRAY keyword introducing an ARRAY JOIN.
	kwArray = "ARRAY"

	// kwMaterialized is the MATERIALIZED CTE qualifier keyword.
	kwMaterialized = "MATERIALIZED"

	// kwNot is the NOT keyword, also used in NOT MATERIALIZED.
	kwNot = "NOT"

	// kwTo is the TO keyword used in LIMIT WITH FILL TO.
	kwTo = "TO"

	// kwStep is the STEP keyword used in LIMIT WITH FILL STEP.
	kwStep = "STEP"

	// kwFill is the FILL keyword used in WITH FILL.
	kwFill = "FILL"

	// kwNulls is the NULLS keyword used in ORDER BY NULLS FIRST/LAST.
	kwNulls = "NULLS"

	// kwAsc is the ASC sort-direction keyword.
	kwAsc = "ASC"

	// kwDesc is the DESC sort-direction keyword.
	kwDesc = "DESC"

	// kwFirst is the FIRST keyword used in NULLS FIRST.
	kwFirst = "FIRST"

	// kwLast is the LAST keyword used in NULLS LAST.
	kwLast = "LAST"

	// kwAny is the ANY join-strictness keyword.
	kwAny = "ANY"

	// kwAll is the ALL keyword used in UNION ALL and GROUP BY ALL.
	kwAll = "ALL"

	// kwGlobal is the GLOBAL join-distribution keyword.
	kwGlobal = "GLOBAL"

	// kwQualify is the QUALIFY clause keyword used as a stop token.
	kwQualify = "QUALIFY"

	// kwGrouping is the GROUPING keyword introducing GROUPING SETS.
	kwGrouping = "GROUPING"

	// kwSets is the SETS keyword used in GROUPING SETS.
	kwSets = "SETS"

	// kwTies is the TIES keyword used in LIMIT WITH TIES.
	kwTies = "TIES"
)

var (
	// stopAfterPrewhere terminates a PREWHERE expression scan at the clauses that may
	// legally follow PREWHERE.
	stopAfterPrewhere = []string{kwWhere, kwGroup, kwHaving, kwOrder, kwLimit, kwOffset, kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept, kwQualify}

	// stopAfterHaving terminates a HAVING expression scan at the clauses that may legally
	// follow HAVING.
	stopAfterHaving = []string{kwOrder, kwLimit, kwOffset, kwSettings, kwFormat, kwWindow, kwUnion, kwIntersect, kwExcept, kwQualify}

	// stopAfterWindow terminates a WINDOW expression scan at the clauses that may legally
	// follow WINDOW.
	stopAfterWindow = []string{kwOrder, kwLimit, kwOffset, kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept, kwQualify}

	// stopAfterQualify terminates a QUALIFY expression scan at the clauses that may legally
	// follow QUALIFY.
	stopAfterQualify = []string{kwOrder, kwLimit, kwOffset, kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept}

	// stopAfterOrderByColumn terminates the per-column expression scan inside an ORDER BY
	// list.
	//
	// It includes the column-modifier keywords ASC, DESC, NULLS, WITH, FILL, FROM, TO, and
	// STEP so each modifier attaches to the column whose expression it follows.
	stopAfterOrderByColumn = []string{
		kwLimit, kwOffset, kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept,
		kwAsc, kwDesc, kwNulls, kwWith, kwFill, kwFrom, kwTo, kwStep,
	}

	// stopArrayJoinSource terminates the textual capture of an ARRAY JOIN expression source.
	// The cursor stops on the optional AS alias keyword, the surrounding clause keywords
	// that signal the end of the FROM tail, and the join-chain keywords that may follow the
	// entry without an intervening comma.
	stopArrayJoinSource = []string{
		kwAs,
		kwWhere, kwPrewhere, kwGroup, kwHaving, kwOrder, kwLimit, kwSettings, kwFormat,
		kwUnion, kwIntersect, kwExcept, kwJoin, kwInner, kwLeft, kwRight, kwFull, kwCross,
		kwArray, kwAsof, kwSemi, kwAnti, kwOn,
	}

	// stopAfterSettings terminates a SETTINGS expression scan at the clauses that may
	// legally follow SETTINGS.
	stopAfterSettings = []string{kwFormat, kwUnion, kwIntersect, kwExcept}

	// stopFromClause is the stop-keyword set used while reading the FROM clause's expression
	// bodies (SAMPLE and ON predicates).
	//
	// It contains every clause that can legally follow FROM together with the join-related
	// keywords that mark the start of an embedded join chain. ANY, ALL, and GLOBAL are
	// included because they are valid ClickHouse strictness and distribution prefixes that
	// introduce the next join entry.
	stopFromClause = []string{
		kwWhere, kwPrewhere, kwGroup, kwHaving, kwOrder, kwLimit, kwOffset, kwSettings, kwFormat,
		kwUnion, kwIntersect, kwExcept, kwJoin, kwInner, kwLeft, kwRight, kwFull, kwCross,
		kwArray, kwAsof, kwSemi, kwAnti, kwAny, kwAll, kwGlobal,
	}

	// stopJoinOnClause is the stop-keyword set used while reading the ON predicate of a
	// JOIN.
	//
	// It contains the same keywords as stopFromClause; their order does not affect matching.
	stopJoinOnClause = []string{
		kwWhere, kwPrewhere, kwGroup, kwHaving, kwOrder, kwLimit, kwOffset, kwSettings, kwFormat,
		kwUnion, kwIntersect, kwExcept, kwJoin, kwInner, kwLeft, kwRight, kwFull, kwCross,
		kwAsof, kwSemi, kwAnti, kwArray, kwAny, kwAll, kwGlobal,
	}

	// stopGroupByClause is the stop-keyword set for the GROUP BY expression body.
	//
	// It includes WINDOW because GROUP BY can precede WINDOW in the SELECT grammar.
	stopGroupByClause = []string{kwGroup, kwHaving, kwOrder, kwLimit, kwOffset, kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept, kwWindow, kwQualify}

	// projectionStopKeywords is the stop-keyword set that terminates a single projection
	// item: the trailing alias keyword and every clause that can follow the SELECT list.
	projectionStopKeywords = []string{kwAs, kwFrom, kwWhere, kwGroup, kwHaving, kwOrder, kwLimit, kwOffset, kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept}

	// expressionKeywords are the keywords that start an expression rather than name a
	// column, so a projection item led by one is never read as a direct column reference.
	expressionKeywords = map[string]bool{
		"CASE": true, "CAST": true, "INTERVAL": true, "EXISTS": true, kwNot: true, keywordNull: true, "TRUE": true, "FALSE": true,
	}

	// infixOperatorKeywords lists the SQL keywords that act as infix operators following an
	// expression.
	//
	// cursorLooksLikeOperator uses it to disambiguate a direct column reference (`col`) from
	// the left side of a binary form (`col AND ...`, `col IN (...)`). It is declared at
	// package level so the lookup is a single map fetch rather than a switch on each call.
	infixOperatorKeywords = map[string]bool{
		"AND":     true,
		"OR":      true,
		"IN":      true,
		"BETWEEN": true,
		"LIKE":    true,
		"ILIKE":   true,
		"IS":      true,
		"NOT":     true,
	}
)

// groupByKeys holds the keys of a GROUP BY list.
type groupByKeys struct {
	// plain holds the keys written as plain grouping elements, which WITH ROLLUP or WITH
	// CUBE leave out of some rows.
	plain []ast.Expression

	// leftOut holds the keys of the list's ROLLUP (...), CUBE (...) or GROUPING SETS, which
	// they leave out of some rows.
	leftOut []ast.Expression

	// modified reports a list with ROLLUP (...), CUBE (...) or GROUPING SETS.
	modified bool
}

// fromSourceAdded records what one FROM or JOIN source added to an analysis.
type fromSourceAdded struct {
	// table is the table the source named, when it named one.
	table *ast.TableReference

	// derivedIndex is the position of the derived table the source added, or -1.
	derivedIndex int

	// joinIndex is the position of the join clause the source added, or -1.
	joinIndex int
}

// limitValue records where the placeholders of one LIMIT or OFFSET value sit in the
// analysis, so the value can be retagged once the clause shows what it is.
type limitValue struct {
	// firstReference is the index of the value's first parameter reference.
	firstReference int

	// placeholderOnly is true when the value is exactly one placeholder.
	placeholderOnly bool
}

// analyseSelect parses a SELECT statement into a RawQueryAnalysis.
//
// It handles every ClickHouse SELECT shape the engine accepts: WITH CTE clauses (regular
// and recursive), the SELECT projection list with optional DISTINCT and DISTINCT ON, FROM
// tables, subqueries, and table functions with optional FINAL and SAMPLE, INNER, LEFT,
// RIGHT, FULL, CROSS, ASOF, SEMI, and ANTI joins, ARRAY JOIN and LEFT ARRAY JOIN,
// PREWHERE and WHERE, GROUP BY with optional WITH ROLLUP, WITH CUBE, or WITH TOTALS,
// HAVING, WINDOW, ORDER BY with optional ASC, DESC, NULLS FIRST, NULLS LAST, and WITH
// FILL, LIMIT in both `LIMIT n` and `LIMIT m, n` forms with optional BY and OFFSET,
// SETTINGS assignments, UNION, INTERSECT, and EXCEPT compound branches, and a FORMAT
// specifier.
//
// The parser is bounded by maxParseDepth so adversarial inputs that nest subqueries
// unboundedly do not blow the stack. The arms of a flat UNION / INTERSECT / EXCEPT chain
// are siblings and are parsed iteratively into one flat CompoundBranches list and share a
// single depth level, so a long chain of arms never approaches the cap.
//
// Returns *ast.RawQueryAnalysis which is the parsed analysis.
// Returns error when the statement is malformed, nests too deeply, or a parameter type
// tag is bad.
func (p *parser) analyseSelect() (*ast.RawQueryAnalysis, error) {
	if p.analysisDepth >= p.maxParseDepth {
		return nil, p.depthError()
	}
	p.analysisDepth++
	defer func() { p.analysisDepth-- }()

	analysis, err := p.analyseSelectArm()
	if err != nil {
		return nil, err
	}
	if err := p.analyseCompoundBranches(analysis); err != nil {
		return nil, err
	}
	if p.syntaxError != nil {
		return nil, p.syntaxError
	}
	if p.analysisDepth == 1 && p.firstParameterTypeError != nil {
		return analysis, p.firstParameterTypeError
	}
	return analysis, nil
}

// analyseSelectArm parses one SELECT arm from the optional WITH clause to the trailing
// FORMAT specifier, stopping before any UNION / INTERSECT / EXCEPT operator.
//
// Returns *ast.RawQueryAnalysis which is the arm's analysis.
// Returns error when the arm is malformed.
func (p *parser) analyseSelectArm() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	analysis.ReadOnly = true
	p.tail = armTail{rowShaping: nil, orderBy: offsetRange{}, pagination: offsetRange{}, trailing: offsetRange{}, orderByPlace: 0}

	if err := p.analyseSelectHeader(analysis); err != nil {
		return nil, err
	}
	fromStart := p.Position
	if p.MatchKeyword(kwFrom) {
		if err := p.parseFromClause(analysis); err != nil {
			return nil, err
		}
	}
	p.analysePrewhere(analysis)
	p.recordSpan(dialectkit.SpanFrom, fromStart, p.Position)
	p.analyseSelectFilters(analysis)
	if err := p.analyseSelectGroupings(analysis); err != nil {
		return nil, err
	}
	if err := p.analyseSelectTail(analysis); err != nil {
		return nil, err
	}
	return analysis, nil
}

// analyseSelectHeader parses the WITH, SELECT, DISTINCT, projection, and FROM portions of
// a SELECT statement, for analyseSelect, which goes on to the clauses after FROM. Each
// helper it calls handles a distinct phase of the SELECT grammar.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns error when the header is malformed.
func (p *parser) analyseSelectHeader(analysis *ast.RawQueryAnalysis) error {
	if p.MatchKeyword(kwWith) {
		if err := p.parseCTEList(analysis); err != nil {
			return err
		}
	}
	if !p.MatchKeyword("SELECT") {
		return dialectkit.NewParseError(p.Current(), "expected SELECT")
	}

	quantifierStart := p.Position
	if p.MatchKeyword("DISTINCT") && p.MatchKeyword(kwOn) {
		if err := p.skipParenthesised(); err != nil {
			return err
		}
	}
	p.recordSpan(dialectkit.SpanQuantifier, quantifierStart, p.Position)
	return p.parseProjectionList(analysis)
}

// analyseSelectGroupings parses the GROUP BY, HAVING, and WINDOW clauses of a SELECT,
// including the optional WITH ROLLUP, WITH CUBE, and WITH TOTALS modifiers on GROUP BY.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns error when a grouping clause is malformed.
func (p *parser) analyseSelectGroupings(analysis *ast.RawQueryAnalysis) error {
	if p.IsKeyword(kwGroup) {
		start := p.Position
		p.Advance()
		if err := p.parseGroupByBody(analysis); err != nil {
			return err
		}
		p.recordSpan(dialectkit.SpanGroupBy, start, p.Position)
	}
	if p.IsKeyword(kwHaving) {
		start := p.Position
		p.Advance()
		conditionStart := p.Position
		p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, stopAfterHaving...)
		p.parseCondition(analysis, ast.ClauseHaving, conditionStart)
		p.recordSpan(dialectkit.SpanHaving, start, p.Position)
	}
	if p.MatchKeyword(kwWindow) {
		p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, stopAfterWindow...)
	}
	return nil
}

// parseGroupByBody handles the body of a GROUP BY clause: the BY keyword check, the
// column list, and the optional WITH ROLLUP, WITH CUBE, or WITH TOTALS trailing modifier.
//
// ClickHouse extends the SQL grammar with two extra forms. `GROUP BY ALL` groups by every
// non-aggregate column in the projection list, and the presence of the modifier is
// recorded in the analysis's detail. `GROUP BY GROUPING SETS ((a, b), (c))` groups by
// every supplied tuple in turn, equivalent to UNION ALL across one GROUP BY per set, and
// the parenthesised tuple list body is captured in the detail too.
//
// Both forms are detected by peeking the first keyword after BY; on match the regular
// column-list parser is bypassed because the grammar following ALL or GROUPING SETS does
// not contain a normal column reference list.
//
// GROUPING SETS, ROLLUP (...) and CUBE (...) in the list, and WITH ROLLUP or WITH CUBE
// after it, group some rows by fewer keys: the keys they leave out are recorded as the
// analysis's grouping sets, which fill such a key with its type's default value unless
// group_by_use_nulls is on.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns error when the GROUP BY body is malformed.
func (p *parser) parseGroupByBody(analysis *ast.RawQueryAnalysis) error {
	if !p.MatchKeyword(kwBy) {
		return dialectkit.NewParseError(p.Current(), "expected BY after GROUP")
	}
	var grouping groupByKeys
	switch {
	case p.MatchKeyword(kwAll):
		detailOf(analysis).groupByAll = true
	case p.IsKeyword(kwGrouping) && p.Peek().IsKeyword(tokenIdentifier, kwSets):
		keys, err := p.parseGroupingSetsBody(analysis)
		if err != nil {
			return err
		}
		grouping.leftOut, grouping.modified = keys, true
	default:
		keys, err := p.parseGroupByColumns(analysis)
		if err != nil {
			return err
		}
		grouping = keys
	}
	if p.MatchKeyword(kwWith) {
		if !p.IsKeyword("ROLLUP", "CUBE", "TOTALS") {
			return dialectkit.NewParseError(p.Current(), "expected ROLLUP / CUBE / TOTALS after WITH")
		}
		if p.IsKeyword("ROLLUP", "CUBE") {
			grouping.leftOut, grouping.modified = append(grouping.leftOut, grouping.plain...), true
		}
		p.Advance()
	}
	if grouping.modified {
		analysis.GroupingSets = &ast.GroupingSets{Keys: grouping.leftOut, FillsDefaults: !p.groupByUseNulls}
	}
	return nil
}

// parseGroupingSetsBody consumes the body of a `GROUP BY GROUPING SETS (...)` clause.
//
// The leading GROUPING and SETS tokens have not been consumed; the helper advances past
// both, captures the parenthesised body as flat text, and records it in the analysis's
// detail so a later consumer can re-emit or further parse the grouping list. Each key of
// the sets is recorded as a GROUP BY expression.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns []ast.Expression which holds the keys of every set, which the sets that do not
// hold them leave out of their rows.
// Returns error when the GROUPING SETS body is malformed.
func (p *parser) parseGroupingSetsBody(analysis *ast.RawQueryAnalysis) ([]ast.Expression, error) {
	p.Advance()
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected '(' after GROUPING SETS")
	}
	start := p.Position
	body, err := p.captureParenthesisedBodyAsText()
	if err != nil {
		return nil, err
	}
	detailOf(analysis).groupingSets = body
	var keys []ast.Expression
	for _, element := range topLevelElements(p.Tokens[start+1:p.Position-1], start+1) {
		first, end := element[0], element[1]
		if p.Tokens[first].Kind == tokenLeftParen && closingParenthesis(p.Tokens[first:end]) == end-first-1 {
			keys = append(keys, p.reparseList(first+1, end-1)...)
			continue
		}
		keys = append(keys, p.reparseExpression(first, end)...)
	}
	addClauseExpressions(analysis, ast.ClauseGroupBy, keys)
	return keys, nil
}

// analyseCompoundBranches handles the UNION, INTERSECT, and EXCEPT branches that may
// follow a SELECT.
//
// The arms are parsed iteratively, each through analyseSelectArm, and appended to one
// flat CompoundBranches list on the primary analysis. No arm re-enters analyseSelect, so
// a chain of N arms consumes a single analysis depth level rather than N, and the
// analyser sees every arm when it resolves and promotes the branch types. Each arm's
// parameter references are the statement's too: the arm is parsed by this parser, which
// numbers its placeholders as the statement numbers them, so they join the primary
// analysis's list, which holds every parameter nested below it.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the primary arm's analysis.
//
// Returns error when a compound branch is malformed.
func (p *parser) analyseCompoundBranches(analysis *ast.RawQueryAnalysis) error {
	for {
		operator, matched := p.matchCompoundOperator()
		if !matched {
			return nil
		}
		branch, branchErr := p.analyseSelectArm()
		if branchErr != nil {
			return branchErr
		}
		analysis.ParameterReferences = append(analysis.ParameterReferences, branch.ParameterReferences...)
		analysis.CompoundBranches = append(analysis.CompoundBranches, ast.RawCompoundBranch{
			Operator: operator,
			Query:    branch,
		})
	}
}

// parseCTEList reads the body of a `WITH cte1 AS (query), cte2 AS (query)` clause and
// stores the parsed definitions on the analysis.
//
// Scalar aliases (`WITH expr AS alias`) are recorded as expressions. WITH RECURSIVE marks
// the parsed query CTEs as recursive.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns error when the WITH clause is malformed or input ends unexpectedly.
func (p *parser) parseCTEList(analysis *ast.RawQueryAnalysis) error {
	isRecursive := p.MatchKeyword("RECURSIVE")
	for {
		if p.AtEnd() {
			return dialectkit.NewParseError(p.Current(), "expected a common table expression after WITH")
		}
		more, err := p.parseSingleCTE(analysis, isRecursive)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}

// captureCTEMaterializedQualifier consumes an optional trailing MATERIALIZED or NOT
// MATERIALIZED qualifier on a CTE definition.
//
// The qualifier is recorded in the definition's detail. The NOT branch is peeked rather
// than matched directly because NOT is also a generic predicate keyword; the qualifier is
// only consumed when the next token is MATERIALIZED.
//
// Takes definition (*ast.RawCTEDefinition) which is the CTE entry whose detail should be
// populated.
func (p *parser) captureCTEMaterializedQualifier(definition *ast.RawCTEDefinition) {
	if p.MatchKeyword(kwMaterialized) {
		definition.Detail = &cteDetail{materialized: true}
		return
	}
	if p.IsKeyword(kwNot) && p.Peek().IsKeyword(tokenIdentifier, kwMaterialized) {
		p.Advance()
		p.Advance()
		definition.Detail = &cteDetail{materialized: false}
	}
}

// parseSingleCTE consumes one CTE entry from the WITH list. Returns more=true when the
// caller should keep iterating because a trailing comma promised another entry;
// more=false otherwise.
//
// Recognises the optional trailing `MATERIALIZED` and `NOT MATERIALIZED` qualifiers the
// parser accepts after the closing paren of the CTE body. When present the qualifier is
// captured in the definition's detail.
//
// Takes analysis (*ast.RawQueryAnalysis), the analysis to populate with the parsed CTE.
// Takes isRecursive (bool), the WITH RECURSIVE flag propagated from parseCTEList.
//
// Returns more (bool), true when another CTE follows.
// Returns error when the entry is malformed.
func (p *parser) parseSingleCTE(analysis *ast.RawQueryAnalysis, isRecursive bool) (more bool, err error) {
	if p.Current().Kind != tokenIdentifier || !p.Peek().IsKeyword(tokenIdentifier, kwAs) || p.PeekAt(2).Kind != tokenLeftParen {
		return p.parseExpressionAlias(analysis)
	}
	cteName, parseErr := p.parseIdentifierOrKeyword()
	if parseErr != nil {
		return false, parseErr
	}
	p.Advance()
	body, bodyErr := p.collectParenthesised()
	if bodyErr != nil {
		return false, bodyErr
	}
	nested := p.newChildParser(body)
	nestedAnalysis, nestedErr := nested.analyseSelect()
	if nestedErr != nil {
		return false, fmt.Errorf("CTE %q: %w", cteName, nestedErr)
	}
	definition := ast.RawCTEDefinition{
		Name:             cteName,
		ColumnNames:      nil,
		OutputColumns:    nestedAnalysis.OutputColumns,
		FromTables:       nestedAnalysis.FromTables,
		JoinClauses:      nestedAnalysis.JoinClauses,
		CompoundBranches: nestedAnalysis.CompoundBranches,
		FromOrder:        nestedAnalysis.FromOrder,
		Query:            nestedAnalysis,

		ParameterReferences: nestedAnalysis.ParameterReferences,
		IsRecursive:         isRecursive,
		Detail:              nil,
	}
	p.captureCTEMaterializedQualifier(&definition)
	p.mergeNestedParameterReferences(analysis, nested, nestedAnalysis)

	analysis.CTEDefinitions = append(analysis.CTEDefinitions, definition)
	if p.Current().Kind == tokenComma {
		p.Advance()
		return true, nil
	}
	return false, nil
}

// parseProjectionColumn reads one item of the SELECT list.
//
// The analysis is threaded through so a `{name:Type}` placeholder inside a projection
// expression (for example `id + {bonus:UInt32} AS total`) is registered against the
// analysis and contributes a typed argument to the generated method.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns ast.RawOutputColumn which is the parsed projection column.
// Returns error when the projection item is malformed.
func (p *parser) parseProjectionColumn(analysis *ast.RawQueryAnalysis) (ast.RawOutputColumn, error) {
	if star, ok := p.tryParseStarProjection(); ok {
		return star, nil
	}
	if p.IsKeyword(functionUntuple) && p.Peek().Kind == tokenLeftParen {
		return ast.RawOutputColumn{}, dialectkit.NewParseError(p.Current(), "untuple(...) in a select list "+
			"gives one column for each element of the tuple, which the analysis cannot type; select the elements "+
			"instead, as t.1 and t.2 or tupleElement(t, 'name')")
	}

	saved := p.Position
	keywordColumn := p.IsKeyword(kwFrom)
	tableAlias, columnName, isDirectColumn := p.tryParseDirectColumnReference()
	if isDirectColumn && p.startsTypedLiteralAt(saved) {
		isDirectColumn = false
		p.Position = saved
	}
	if isDirectColumn && !p.cursorLooksLikeOperator() {
		return p.finaliseDirectColumnProjection(tableAlias, columnName, keywordColumn && tableAlias == "")
	}
	if isDirectColumn {
		p.Position = saved
	}

	expression, aliasAt := p.tryProjectionExpressionTree()
	if aliasAt >= 0 {
		p.consumeTokensUntil(analysis, aliasAt, sqlmodel.ParameterContextComparison)
	} else {
		p.consumeExpressionUntilCommaOrKeyword(
			analysis, sqlmodel.ParameterContextComparison,
			projectionStopKeywords...,
		)
	}
	alias, _, err := p.parseOptionalAlias()
	if err != nil {
		return ast.RawOutputColumn{}, err
	}
	if alias == "" {
		alias = elementAccessPath(expression)
	}
	return ast.RawOutputColumn{
		Name:       alias,
		Expression: expression,
		TableAlias: "",
		ColumnName: "",
		IsStar:     false,
	}, nil
}

// tryParseStarProjection consumes a `*` or `table.*` star expansion when the cursor is
// positioned on one. Returns ok=false otherwise so the caller can fall through to the
// regular column / expression paths.
//
// Returns column (ast.RawOutputColumn), the star projection with TableAlias populated for
// qualified stars.
// Returns ok (bool), true when a star projection was consumed.
func (p *parser) tryParseStarProjection() (column ast.RawOutputColumn, ok bool) {
	if p.Current().Kind == tokenStar {
		p.Advance()
		return ast.RawOutputColumn{
			IsStar:     true,
			Expression: nil,
			Name:       "",
			TableAlias: "",
			ColumnName: "",
		}, true
	}
	if p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenDot {
		identifier := p.Current().Value
		const qualifiedStarLookahead = 2
		if p.Position+qualifiedStarLookahead < len(p.Tokens) && p.Tokens[p.Position+qualifiedStarLookahead].Kind == tokenStar {
			p.Advance()
			p.Advance()
			p.Advance()
			return ast.RawOutputColumn{
				IsStar:     true,
				TableAlias: identifier,
				Expression: nil,
				Name:       "",
				ColumnName: "",
			}, true
		}
	}
	return ast.RawOutputColumn{}, false
}

// finaliseDirectColumnProjection assigns the display name of a direct column projection.
//
// Explicit aliases take precedence. Qualified references wait for element resolution so
// the name reflects whether the qualifier is a table or a column.
//
// Takes tableAlias (string), the table qualifier captured from the reference (empty for
// bare-column references).
// Takes columnName (string), the column identifier.
// Takes aliasNeedsAs (bool), true for a column written as the bare keyword FROM.
//
// Returns the populated RawOutputColumn and any alias parse error.
func (p *parser) finaliseDirectColumnProjection(tableAlias string, columnName string, aliasNeedsAs bool) (ast.RawOutputColumn, error) {
	alias := ""
	if !aliasNeedsAs || p.IsKeyword(kwAs) {
		parsed, _, err := p.parseOptionalAlias()
		if err != nil {
			return ast.RawOutputColumn{}, err
		}
		alias = parsed
	}
	name := columnName
	switch {
	case alias != "":
		name = alias
	case tableAlias != "":
		name = ""
	}
	return ast.RawOutputColumn{
		Name:       name,
		TableAlias: tableAlias,
		ColumnName: columnName,
		Expression: nil,
		IsStar:     false,
	}, nil
}

// consumeExpressionUntilCommaOrKeyword consumes tokens, honouring paren depth, until
// reaching a top-level comma or one of the named clause keywords.
//
// The projection-list reader uses it so each item's trailing alias is left for
// parseOptionalAlias to consume. Any `{name:Type}` placeholder encountered is registered
// against the analysis so projection-list parameters are retained.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register placeholders
// against.
// Takes context (sqlmodel.ParameterContext) which is the context to record for any
// placeholder found.
// Takes stopKeywords (...string) which are the clause keywords that halt the scan.
func (p *parser) consumeExpressionUntilCommaOrKeyword(analysis *ast.RawQueryAnalysis, context sqlmodel.ParameterContext, stopKeywords ...string) {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && tokenIsTopLevelStop(current, stopKeywords) {
			return
		}
		if current.Kind == tokenClickHouseParam {
			p.registerClickHouseParameter(analysis, current, context)
		}
		newDepth, halt := advanceParenDepth(current, depth)
		if halt {
			return
		}
		depth = newDepth
		p.Advance()
	}
}

// tryParseDirectColumnReference checks whether the cursor is on a bare column reference
// (`col`, `t.col`) NOT followed by a paren (which would mean a function call) and not a
// keyword that starts an expression, such as CASE.
//
// On success advances the cursor past the reference and returns table alias + column
// name; on miss returns false with the cursor unchanged.
//
// Returns tableAlias (string), the qualifier portion of a qualified reference, or "" for
// a bare-column form.
// Returns columnName (string), the column identifier.
// Returns matched (bool), true when a column reference was consumed.
func (p *parser) tryParseDirectColumnReference() (tableAlias string, columnName string, matched bool) {
	saved := p.Position
	if p.Current().Kind != tokenIdentifier || expressionKeywords[p.Current().Keyword(tokenIdentifier)] {
		return "", "", false
	}
	first := p.Current().Value
	p.Advance()
	if p.Current().Kind == tokenLeftParen {
		p.Position = saved
		return "", "", false
	}
	if p.Current().Kind != tokenDot {
		return "", first, true
	}
	p.Advance()
	if p.Current().Kind != tokenIdentifier {
		p.Position = saved
		return "", "", false
	}
	second := p.Current().Value
	p.Advance()
	if p.Current().Kind == tokenLeftParen {
		p.Position = saved
		return "", "", false
	}
	return first, second, true
}

// parseOptionalAlias reads an optional `[AS] alias` after a projection-list expression.
//
// Without AS, the keyword rejection list covers every clause-starting word so downstream
// clause handlers such as WINDOW, OFFSET, and WITH see the keyword still on the stream
// rather than consumed as a table alias. After an explicit AS any word is the alias, as
// ClickHouse reads it, so `AS inner` names a derived table. Every keyword is referenced
// through the kw* constants declared at the top of this file so the alias rejection set
// shares a single source of truth with the stop-keyword slices used by the consume
// helpers.
//
// Returns string which is the alias, or "" when none is present.
// Returns bool which is true when the AS keyword was present.
// Returns error when AS is followed by no alias.
func (p *parser) parseOptionalAlias() (string, bool, error) {
	entry := p.Position
	hadAs := p.MatchKeyword(kwAs)
	if hadAs {
		p.aliasStart = entry
	}
	if p.Current().Kind != tokenIdentifier {
		if hadAs {
			return "", hadAs, dialectkit.NewParseError(p.Current(), messageExpectedAlias)
		}
		return "", hadAs, nil
	}

	if !hadAs && p.IsKeyword(
		kwFrom, kwWhere, kwGroup, kwHaving, kwOrder, kwLimit, kwOffset,
		kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept, kwJoin,
		kwInner, kwLeft, kwRight, kwFull, kwCross, kwOn, "USING",
		kwPrewhere, kwArray, kwFinal, "SAMPLE", kwAsof, kwSemi, kwAnti,
		kwWindow, kwWith, "RECURSIVE", kwBy, kwTies, kwFill,
		kwQualify, kwAny, kwAll, kwGlobal,
	) {
		return "", hadAs, nil
	}
	alias := p.Current().Value
	p.aliasStart = entry
	p.Advance()
	return alias, hadAs, nil
}

// cursorLooksLikeOperator reports whether the current token would extend a preceding
// expression as an infix operator such as `+`, `=`, `IN`, or `AND`.
//
// parseProjectionColumn uses it to disambiguate `SELECT col, ...` (direct column) from
// `SELECT col + 1, ...` or `SELECT col * 2, ...` (computed expression; the tokeniser
// gives `*` its own star kind, which after a column can only be multiplication). Without
// this check the direct-column branch consumes `col` and leaves the binary operator
// dangling, corrupting subsequent projection parsing. The keyword set is owned by
// infixOperatorKeywords at package level so the membership test is a single map fetch.
//
// Returns bool which is true when the current token continues an expression.
func (p *parser) cursorLooksLikeOperator() bool {
	current := p.Current()
	if current.Kind == tokenOperator {
		return true
	}
	if current.Kind == tokenCast {
		return true
	}
	if current.Kind == tokenLeftBracket {
		return true
	}
	if current.Kind == tokenStar {
		return true
	}
	if current.Kind == tokenDot {
		return true
	}
	if current.Kind == tokenIdentifier {
		return infixOperatorKeywords[current.Keyword(tokenIdentifier)]
	}
	return false
}

// parseFromClause reads the FROM clause body, supporting tables, derived tables
// (subqueries), table-valued functions, and JOINs.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns error when the FROM clause is malformed.
func (p *parser) parseFromClause(analysis *ast.RawQueryAnalysis) error {
	if _, err := p.parseRecordedFromSource(analysis, ast.JoinInner); err != nil {
		return err
	}
	if err := p.parseFromTrailingModifiers(analysis); err != nil {
		return err
	}
	if err := p.parseFromArrayJoinChain(analysis); err != nil {
		return err
	}
	return p.parseFromJoinChain(analysis)
}

// parseFromTrailingModifiers consumes the FINAL and SAMPLE clauses that may follow the
// first table reference.
//
// SAMPLE accepts an expression body which the analyser captures so ClickHouse
// `{name:Type}` placeholders inside the sample expression are reflected in the parameter
// list.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns error when a SAMPLE expression is malformed.
func (p *parser) parseFromTrailingModifiers(analysis *ast.RawQueryAnalysis) error {
	for {
		if p.MatchKeyword(kwFinal) {
			continue
		}
		if p.MatchKeyword("SAMPLE") {
			p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, stopFromClause...)
			continue
		}
		return nil
	}
}

// parseFromJoinChain processes the JOIN chain that may follow the initial table
// reference.
//
// Each JOIN reads a fresh source and its ON or USING qualifier; ON predicates run through
// the parameter tracker because ClickHouse parameters can appear in join conditions.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns error when a JOIN clause is malformed.
func (p *parser) parseFromJoinChain(analysis *ast.RawQueryAnalysis) error {
	for {
		joinKind, matched := p.matchJoinKeyword()
		if !matched {
			return nil
		}
		source, err := p.parseRecordedFromSource(analysis, joinKind)
		if err != nil {
			return err
		}
		_ = p.MatchKeyword(kwFinal)
		using, err := p.parseJoinQualifier(analysis)
		if err != nil {
			return err
		}
		attachJoinUsing(analysis, source, joinKind, using)
	}
}

// parseRecordedFromSource reads one FROM or JOIN source and records the name it is
// visible under in the analysis's FROM order.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
// Takes joinKind (ast.JoinKind) which is the join kind for this source.
//
// Returns fromSourceAdded which says what the source added.
// Returns error when the source is malformed.
func (p *parser) parseRecordedFromSource(analysis *ast.RawQueryAnalysis, joinKind ast.JoinKind) (fromSourceAdded, error) {
	tablesBefore := len(analysis.FromTables)
	joinsBefore := len(analysis.JoinClauses)
	derivedBefore := len(analysis.RawDerivedTables)
	functionsBefore := len(analysis.RawTableValuedFunctions)
	added := fromSourceAdded{table: nil, derivedIndex: -1, joinIndex: -1}
	if err := p.parseFromSource(analysis, joinKind); err != nil {
		return added, err
	}
	name := ""
	switch {
	case len(analysis.FromTables) > tablesBefore:
		added.table = &analysis.FromTables[tablesBefore]
		name = added.table.Alias
		if name == "" {
			name = added.table.Name
		}
		if len(analysis.JoinClauses) > joinsBefore {
			added.joinIndex = joinsBefore
		}
	case len(analysis.RawDerivedTables) > derivedBefore:
		added.derivedIndex = derivedBefore
		name = analysis.RawDerivedTables[derivedBefore].Alias
	case len(analysis.RawTableValuedFunctions) > functionsBefore:
		name = analysis.RawTableValuedFunctions[functionsBefore].Alias
	}
	if name != "" {
		analysis.FromOrder = append(analysis.FromOrder, name)
	}
	return added, nil
}

// parseJoinQualifier reads the ON or USING clause that follows a JOIN in the chain
// parseFromJoinChain reads.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns *ast.JoinUsing which describes the columns a USING clause merges, leading the
// joined row as ClickHouse places them, or nil for an ON clause.
// Returns error when the USING column list is malformed.
func (p *parser) parseJoinQualifier(analysis *ast.RawQueryAnalysis) (*ast.JoinUsing, error) {
	if p.MatchKeyword(kwOn) {
		start := p.Position
		p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, stopJoinOnClause...)
		p.parseCondition(analysis, ast.ClauseJoinCondition, start)
		return nil, nil
	}
	if !p.MatchKeyword("USING") {
		return nil, nil
	}
	parenthesised := p.Current().Kind == tokenLeftParen
	if parenthesised {
		p.Advance()
	}
	var columns []string
	for {
		column, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		columns = append(columns, column)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if parenthesised {
		if p.Current().Kind != tokenRightParen {
			return nil, dialectkit.NewParseError(p.Current(), "expected ')' to close the USING column list")
		}
		p.Advance()
	}
	return &ast.JoinUsing{Columns: columns, Natural: false, Placement: ast.JoinColumnsLead}, nil
}

// parseFromSource reads one table reference in a FROM or JOIN clause, handling plain
// tables, derived-table subqueries, and table-valued function calls.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
// Takes joinKind (ast.JoinKind) which is the join kind for this source.
//
// Returns error when the table reference is malformed.
func (p *parser) parseFromSource(analysis *ast.RawQueryAnalysis, joinKind ast.JoinKind) error {
	if p.Current().Kind == tokenLeftParen {
		return p.parseFromDerivedSubquery(analysis, joinKind)
	}
	first, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return err
	}
	if p.Current().Kind == tokenDot {
		return p.parseFromQualifiedTable(analysis, joinKind, first)
	}
	if p.Current().Kind == tokenLeftParen {
		return p.parseFromTableValuedFunction(analysis, joinKind, first)
	}
	alias, _, aliasErr := p.parseOptionalAlias()
	if aliasErr != nil {
		return aliasErr
	}
	appendTableReference(analysis, ast.TableReference{
		Name:   first,
		Alias:  alias,
		Schema: "",
	}, joinKind)
	return nil
}

// parseFromDerivedSubquery handles `(SELECT ...) [AS] alias` derived table references.
//
// The subquery is fully re-parsed via a nested parser so the analysis records the inner
// output columns and join chain alongside the alias.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
// Takes joinKind (ast.JoinKind) which is the join kind for this source.
//
// Returns error when the derived subquery is malformed.
func (p *parser) parseFromDerivedSubquery(analysis *ast.RawQueryAnalysis, joinKind ast.JoinKind) error {
	body, err := p.collectParenthesised()
	if err != nil {
		return err
	}
	nested := p.newChildParser(body)
	nestedAnalysis, nestedErr := nested.analyseSelect()
	if nestedErr != nil {
		return nestedErr
	}
	alias, _, aliasErr := p.parseOptionalAlias()
	if aliasErr != nil {
		return aliasErr
	}
	p.mergeNestedParameterReferences(analysis, nested, nestedAnalysis)
	analysis.RawDerivedTables = append(analysis.RawDerivedTables, ast.RawDerivedTableReference{
		InnerQuery:  nestedAnalysis,
		Alias:       alias,
		ColumnNames: nil,
		JoinKind:    joinKind,
		Using:       nil,
	})
	return nil
}

// parseFromQualifiedTable handles `schema.table [AS] alias` after the leading identifier
// and dot have already been consumed.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
// Takes joinKind (ast.JoinKind) which is the join kind for this source.
// Takes schemaName (string) which is the leading schema qualifier.
//
// Returns error when the qualified table reference is malformed.
func (p *parser) parseFromQualifiedTable(analysis *ast.RawQueryAnalysis, joinKind ast.JoinKind, schemaName string) error {
	p.Advance()
	second, secondErr := p.parseIdentifierOrKeyword()
	if secondErr != nil {
		return secondErr
	}

	if p.Current().Kind == tokenLeftParen {
		if err := p.consumeTableFunctionArguments(analysis); err != nil {
			return err
		}
		alias, _, aliasErr := p.parseOptionalAlias()
		if aliasErr != nil {
			return aliasErr
		}
		analysis.RawTableValuedFunctions = append(analysis.RawTableValuedFunctions, ast.RawTableValuedFunctionReference{
			FunctionName:      schemaName + "." + second,
			Arguments:         nil,
			Alias:             alias,
			JoinKind:          joinKind,
			ColumnDefinitions: nil,
			Ordinality:        nil,
			Functions:         nil,
		})
		return nil
	}
	alias, _, aliasErr := p.parseOptionalAlias()
	if aliasErr != nil {
		return aliasErr
	}
	appendTableReference(analysis, ast.TableReference{
		Schema: schemaName,
		Name:   second,
		Alias:  alias,
	}, joinKind)
	return nil
}

// parseFromTableValuedFunction handles a table-valued function call like `numbers(10)
// [AS] alias`. The argument list is consumed opaquely; downstream resolution maps the
// function name to its declared column shape.
//
// The argument list is not analysed, so argument types are not inferred from the
// function, but each `{name:Type}` placeholder in it is registered for binding, typed by
// its own tag.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
// Takes joinKind (ast.JoinKind) which is the join kind for this source.
// Takes functionName (string) which is the table function name.
//
// Returns error when the table-valued function reference is malformed.
func (p *parser) parseFromTableValuedFunction(analysis *ast.RawQueryAnalysis, joinKind ast.JoinKind, functionName string) error {
	if err := p.consumeTableFunctionArguments(analysis); err != nil {
		return err
	}
	alias, _, aliasErr := p.parseOptionalAlias()
	if aliasErr != nil {
		return aliasErr
	}
	analysis.RawTableValuedFunctions = append(analysis.RawTableValuedFunctions, ast.RawTableValuedFunctionReference{
		FunctionName:      functionName,
		Arguments:         nil,
		Alias:             alias,
		JoinKind:          joinKind,
		ColumnDefinitions: nil,
		Ordinality:        nil,
		Functions:         nil,
	})
	return nil
}

// parseWhereClause consumes the WHERE expression. Tracks any `{name:Type}` parameter
// references encountered and adds them to the analysis with ParameterContextComparison.
//
// Placeholder types come from their explicit tags rather than from column-based
// inference. The caller records the predicate expression separately.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
func (p *parser) parseWhereClause(analysis *ast.RawQueryAnalysis) {
	p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, stopGroupByClause...)
}

// parseGroupByColumns reads the GROUP BY column list.
//
// ClickHouse allows expressions and column ordinals (`GROUP BY 1, 2`); the helper records
// direct column references and reparses the consumed expressions. A ROLLUP (...) or CUBE
// (...) element groups some rows by fewer of its keys rather than calling a function. A
// key that is an integer alone names the select item at that position.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns groupByKeys which holds the list's keys.
// Returns error when a GROUP BY column is malformed.
func (p *parser) parseGroupByColumns(analysis *ast.RawQueryAnalysis) (groupByKeys, error) {
	var keys groupByKeys
	for {
		if p.AtEnd() {
			return keys, nil
		}

		start := p.Position
		if p.IsKeyword("ROLLUP", "CUBE") && p.Peek().Kind == tokenLeftParen {
			p.consumeOneExpression(analysis, sqlmodel.ParameterContextComparison)
			inner := p.reparseList(start+2, p.Position-1)
			addClauseExpressions(analysis, ast.ClauseGroupBy, inner)
			keys.leftOut, keys.modified = append(keys.leftOut, inner...), true
		} else {
			table, column, isDirect := p.tryParseDirectColumnReference()
			if isDirect {
				analysis.GroupByColumns = append(analysis.GroupByColumns, ast.ColumnReference{
					TableAlias: table,
					ColumnName: column,
				})
			} else {
				p.consumeOneExpression(analysis, sqlmodel.ParameterContextComparison)
			}
			expressions := p.reparseExpression(start, p.Position)
			addClauseExpressions(analysis, ast.ClauseGroupBy, expressions)
			keys.plain = append(keys.plain, p.groupingKeys(start, expressions, analysis.OutputColumns)...)
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	return keys, nil
}

// groupingKeys returns the keys a plain GROUP BY element stands for: the element, or, for
// an integer alone, the expression of the select item at that position.
//
// Takes start (int) which is the index of the element's first token.
// Takes expressions ([]ast.Expression) which holds the element as parsed.
// Takes outputs ([]ast.RawOutputColumn) which is the select list.
//
// Returns []ast.Expression which holds the keys.
func (p *parser) groupingKeys(start int, expressions []ast.Expression, outputs []ast.RawOutputColumn) []ast.Expression {
	if p.Position != start+1 || p.Tokens[start].Kind != tokenNumber {
		return expressions
	}
	position, err := strconv.Atoi(p.Tokens[start].Value)
	if err != nil || position < 1 || position > len(outputs) || outputs[position-1].Expression == nil {
		return expressions
	}
	return []ast.Expression{outputs[position-1].Expression}
}

// parseLimitClause reads a LIMIT clause body.
//
// ClickHouse supports `LIMIT n`, `LIMIT n OFFSET m`, `LIMIT m, n`, `LIMIT n WITH TIES`,
// and `LIMIT n BY cols`. Every sub-expression is consumed via the parameter-tracking
// helper so `LIMIT {n:UInt32}` and `LIMIT {n:UInt32} BY user_id` register the placeholder
// against the analysis. A placeholder that is the whole row count is tagged as a LIMIT
// parameter, and one that is the whole offset as an OFFSET parameter, so its default and
// max are applied as on every other dialect. The first expression's stop list includes
// the top-level comma so the `LIMIT m, n` form parses correctly: in that form the first
// expression is the offset and the second is the row count.
//
// A LIMIT n BY clause ends at the next LIMIT or OFFSET, so `LIMIT 1 BY id LIMIT
// {n:UInt32}` keeps its second LIMIT as the statement's row count rather than reading it
// as part of the BY list. The BY list's expressions are recorded as a clause that shapes
// the rows.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns bool which is true for the LIMIT n BY form.
func (p *parser) parseLimitClause(analysis *ast.RawQueryAnalysis) bool {
	limitFirstStops := []string{kwOffset, kwBy, kwWith, kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept}
	limitOffsetStops := []string{kwBy, kwWith, kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept}
	limitByStops := []string{kwLimit, kwOffset, kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept, kwWith}
	firstValue := p.consumeLimitValue(analysis, sqlmodel.ParameterContextLimit, limitFirstStops)
	for {
		switch {
		case p.MatchKeyword(kwOffset):
			p.consumeLimitValue(analysis, sqlmodel.ParameterContextOffset, limitOffsetStops)
		case p.MatchKeyword(kwWith):
			p.parseLimitWithModifier(analysis)
		case p.MatchKeyword(kwBy):
			start := p.Position
			p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, limitByStops...)
			addClauseExpressions(analysis, ast.ClauseRowShaping, p.reparseList(start, p.Position))
			return true
		case p.Current().Kind == tokenComma:
			p.Advance()
			retagLimitValue(analysis, firstValue, sqlmodel.ParameterContextOffset)
			p.consumeLimitValue(analysis, sqlmodel.ParameterContextLimit, limitFirstStops)
		default:
			return false
		}
	}
}

// parseOffsetClause reads a standalone OFFSET and any following FETCH clause.
//
// Parameters supplying their counts are tagged for offset or limit validation.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
func (p *parser) parseOffsetClause(analysis *ast.RawQueryAnalysis) {
	offsetStops := []string{"ROW", "ROWS", "FETCH", kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept}
	fetchStops := []string{"ROW", "ROWS", kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept}
	p.consumeLimitValue(analysis, sqlmodel.ParameterContextOffset, offsetStops)
	p.matchRowsKeyword()
	if !p.MatchKeyword("FETCH") {
		return
	}
	if !p.MatchKeyword(kwFirst) {
		p.MatchKeyword("NEXT")
	}
	p.consumeLimitValue(analysis, sqlmodel.ParameterContextLimit, fetchStops)
	p.matchRowsKeyword()
	if p.MatchKeyword(kwWith) {
		p.MatchKeyword(kwTies)
		return
	}
	p.MatchKeyword("ONLY")
}

// matchRowsKeyword consumes an optional ROW or ROWS noise word.
func (p *parser) matchRowsKeyword() {
	if !p.MatchKeyword("ROW") {
		p.MatchKeyword("ROWS")
	}
}

// consumeLimitValue consumes one row-count or offset expression of a LIMIT, OFFSET or
// FETCH clause, registering its placeholders, and tags the placeholder with the given
// context when it is the whole expression. A placeholder inside a larger expression keeps
// the comparison context, because its value is not the row count itself.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the parameter references.
// Takes context (sqlmodel.ParameterContext) which is ParameterContextLimit or
// ParameterContextOffset.
// Takes stopKeywords ([]string) which are the keywords that end the expression.
//
// Returns limitValue which locates the value's parameter references.
func (p *parser) consumeLimitValue(analysis *ast.RawQueryAnalysis, context sqlmodel.ParameterContext, stopKeywords []string) limitValue {
	value := limitValue{firstReference: len(analysis.ParameterReferences), placeholderOnly: false}
	start := p.Position
	p.consumeExpressionTrackingParamsCommaAware(analysis, sqlmodel.ParameterContextComparison, stopKeywords...)
	value.placeholderOnly = p.Position == start+1 && p.Tokens[start].Kind == tokenClickHouseParam
	retagLimitValue(analysis, value, context)
	return value
}

// matchCompoundOperator recognises UNION ALL, UNION DISTINCT, INTERSECT, and EXCEPT and
// consumes the operator tokens.
//
// Returns ast.CompoundOperator which is the matched operator enum value.
// Returns bool which is true when an operator was consumed.
func (p *parser) matchCompoundOperator() (ast.CompoundOperator, bool) {
	switch {
	case p.MatchKeyword("UNION"):
		if p.MatchKeyword("ALL") {
			return ast.CompoundUnionAll, true
		}
		p.MatchKeyword("DISTINCT")
		return ast.CompoundUnion, true
	case p.MatchKeyword("INTERSECT"):
		return ast.CompoundIntersect, true
	case p.MatchKeyword("EXCEPT"):
		return ast.CompoundExcept, true
	}
	return 0, false
}

// topLevelElements splits a token list at its commas outside any parentheses.
//
// Takes tokens ([]token) which is the list.
// Takes offset (int) which is the index of the list's first token in the statement.
//
// Returns [][2]int which holds, for each non-empty element, the statement indexes of its
// first token and of the token after its last.
func topLevelElements(tokens []token, offset int) [][2]int {
	var elements [][2]int
	depth, first := 0, 0
	for index := range tokens {
		switch tokens[index].Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		case tokenComma:
			if depth == 0 {
				if index > first {
					elements = append(elements, [2]int{offset + first, offset + index})
				}
				first = index + 1
			}
		default:
		}
	}
	if len(tokens) > first {
		elements = append(elements, [2]int{offset + first, offset + len(tokens)})
	}
	return elements
}

// tokenIsTopLevelStop reports whether candidate terminates a depth-0 expression scan.
//
// A top-level comma and any identifier matching one of the supplied stop keywords both
// halt the scan.
//
// Takes candidate (token) which is the token under inspection.
// Takes stopKeywords ([]string) which are the clause keywords that halt the scan.
//
// Returns bool which is true when the token terminates the scan.
func tokenIsTopLevelStop(candidate token, stopKeywords []string) bool {
	if candidate.Kind == tokenComma {
		return true
	}
	if candidate.Kind != tokenIdentifier {
		return false
	}
	for _, stop := range stopKeywords {
		if candidate.IsKeyword(tokenIdentifier, stop) {
			return true
		}
	}
	return false
}

// advanceParenDepth updates the running nesting depth for the supplied token, counting
// parentheses, square brackets and the braces of map literals, so a comma inside an array
// literal such as [1, 2] never ends an expression.
//
// Takes candidate (token) which is the token under inspection.
// Takes depth (int) which is the current nesting depth before this token.
//
// Returns newDepth (int) which is the nesting depth after this token.
// Returns halt (bool) which is true on an unbalanced closing parenthesis or bracket at
// depth 0.
func advanceParenDepth(candidate token, depth int) (newDepth int, halt bool) {
	switch candidate.Kind {
	case tokenLeftParen, tokenLeftBracket, tokenLeftBrace:
		return depth + 1, false
	case tokenRightParen, tokenRightBracket, tokenRightBrace:
		if depth == 0 {
			return depth, true
		}
		return depth - 1, false
	default:
		return depth, false
	}
}

// attachJoinUsing records the columns a JOIN's USING clause merges on the relation the
// JOIN added. An inner join records no join clause of its own, so one is added for it.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
// Takes source (fromSourceAdded) which says what the JOIN's source added.
// Takes joinKind (ast.JoinKind) which is the join kind.
// Takes using (*ast.JoinUsing) which describes the merge, or nil for none.
func attachJoinUsing(analysis *ast.RawQueryAnalysis, source fromSourceAdded, joinKind ast.JoinKind, using *ast.JoinUsing) {
	switch {
	case using == nil:
	case source.joinIndex >= 0:
		analysis.JoinClauses[source.joinIndex].Using = using
	case source.table != nil:
		analysis.JoinClauses = append(analysis.JoinClauses, ast.JoinClause{Table: *source.table, Kind: joinKind, Using: using})
	case source.derivedIndex >= 0:
		analysis.RawDerivedTables[source.derivedIndex].Using = using
	}
}

// retagLimitValue sets the context of a LIMIT or OFFSET value's placeholder when the
// placeholder is the whole value.
//
// Takes analysis (*ast.RawQueryAnalysis) which holds the parameter references.
// Takes value (limitValue) which locates the value's references.
// Takes context (sqlmodel.ParameterContext) which is the context to set.
func retagLimitValue(analysis *ast.RawQueryAnalysis, value limitValue, context sqlmodel.ParameterContext) {
	if !value.placeholderOnly || value.firstReference >= len(analysis.ParameterReferences) {
		return
	}
	analysis.ParameterReferences[value.firstReference].Context = context
}

// appendTableReference adds the table to the FROM list, and also records a JoinClause
// when the join kind is non-Inner so downstream consumers can track per-join nullability.
//
// The first FROM entry is always treated as inner-joined; subsequent joined entries need
// both the FromTables append and the JoinClauses append.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
// Takes reference (ast.TableReference) which is the table reference to append.
// Takes joinKind (ast.JoinKind) which is the join kind for this table.
func appendTableReference(analysis *ast.RawQueryAnalysis, reference ast.TableReference, joinKind ast.JoinKind) {
	analysis.FromTables = append(analysis.FromTables, reference)
	if len(analysis.FromTables) > 1 && joinKind != ast.JoinInner {
		analysis.JoinClauses = append(analysis.JoinClauses, ast.JoinClause{
			Table: reference,
			Kind:  joinKind,
			Using: nil,
		})
	}
}

// isTopClauseKeyword reports whether a token is one of the top-level clause keywords that
// terminates an expression. A delimited identifier is a name, so it never is.
//
// Takes candidate (token) which is the token to test.
//
// Returns bool which is true when the token is a top-level clause keyword.
func isTopClauseKeyword(candidate token) bool {
	switch candidate.Keyword(tokenIdentifier) {
	case kwFrom, kwWhere, kwGroup, kwHaving, kwOrder, kwLimit, kwSettings,
		kwFormat, kwUnion, kwIntersect, kwExcept, kwJoin, kwInner, kwLeft,
		kwRight, kwFull, kwCross, kwOn, "USING", kwPrewhere, kwWindow,
		kwArray, kwFinal, "SAMPLE", kwAsof, kwSemi, kwAnti, kwOffset, kwBy,
		kwWith, kwQualify, kwAny, kwAll, kwGlobal:
		return true
	}
	return false
}
