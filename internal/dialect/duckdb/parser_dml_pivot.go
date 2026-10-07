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

package duckdb

import (
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// defaultUnpivotNameColumn is the column UNPIVOT writes the unpivoted column names to
	// when no INTO NAME is given.
	defaultUnpivotNameColumn = "name"

	// defaultUnpivotValueColumn is the column UNPIVOT writes the unpivoted values to when no
	// VALUE is given.
	defaultUnpivotValueColumn = "value"

	// pivotSourceAlias is the alias a pivoted subquery is known by when it has none, so its
	// columns resolve inside the pivot.
	pivotSourceAlias = "pivot_source"

	// maxPivotOutputColumns is how many output columns one PIVOT may make, one for each
	// combination of its IN lists' values and each aggregate, unless WithMaxPivotColumns
	// sets another limit. A few IN lists of a few dozen values each would otherwise make
	// hundreds of thousands of columns from a short statement.
	maxPivotOutputColumns = 4096

	// pivotNeedsValuesMessage reports a PIVOT whose output columns come from the data: an ON
	// column without an IN list makes one column per value the data holds.
	pivotNeedsValuesMessage = "a PIVOT ON column without an IN list makes one output column per value in the " +
		"data, so its columns cannot be known when generating code: list the values to pivot on, such as " +
		"ON species IN ('cod', 'hake')"

	// pivotNeedsGroupingMessage reports a PIVOT or UNPIVOT whose source columns cannot be
	// listed when it relies on them for its output columns.
	pivotNeedsGroupingMessage = "this PIVOT or UNPIVOT takes its remaining output columns from its source, " +
		"which cannot be listed here: pivot a table the migrations create, or a subquery that names its columns, " +
		"or give PIVOT a GROUP BY"
)

// pivotSource is what a PIVOT or UNPIVOT reads: a base table or a subquery, and the
// columns it holds when they can be listed.
type pivotSource struct {
	// table is the base table, or nil for a subquery.
	table *ast.TableReference

	// derivedTables holds the subquery or table function the source was read from, which
	// joins the pivot's FROM clause.
	derivedTables []ast.RawDerivedTableReference

	// functions holds a table-valued function source.
	functions []ast.RawTableValuedFunctionReference

	// columns lists the source's column names, or nil when they cannot be listed.
	columns []string
}

// pivotValue is one value of a PIVOT's IN list and the name its output column takes.
type pivotValue struct {
	// name is the value's alias, or the value as written.
	name string
}

// pivotAggregate is one aggregate of a PIVOT's USING list.
type pivotAggregate struct {
	// expression is the aggregate expression.
	expression ast.Expression

	// name is the aggregate's alias, or its text when it has none.
	name string
}

// pivotShape is the parsed shape of a PIVOT.
type pivotShape struct {
	// onTokens holds the first token of each pivoted expression, where a problem with that
	// expression is reported.
	onTokens []token

	// onColumns holds the pivoted expressions.
	onColumns []ast.Expression

	// valueLists holds each pivoted expression's IN list.
	valueLists [][]pivotValue

	// aggregates holds the aggregates; empty stands for count(*).
	aggregates []pivotAggregate

	// groupColumns holds the GROUP BY columns.
	groupColumns []ast.ColumnReference

	// keyword is the PIVOT keyword the pivot opens with, where a problem with the pivot as a
	// whole is reported.
	keyword token

	// explicitGroup is true when the PIVOT has a GROUP BY.
	explicitGroup bool
}

// standardUnpivot is what the head of a SQL-standard UNPIVOT clause says.
type standardUnpivot struct {
	// keyword is the UNPIVOT keyword.
	keyword token

	// includeNulls is true when rows whose value is NULL are kept.
	includeNulls bool
}

// unpivotShape is the parsed shape of an UNPIVOT.
type unpivotShape struct {
	// nameColumn is the column the unpivoted column names go to.
	nameColumn string

	// valueColumn is the column the unpivoted values go to.
	valueColumn string

	// unpivoted holds the columns turned into rows.
	unpivoted []string

	// keyword is the UNPIVOT keyword the unpivot opens with, where a problem with the
	// unpivot as a whole is reported.
	keyword token

	// includeNulls is true when rows whose value is NULL are kept.
	includeNulls bool
}

// analysePivotStatement analyses DuckDB's PIVOT and UNPIVOT statements, and their
// PIVOT_WIDER and PIVOT_LONGER spellings, as the query they stand for, so their output
// columns are known. A subquery the statement reads joins this parser's FROM-clause
// state, from which the statement's analysis is finalised.
//
// Takes analysis (*ast.RawQueryAnalysis) which holds any CTE list parsed before the
// statement and receives the pivot.
//
// Returns *ast.RawQueryAnalysis which is the pivot's analysis.
// Returns error when the statement is malformed or its columns depend on the data.
func (p *parser) analysePivotStatement(analysis *ast.RawQueryAnalysis) (*ast.RawQueryAnalysis, error) {
	unpivot := p.IsKeyword(keywordUNPIVOT, keywordPIVOTLONGER)
	keyword := p.Advance()
	source, err := p.parsePivotStatementSource()
	if err != nil {
		return nil, err
	}
	if unpivot {
		err = p.parseUnpivotStatementBody(analysis, source, keyword)
	} else {
		err = p.parsePivotStatementBody(analysis, source, keyword)
	}
	if err != nil {
		p.recordSyntaxError(err)
		return nil, err
	}
	p.rawDerivedTables = append(p.rawDerivedTables, analysis.RawDerivedTables...)
	p.rawTableValuedFunctions = append(p.rawTableValuedFunctions, analysis.RawTableValuedFunctions...)
	return analysis, nil
}

// parsePivotStatementSource parses the source of a PIVOT or UNPIVOT statement: a table,
// optionally aliased, or a parenthesised query.
//
// Returns pivotSource which is the source.
// Returns error when the source is malformed.
func (p *parser) parsePivotStatementSource() (pivotSource, error) {
	derivedBefore := len(p.rawDerivedTables)
	functionsBefore := len(p.rawTableValuedFunctions)
	tableReference, err := p.parseTableSourceBody(ast.JoinInner, new([]ast.JoinClause))
	if err != nil {
		return pivotSource{}, err
	}
	return p.takePivotSource(tableReference, derivedBefore, functionsBefore), nil
}

// takePivotSource builds the pivot source from the table a source parse returned, or
// moves the subquery or table function it recorded out of this parser's FROM clause.
//
// Takes tableReference (*ast.TableReference) which is the base table, or nil.
// Takes derivedBefore (int) which is the derived-table count before the source.
// Takes functionsBefore (int) which is the table-function count before the source.
//
// Returns pivotSource which is the source with its columns when they can be listed.
func (p *parser) takePivotSource(tableReference *ast.TableReference, derivedBefore, functionsBefore int) pivotSource {
	source := pivotSource{
		table:         tableReference,
		derivedTables: slices.Clone(p.rawDerivedTables[derivedBefore:]),
		functions:     slices.Clone(p.rawTableValuedFunctions[functionsBefore:]),
		columns:       nil,
	}
	p.rawDerivedTables = p.rawDerivedTables[:derivedBefore]
	p.rawTableValuedFunctions = p.rawTableValuedFunctions[:functionsBefore]
	source.columns = p.pivotSourceColumns(source)
	for index := range source.derivedTables {
		if source.derivedTables[index].Alias == "" {
			source.derivedTables[index].Alias = pivotSourceAlias
		}
	}
	return source
}

// pivotSourceColumns lists a pivot source's column names: a subquery's named output
// columns, or a table's or view's columns from the catalogue.
//
// Takes source (pivotSource) which is the source.
//
// Returns []string which holds the column names, or nil when they cannot be listed.
func (p *parser) pivotSourceColumns(source pivotSource) []string {
	if source.table != nil {
		return p.catalogueColumns(source.table.Schema, source.table.Name)
	}
	if len(source.derivedTables) != 1 || len(source.functions) != 0 || source.derivedTables[0].InnerQuery == nil {
		return nil
	}
	var names []string
	for _, column := range source.derivedTables[0].InnerQuery.OutputColumns {
		if column.IsStar || column.Name == "" {
			return nil
		}
		names = append(names, column.Name)
	}
	return names
}

// catalogueColumns lists the columns of a table or view in the catalogue the query is
// analysed against.
//
// Takes schema (string) which is the qualifier, or empty.
// Takes name (string) which is the table or view name.
//
// Returns []string which holds the column names, or nil when the catalogue does not hold
// the relation.
func (p *parser) catalogueColumns(schema, name string) []string {
	if p.catalogue == nil {
		return nil
	}
	var columns []sqlmodel.Column
	if table := p.catalogue.LookupTable(schema, name); table != nil {
		columns = table.Columns
	} else if view := p.catalogue.LookupView(schema, name); view != nil {
		columns = view.Columns
	} else {
		return nil
	}
	names := make([]string, 0, len(columns))
	for index := range columns {
		names = append(names, columns[index].Name)
	}
	return names
}

// parsePivotStatementBody parses ON columns [IN (values)], USING aggregates and GROUP BY
// of a PIVOT statement and fills analysis with the query it stands for.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the pivot.
// Takes source (pivotSource) which is what the pivot reads.
// Takes keyword (token) which is the PIVOT or PIVOT_WIDER the statement opens with.
//
// Returns error when the body is malformed, the columns depend on the data, or they are
// more than the dialect's limit.
func (p *parser) parsePivotStatementBody(analysis *ast.RawQueryAnalysis, source pivotSource, keyword token) error {
	if _, err := p.ExpectKeyword(keywordON); err != nil {
		return err
	}
	onTokens, onColumns, valueLists, err := p.parsePivotOnList(keywordUSING, keywordGROUP)
	if err != nil {
		return err
	}
	var aggregates []pivotAggregate
	if p.MatchKeyword(keywordUSING) {
		aggregates = p.parsePivotAggregates()
	}
	var groupColumns []ast.ColumnReference
	explicitGroup := false
	if p.MatchKeyword(keywordGROUP) {
		if _, err := p.ExpectKeyword(keywordBY); err != nil {
			return err
		}
		groupColumns, _ = p.parseGroupByClause(nil)
		explicitGroup = true
	}
	return fillPivotAnalysis(analysis, source, pivotShape{
		keyword:       keyword,
		onTokens:      onTokens,
		onColumns:     onColumns,
		valueLists:    valueLists,
		aggregates:    aggregates,
		groupColumns:  groupColumns,
		explicitGroup: explicitGroup,
	}, p.maxPivotColumns)
}

// parsePivotOnList parses the pivoted columns and their IN lists, up to one of the
// keywords that follow them.
//
// Takes followers (...string) which are the keywords that end the list.
//
// Returns []token which holds the first token of each pivoted expression.
// Returns []ast.Expression which holds the pivoted expressions.
// Returns [][]pivotValue which holds each expression's IN list, or nil for one without.
// Returns error when an IN list is malformed.
func (p *parser) parsePivotOnList(followers ...string) ([]token, []ast.Expression, [][]pivotValue, error) {
	var onTokens []token
	var onColumns []ast.Expression
	var valueLists [][]pivotValue
	for {
		onTokens = append(onTokens, p.Current())
		onColumns = append(onColumns, p.parseBitwiseExpression())
		var values []pivotValue
		if p.MatchKeyword(keywordIN) {
			parsed, err := p.parsePivotValues()
			if err != nil {
				return nil, nil, nil, err
			}
			values = parsed
		}
		valueLists = append(valueLists, values)
		if p.Current().Kind != tokenComma || p.IsKeyword(followers...) {
			break
		}
		p.Advance()
	}
	return onTokens, onColumns, valueLists, nil
}

// parsePivotValues parses the parenthesised IN list of a pivoted column: literal values,
// each with an optional alias naming its column.
//
// Returns []pivotValue which holds the values in order.
// Returns error when the list is malformed.
func (p *parser) parsePivotValues() ([]pivotValue, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected '(' to open the IN list")
	}
	p.Advance()
	var values []pivotValue
	for p.Current().Kind != tokenRightParen {
		valueToken := p.Current()
		p.parseExpression()
		name := valueToken.Value
		if p.MatchKeyword(keywordAS) {
			alias, err := p.parseIdentifierOrKeyword()
			if err != nil {
				return nil, err
			}
			name = alias
		}
		values = append(values, pivotValue{name: name})
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected ')' to close the IN list")
	}
	p.Advance()
	return values, nil
}

// parsePivotAggregates parses the aggregates of a PIVOT, each with an optional alias.
//
// Returns []pivotAggregate which holds the aggregates in order.
func (p *parser) parsePivotAggregates() []pivotAggregate {
	var aggregates []pivotAggregate
	for {
		startIndex := p.Position
		expression := p.parseExpression()
		name := aggregateText(p.Tokens[startIndex:p.Position])
		if p.MatchKeyword(keywordAS) {
			alias, err := p.parseIdentifierOrKeyword()
			p.recordSyntaxError(err)
			name = alias
		}
		aggregates = append(aggregates, pivotAggregate{expression: expression, name: name})
		if p.Current().Kind != tokenComma {
			return aggregates
		}
		p.Advance()
	}
}

// parseUnpivotStatementBody parses ON columns INTO NAME name VALUE value of an UNPIVOT
// statement and fills analysis with the query it stands for.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the unpivot.
// Takes source (pivotSource) which is what the unpivot reads.
// Takes keyword (token) which is the UNPIVOT or PIVOT_LONGER the statement opens with.
//
// Returns error when the body is malformed or the source's columns cannot be listed.
func (p *parser) parseUnpivotStatementBody(analysis *ast.RawQueryAnalysis, source pivotSource, keyword token) error {
	if _, err := p.ExpectKeyword(keywordON); err != nil {
		return err
	}
	unpivoted, err := p.parseUnpivotColumns(keywordINTO)
	if err != nil {
		return err
	}
	nameColumn, valueColumn := defaultUnpivotNameColumn, defaultUnpivotValueColumn
	if p.MatchKeyword(keywordINTO) {
		if nameColumn, valueColumn, err = p.parseUnpivotInto(); err != nil {
			return err
		}
	}
	return fillUnpivotAnalysis(analysis, source, unpivotShape{
		keyword:      keyword,
		unpivoted:    unpivoted,
		nameColumn:   nameColumn,
		valueColumn:  valueColumn,
		includeNulls: false,
	})
}

// parseUnpivotColumns parses the columns an UNPIVOT turns into rows.
//
// Takes followers (...string) which are the keywords that end the list.
//
// Returns []string which holds the column names.
// Returns error when an entry is not a plain column, such as COLUMNS(*), whose columns
// cannot be listed here.
func (p *parser) parseUnpivotColumns(followers ...string) ([]string, error) {
	var names []string
	for {
		if p.Current().Kind != tokenIdentifier || p.Peek().Kind == tokenLeftParen {
			return nil, newUnanalysableError(p.Current(),
				"UNPIVOT needs its columns listed by name, so its output columns can be known when generating code")
		}
		names = append(names, p.Advance().Value)
		if p.Current().Kind != tokenComma || p.IsKeyword(followers...) {
			return names, nil
		}
		p.Advance()
	}
}

// parseUnpivotInto parses the NAME name VALUE value after an UNPIVOT's INTO.
//
// Returns nameColumn (string) which is the name column.
// Returns valueColumn (string) which is the value column.
// Returns err (error) when either is missing.
func (p *parser) parseUnpivotInto() (nameColumn string, valueColumn string, err error) {
	if _, err = p.ExpectKeyword("NAME"); err != nil {
		return "", "", err
	}
	if nameColumn, err = p.parseIdentifierOrKeyword(); err != nil {
		return "", "", err
	}
	if _, err = p.ExpectKeyword(keywordVALUE); err != nil {
		return "", "", err
	}
	if valueColumn, err = p.parseIdentifierOrKeyword(); err != nil {
		return "", "", err
	}
	if p.Current().Kind == tokenComma {
		return "", "", newUnanalysableError(p.Current(), "UNPIVOT into more than one VALUE column is not supported")
	}
	return nameColumn, valueColumn, nil
}

// parsePivotSuffix parses the SQL-standard PIVOT (aggregates FOR column IN (values) ...)
// or UNPIVOT [INCLUDE | EXCLUDE NULLS] (value FOR name IN (columns)) that follows a FROM
// item, replacing the item with the pivot as a derived table.
//
// Takes joinKind (ast.JoinKind) which records how the item is joined into the FROM
// clause.
// Takes source (pivotSource) which is the item being pivoted.
//
// Returns error when the clause is malformed or its columns depend on the data.
func (p *parser) parsePivotSuffix(joinKind ast.JoinKind, source pivotSource) error {
	defer p.dropClauseExpressions(len(p.clauseExpressions))
	unpivot := p.IsKeyword(keywordUNPIVOT)
	keyword := p.Advance()
	includeNulls := false
	if unpivot && p.MatchKeyword("INCLUDE") {
		includeNulls = true
		p.MatchKeyword(keywordNULLS)
	} else if unpivot && p.MatchKeyword("EXCLUDE") {
		p.MatchKeyword(keywordNULLS)
	}
	if p.Current().Kind != tokenLeftParen {
		return p.syntaxErrorAt("expected '('")
	}
	p.Advance()
	pivot := &ast.RawQueryAnalysis{}
	var err error
	if unpivot {
		err = p.parseStandardUnpivot(pivot, source, standardUnpivot{keyword: keyword, includeNulls: includeNulls})
	} else {
		err = p.parseStandardPivot(pivot, source, keyword)
	}
	if err != nil {
		p.recordSyntaxError(err)
		return err
	}
	if p.Current().Kind != tokenRightParen {
		return p.syntaxErrorAt("expected ')' to close the " + pivotKeyword(unpivot))
	}
	p.Advance()
	pivot.ReadOnly = true
	alias := p.parseTableAlias()
	if alias == "" {
		alias = strings.ToLower(pivotKeyword(unpivot))
	}
	p.rawDerivedTables = append(p.rawDerivedTables, ast.RawDerivedTableReference{Alias: alias, ColumnNames: nil, InnerQuery: pivot,
		JoinKind: joinKind, Using: nil})
	return nil
}

// parseStandardPivot parses the body of a SQL-standard PIVOT: aggregates FOR column IN
// (values) [column IN (values) ...] [GROUP BY columns].
//
// Takes pivot (*ast.RawQueryAnalysis) which receives the query the pivot stands for.
// Takes source (pivotSource) which is the item being pivoted.
// Takes keyword (token) which is the PIVOT keyword.
//
// Returns error when the body is malformed, its columns depend on the data, or they are
// more than the dialect's limit.
func (p *parser) parseStandardPivot(pivot *ast.RawQueryAnalysis, source pivotSource, keyword token) error {
	aggregates := p.parsePivotAggregates()
	if _, err := p.ExpectKeyword(keywordFOR); err != nil {
		return err
	}
	var onTokens []token
	var onColumns []ast.Expression
	var valueLists [][]pivotValue
	for p.Current().Kind != tokenRightParen && !p.IsKeyword(keywordGROUP) && !p.AtEnd() {
		onTokens = append(onTokens, p.Current())
		onColumns = append(onColumns, p.parseBitwiseExpression())
		if _, err := p.ExpectKeyword(keywordIN); err != nil {
			return err
		}
		values, err := p.parsePivotValues()
		if err != nil {
			return err
		}
		valueLists = append(valueLists, values)
	}
	var groupColumns []ast.ColumnReference
	explicitGroup := false
	if p.MatchKeyword(keywordGROUP) {
		if _, err := p.ExpectKeyword(keywordBY); err != nil {
			return err
		}
		groupColumns, _ = p.parseGroupByClause(nil)
		explicitGroup = true
	}
	return fillPivotAnalysis(pivot, source, pivotShape{
		keyword: keyword, onTokens: onTokens, onColumns: onColumns, valueLists: valueLists, aggregates: aggregates,
		groupColumns: groupColumns, explicitGroup: explicitGroup,
	}, p.maxPivotColumns)
}

// parseStandardUnpivot parses the body of a SQL-standard UNPIVOT: value FOR name IN
// (columns).
//
// Takes pivot (*ast.RawQueryAnalysis) which receives the query the unpivot stands for.
// Takes source (pivotSource) which is the item being unpivoted.
// Takes clause (standardUnpivot) which holds the UNPIVOT keyword and what it does with
// NULL values.
//
// Returns error when the body is malformed or the source's columns cannot be listed.
func (p *parser) parseStandardUnpivot(pivot *ast.RawQueryAnalysis, source pivotSource, clause standardUnpivot) error {
	valueColumn, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return err
	}
	if _, err := p.ExpectKeyword(keywordFOR); err != nil {
		return err
	}
	nameColumn, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return err
	}
	if _, err := p.ExpectKeyword(keywordIN); err != nil {
		return err
	}
	if p.Current().Kind != tokenLeftParen {
		return dialectkit.NewParseError(p.Current(), "expected '(' to open the IN list")
	}
	p.Advance()
	unpivoted, err := p.parseUnpivotColumns()
	if err != nil {
		return err
	}
	if p.Current().Kind != tokenRightParen {
		return dialectkit.NewParseError(p.Current(), "expected ')' to close the IN list")
	}
	p.Advance()
	return fillUnpivotAnalysis(pivot, source, unpivotShape{
		keyword: clause.keyword, unpivoted: unpivoted, nameColumn: nameColumn, valueColumn: valueColumn,
		includeNulls: clause.includeNulls,
	})
}

// aggregateText writes an aggregate as DuckDB names an unaliased one in a PIVOT's column
// names: its tokens without spacing and the function lower-cased, with count(*) written
// count_star().
//
// Takes tokens ([]token) which are the aggregate's tokens.
//
// Returns string which is the aggregate's text.
func aggregateText(tokens []token) string {
	var builder strings.Builder
	for index, current := range tokens {
		if current.Kind == tokenIdentifier && index+1 < len(tokens) && tokens[index+1].Kind == tokenLeftParen {
			builder.WriteString(strings.ToLower(current.Value))
			continue
		}
		builder.WriteString(current.Raw)
	}
	text := builder.String()
	if text == "count(*)" {
		return "count_star()"
	}
	return text
}

// fillPivotAnalysis fills an analysis with the query a PIVOT stands for: the grouping
// columns, then one column for each combination of the IN lists' values and each
// aggregate, typed by the aggregate over the source.
//
// The number of value columns is worked out, without overflowing, before any is made, so
// a pivot over long IN lists is refused rather than built.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the query.
// Takes source (pivotSource) which is what the pivot reads.
// Takes shape (pivotShape) which is the parsed pivot.
// Takes maxColumns (int) which is the most value columns the pivot may make.
//
// Returns error when a pivoted column has no IN list, the value columns are more than
// maxColumns, or the grouping columns cannot be listed.
func fillPivotAnalysis(analysis *ast.RawQueryAnalysis, source pivotSource, shape pivotShape, maxColumns int) error {
	for index, values := range shape.valueLists {
		if len(values) == 0 {
			return newUnanalysableError(shape.onTokens[index], pivotNeedsValuesMessage)
		}
	}
	aggregates := shape.aggregates
	if len(aggregates) == 0 {
		aggregates = []pivotAggregate{{expression: countStar(), name: "count_star()"}}
	}
	columnCount, fits := pivotColumnCount(shape.valueLists, len(aggregates), maxColumns)
	if !fits {
		return newLimitError(errPivotColumnLimit, shape.keyword,
			fmt.Sprintf("its IN lists and aggregates make more than %d output columns; pivot on fewer values, or raise "+
				"the limit with WithMaxPivotColumns", maxColumns))
	}
	groupColumns := shape.groupColumns
	if !shape.explicitGroup {
		implicit, listed := implicitPivotGroup(source, shape.onColumns, aggregates)
		if !listed {
			return newUnanalysableError(shape.keyword, pivotNeedsGroupingMessage)
		}
		groupColumns = implicit
	}
	applyPivotSource(analysis, source)
	analysis.GroupByColumns = groupColumns
	for _, column := range groupColumns {
		analysis.OutputColumns = append(analysis.OutputColumns, ast.RawOutputColumn{
			Expression: nil, Name: column.ColumnName, TableAlias: column.TableAlias, ColumnName: column.ColumnName, IsStar: false,
		})
	}
	analysis.OutputColumns = append(analysis.OutputColumns, pivotValueColumns(shape.valueLists, aggregates, columnCount)...)
	return nil
}

// pivotColumnCount works out how many value columns a PIVOT makes, the product of its IN
// lists' lengths and its aggregate count, stopping as soon as the product passes a limit
// so it never overflows.
//
// Takes valueLists ([][]pivotValue) which holds each pivoted column's values.
// Takes aggregates (int) which is how many aggregates the pivot has.
// Takes limit (int) which is the most columns allowed.
//
// Returns int which is the number of columns, when it fits.
// Returns bool which is false when the columns are more than limit.
func pivotColumnCount(valueLists [][]pivotValue, aggregates int, limit int) (int, bool) {
	if aggregates > limit {
		return 0, false
	}
	count := aggregates
	for _, values := range valueLists {
		if len(values) > 0 && count > limit/len(values) {
			return 0, false
		}
		count *= len(values)
	}
	return count, true
}

// countStar returns the count(*) a PIVOT without aggregates counts with.
//
// Returns ast.Expression which is the count(*) call.
func countStar() ast.Expression {
	return &ast.FunctionCallExpression{FunctionName: "count", Schema: "", Arguments: nil, FilterExpression: nil, Span: ast.Span{}}
}

// implicitPivotGroup lists the columns a PIVOT without GROUP BY groups by: every column
// of its source that it neither pivots on nor aggregates.
//
// Takes source (pivotSource) which is what the pivot reads.
// Takes onColumns ([]ast.Expression) which are the pivoted expressions.
// Takes aggregates ([]pivotAggregate) which are the aggregates.
//
// Returns []ast.ColumnReference which holds the grouping columns.
// Returns bool which is false when the source's columns cannot be listed.
func implicitPivotGroup(source pivotSource, onColumns []ast.Expression, aggregates []pivotAggregate) ([]ast.ColumnReference, bool) {
	if source.columns == nil {
		return nil, false
	}
	used := map[string]bool{}
	for _, expression := range onColumns {
		markColumns(used, columnNamesIn(expression))
	}
	for _, aggregate := range aggregates {
		markColumns(used, columnNamesIn(aggregate.expression))
	}
	var groupColumns []ast.ColumnReference
	for _, name := range source.columns {
		if !used[strings.ToLower(name)] {
			groupColumns = append(groupColumns, ast.ColumnReference{TableAlias: "", ColumnName: name})
		}
	}
	return groupColumns, true
}

// pivotValueColumns makes a PIVOT's value columns: one for each combination of the IN
// lists' values and each aggregate, typed by the aggregate.
//
// Takes valueLists ([][]pivotValue) which holds each pivoted column's values.
// Takes aggregates ([]pivotAggregate) which are the aggregates.
// Takes columnCount (int) which is how many columns they make, as pivotColumnCount works
// it out.
//
// Returns []ast.RawOutputColumn which holds the columns in DuckDB's order.
func pivotValueColumns(valueLists [][]pivotValue, aggregates []pivotAggregate, columnCount int) []ast.RawOutputColumn {
	combinations := pivotCombinations(valueLists)
	columns := make([]ast.RawOutputColumn, 0, columnCount)
	for _, combination := range combinations {
		for _, aggregate := range aggregates {
			name := combination
			if len(aggregates) > 1 {
				name += "_" + aggregate.name
			}
			columns = append(columns, ast.RawOutputColumn{
				Expression: aggregate.expression, Name: name, TableAlias: "", ColumnName: "", IsStar: false,
			})
		}
	}
	return columns
}

// markColumns records column names, lower-cased, in a set.
//
// Takes used (map[string]bool) which is the set.
// Takes names ([]string) which are the names to record.
func markColumns(used map[string]bool, names []string) {
	for _, name := range names {
		used[strings.ToLower(name)] = true
	}
}

// pivotCombinations names each combination of the IN lists' values, in the order DuckDB
// makes their columns: the values joined by underscores, the first list varying slowest.
//
// Takes valueLists ([][]pivotValue) which holds each pivoted column's values.
//
// Returns []string which holds the combinations' names.
func pivotCombinations(valueLists [][]pivotValue) []string {
	combinations := []string{""}
	for _, values := range valueLists {
		next := make([]string, 0, len(combinations)*len(values))
		for _, prefix := range combinations {
			for _, value := range values {
				if prefix == "" {
					next = append(next, value.name)
					continue
				}
				next = append(next, prefix+"_"+value.name)
			}
		}
		combinations = next
	}
	return combinations
}

// applyPivotSource makes a pivot source the FROM clause of the query a pivot stands for.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the source.
// Takes source (pivotSource) which is the source.
func applyPivotSource(analysis *ast.RawQueryAnalysis, source pivotSource) {
	if source.table != nil {
		analysis.FromTables = append(analysis.FromTables, *source.table)
	}
	analysis.RawDerivedTables = append(analysis.RawDerivedTables, source.derivedTables...)
	analysis.RawTableValuedFunctions = append(analysis.RawTableValuedFunctions, source.functions...)
}

// fillUnpivotAnalysis fills an analysis with the query an UNPIVOT stands for: the
// source's other columns, the name column, and the value column, typed as the common type
// of the unpivoted columns.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the query.
// Takes source (pivotSource) which is what the unpivot reads.
// Takes shape (unpivotShape) which is the parsed unpivot.
//
// Returns error when the source's columns cannot be listed.
func fillUnpivotAnalysis(analysis *ast.RawQueryAnalysis, source pivotSource, shape unpivotShape) error {
	if source.columns == nil {
		return newUnanalysableError(shape.keyword, pivotNeedsGroupingMessage)
	}
	used := map[string]bool{}
	markColumns(used, shape.unpivoted)
	applyPivotSource(analysis, source)
	for _, name := range source.columns {
		if used[strings.ToLower(name)] {
			continue
		}
		analysis.OutputColumns = append(analysis.OutputColumns, ast.RawOutputColumn{
			Expression: nil, Name: name, TableAlias: "", ColumnName: name, IsStar: false,
		})
	}
	values := make([]ast.Expression, 0, len(shape.unpivoted))
	for _, name := range shape.unpivoted {
		values = append(values, &ast.ColumnRefExpression{TableAlias: "", ColumnName: name, Span: ast.Span{}})
	}
	var valueExpression ast.Expression = &ast.CoalesceExpression{Arguments: values}
	if shape.includeNulls {
		branches := make([]ast.CaseWhenBranch, 0, len(values))
		for _, value := range values {
			branches = append(branches, ast.CaseWhenBranch{Condition: &ast.UnknownExpression{}, Result: value})
		}
		valueExpression = &ast.CaseWhenExpression{Branches: branches, ElseResult: nil}
	}
	analysis.OutputColumns = append(analysis.OutputColumns,
		ast.RawOutputColumn{Expression: &ast.LiteralExpression{TypeName: "varchar"}, Name: shape.nameColumn, TableAlias: "", ColumnName: "", IsStar: false},
		ast.RawOutputColumn{Expression: valueExpression, Name: shape.valueColumn, TableAlias: "", ColumnName: "", IsStar: false},
	)
	return nil
}

// pivotKeyword returns the keyword of a pivot clause.
//
// Takes unpivot (bool) which is true for an UNPIVOT.
//
// Returns string which is UNPIVOT or PIVOT.
func pivotKeyword(unpivot bool) string {
	if unpivot {
		return keywordUNPIVOT
	}
	return keywordPIVOT
}
