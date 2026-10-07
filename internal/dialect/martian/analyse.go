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

package martian

import (
	"cmp"
	"context"
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// spanTokenKind classifies a Martian token for the shared placeholder locator.
type spanTokenKind uint8

const (
	// spanOther is any token the locator does not need to tell apart.
	spanOther spanTokenKind = iota

	// spanWord is an unquoted identifier or keyword.
	spanWord

	// spanParameter is a placeholder.
	spanParameter

	// spanOpen is "(".
	spanOpen

	// spanClose is ")".
	spanClose
)

var (
	// placeholderTokenKinds tells the shared placeholder locator how spanTokens classifies
	// Martian's tokens.
	placeholderTokenKinds = dialectkit.PlaceholderTokens[spanTokenKind]{
		IsParameter:      func(candidate dialectkit.Token[spanTokenKind]) bool { return candidate.Kind == spanParameter },
		Word:             spanWord,
		LeftParenthesis:  spanOpen,
		RightParenthesis: spanClose,
	}
)

// analysisBuilder accumulates the raw analysis of one DML statement.
type analysisBuilder struct {
	// analysis is the analysis being built.
	analysis *ast.RawQueryAnalysis
}

// newAnalysisBuilder starts the analysis of a statement over one table.
//
// Takes reference (tableReference) which is the table the statement reads or writes.
//
// Returns *analysisBuilder which holds an analysis with that table in FROM.
func newAnalysisBuilder(reference tableReference) *analysisBuilder {
	analysis := &ast.RawQueryAnalysis{}
	builder := &analysisBuilder{analysis: analysis}
	if reference.call != nil {
		builder.addFromCall(reference)
		return builder
	}
	analysis.FromTables = []ast.TableReference{{
		Schema: reference.table.schema,
		Name:   reference.table.name,
		Alias:  reference.alias,
	}}
	return builder
}

// addFromCall records a function call a SELECT reads from as a table-valued function of
// the analysis, named after its alias or, without one, after the function, and records
// the placeholders among its arguments.
//
// Takes reference (tableReference) which holds the call and its alias.
func (b *analysisBuilder) addFromCall(reference tableReference) {
	arguments := make([]ast.Expression, 0, len(reference.call.arguments))
	for _, argument := range reference.call.arguments {
		arguments = append(arguments, modelExpression(argument))
	}
	b.analysis.RawTableValuedFunctions = []ast.RawTableValuedFunctionReference{{
		FunctionName:      reference.call.name,
		Arguments:         arguments,
		Alias:             cmp.Or(reference.alias, reference.call.name),
		ColumnDefinitions: nil,
		Ordinality:        nil,
		Functions:         nil,
		JoinKind:          ast.JoinInner,
	}}
	b.collectParameters(reference.call, nil, sqlmodel.ParameterContextFunctionArgument)
}

// addWhere records a WHERE condition and the parameters inside it.
//
// Takes where (expression) which is the condition, or nil.
func (b *analysisBuilder) addWhere(where expression) {
	if where == nil {
		return
	}
	b.analysis.HasWhereClause = true
	b.collectParameters(where, nil, sqlmodel.ParameterContextUnknown)
	b.addClauseExpression(ast.ClauseWhere, where)
}

// addClauseExpression records an expression a clause holds, in the shared expression
// model, so the analyser can check the names it uses.
//
// Takes clause (ast.Clause) which is the clause.
// Takes node (expression) which is the expression.
func (b *analysisBuilder) addClauseExpression(clause ast.Clause, node expression) {
	b.analysis.ClauseExpressions = append(b.analysis.ClauseExpressions,
		ast.ClauseExpression{Expression: modelExpression(node), Clause: clause})
}

// collectParameters records every placeholder in an expression, linking a placeholder
// compared with a column to that column.
//
// Takes node (expression) which is the expression to walk, or nil.
// Takes target (*ast.ColumnReference) which is the column a bare placeholder is assigned
// to, or nil.
// Takes parameterContext (sqlmodel.ParameterContext) which is where a bare placeholder
// appears.
func (b *analysisBuilder) collectParameters(node expression, target *ast.ColumnReference, parameterContext sqlmodel.ParameterContext) {
	switch typed := node.(type) {
	case *parameterExpression:
		b.addParameter(typed.number, target, parameterContext, nil)
	case *convertExpression:
		b.collectConvertedParameters(typed, target, parameterContext)
	case *binaryExpression:
		b.collectBinaryParameters(typed)
	case *functionExpression:
		for _, argument := range typed.arguments {
			b.collectParameters(argument, nil, sqlmodel.ParameterContextFunctionArgument)
		}
	case *nullTestExpression:
		b.collectParameters(typed.operand, nil, sqlmodel.ParameterContextUnknown)
	case *inListExpression:
		b.collectParameters(typed.operand, nil, sqlmodel.ParameterContextUnknown)
		for _, member := range typed.values {
			b.collectParameters(member, columnReferenceOf(typed.operand), sqlmodel.ParameterContextInList)
		}
	}
}

// collectConvertedParameters records the placeholders of a CONVERT: a placeholder it
// converts alone keeps the place the CONVERT has and declares the CONVERT's type, and any
// other placeholder inside it is recorded with no column.
//
// Takes node (*convertExpression) which is the conversion.
// Takes target (*ast.ColumnReference) which is the column the conversion is assigned to
// or compared with, or nil.
// Takes parameterContext (sqlmodel.ParameterContext) which is where the conversion
// appears.
func (b *analysisBuilder) collectConvertedParameters(node *convertExpression, target *ast.ColumnReference,
	parameterContext sqlmodel.ParameterContext) {
	parameter, isParameter := node.operand.(*parameterExpression)
	if !isParameter {
		b.collectParameters(node.operand, nil, sqlmodel.ParameterContextUnknown)
		return
	}
	declared := normaliseTypeName(node.typeName, node.modifiers)
	b.addParameter(parameter.number, target, parameterContext, &declared)
}

// collectBinaryParameters records the placeholders of a binary expression, linking a
// placeholder compared with a column to that column.
//
// Takes node (*binaryExpression) which is the expression to walk.
func (b *analysisBuilder) collectBinaryParameters(node *binaryExpression) {
	if !slices.Contains(comparisonOperators, node.operator) {
		b.collectParameters(node.left, nil, sqlmodel.ParameterContextUnknown)
		b.collectParameters(node.right, nil, sqlmodel.ParameterContextUnknown)
		return
	}
	b.collectParameters(node.left, columnReferenceOf(node.right), sqlmodel.ParameterContextComparison)
	b.collectParameters(node.right, columnReferenceOf(node.left), sqlmodel.ParameterContextComparison)
}

// addParameter appends one parameter reference.
//
// Takes number (int) which is the placeholder's N.
// Takes target (*ast.ColumnReference) which is the linked column, or nil.
// Takes parameterContext (sqlmodel.ParameterContext) which is where the placeholder
// appears.
// Takes declared (*sqlmodel.SQLType) which is the type a CONVERT of the placeholder alone
// declares, or nil.
func (b *analysisBuilder) addParameter(number int, target *ast.ColumnReference, parameterContext sqlmodel.ParameterContext,
	declared *sqlmodel.SQLType) {
	b.analysis.ParameterReferences = append(b.analysis.ParameterReferences, ast.RawParameterReference{
		ColumnReference:       target,
		CastType:              declared,
		Name:                  "",
		EnclosingFunctionName: "",
		Number:                number,
		ArgumentOrdinal:       0,
		Context:               parameterContext,
	})
}

// createTableMutation turns CREATE TABLE into the catalogue mutation it applies.
//
// Takes created (*createTableStatement) which is the parsed statement.
//
// Returns *ast.CatalogueMutation which creates the table.
func createTableMutation(created *createTableStatement) *ast.CatalogueMutation {
	columns := make([]sqlmodel.Column, 0, len(created.columns))
	for index := range created.columns {
		definition := &created.columns[index]
		notNull := definition.notNull || slices.Contains(created.primaryKey, definition.name)
		column := sqlmodel.NewColumn(definition.name, normaliseTypeName(definition.typeName, definition.modifiers), !notNull)
		column.HasDefault = definition.hasDefault
		columns = append(columns, column)
	}
	return ast.NewCatalogueMutation(
		ast.MutationCreateTable,
		created.table.schema,
		created.table.name,
		ast.WithColumns(columns),
		ast.WithPrimaryKey(created.primaryKey),
	)
}

// analyseSelect builds the raw analysis of SELECT, stopping with the context's cause once
// the context is done.
//
// Takes selected (*selectStatement) which is the parsed statement.
//
// Returns *ast.RawQueryAnalysis which describes the query.
// Returns error which is the context's cause once the context is done.
func analyseSelect(ctx context.Context, selected *selectStatement) (*ast.RawQueryAnalysis, error) {
	builder := newAnalysisBuilder(selected.from)
	builder.analysis.ReadOnly = true
	for index := range selected.items {
		if cause := context.Cause(ctx); cause != nil {
			return nil, cause
		}
		builder.analysis.OutputColumns = append(builder.analysis.OutputColumns, outputColumn(&selected.items[index]))
		builder.collectParameters(selected.items[index].expression, nil, sqlmodel.ParameterContextUnknown)
	}
	builder.addWhere(selected.where)
	for index := range selected.orderBy {
		builder.collectParameters(selected.orderBy[index].expression, nil, sqlmodel.ParameterContextUnknown)
		builder.addClauseExpression(ast.ClauseOrderBy, selected.orderBy[index].expression)
	}
	return builder.analysis, nil
}

// analyseInsert builds the raw analysis of INSERT.
//
// Takes inserted (*insertStatement) which is the parsed statement.
//
// Returns *ast.RawQueryAnalysis which describes the query.
func analyseInsert(inserted *insertStatement) *ast.RawQueryAnalysis {
	builder := newAnalysisBuilder(inserted.into)
	builder.analysis.DataChange = ast.DataChangeInsert
	builder.analysis.InsertTable = inserted.into.table.name
	builder.analysis.InsertTarget = builder.analysis.FromTables[0]
	builder.analysis.InsertColumns = slices.Clone(inserted.columns)
	for index, value := range inserted.values {
		target := &ast.ColumnReference{TableAlias: inserted.into.table.name, ColumnName: inserted.columns[index]}
		builder.collectParameters(value, target, sqlmodel.ParameterContextAssignment)
		builder.addClauseExpression(ast.ClauseInsertValue, value)
	}
	return builder.analysis
}

// analyseUpdate builds the raw analysis of UPDATE.
//
// Takes updated (*updateStatement) which is the parsed statement.
//
// Returns *ast.RawQueryAnalysis which describes the query.
func analyseUpdate(updated *updateStatement) *ast.RawQueryAnalysis {
	builder := newAnalysisBuilder(updated.target)
	builder.analysis.DataChange = ast.DataChangeUpdate
	for index := range updated.assignments {
		entry := &updated.assignments[index]
		target := &ast.ColumnReference{TableAlias: updated.target.table.name, ColumnName: entry.column}
		builder.collectParameters(entry.value, target, sqlmodel.ParameterContextAssignment)
		builder.addClauseExpression(ast.ClauseSetTarget,
			&columnExpression{qualifier: "", name: entry.column, span: entry.columnSpan})
		builder.addClauseExpression(ast.ClauseSetValue, entry.value)
	}
	builder.addWhere(updated.where)
	return builder.analysis
}

// analyseDelete builds the raw analysis of DELETE.
//
// Takes deleted (*deleteStatement) which is the parsed statement.
//
// Returns *ast.RawQueryAnalysis which describes the query.
func analyseDelete(deleted *deleteStatement) *ast.RawQueryAnalysis {
	builder := newAnalysisBuilder(deleted.from)
	builder.analysis.DataChange = ast.DataChangeDelete
	builder.addWhere(deleted.where)
	return builder.analysis
}

// columnReferenceOf returns the column an expression references directly.
//
// Takes node (expression) which is the expression.
//
// Returns *ast.ColumnReference which is the column, or nil when the expression is not a
// column reference.
func columnReferenceOf(node expression) *ast.ColumnReference {
	column, isColumn := node.(*columnExpression)
	if !isColumn {
		return nil
	}
	return &ast.ColumnReference{TableAlias: column.qualifier, ColumnName: column.name}
}

// outputColumn builds the raw output column of one SELECT item.
//
// Takes item (*selectItem) which is the SELECT list item.
//
// Returns ast.RawOutputColumn which describes the column.
func outputColumn(item *selectItem) ast.RawOutputColumn {
	if item.star {
		return ast.RawOutputColumn{Expression: nil, Name: "", TableAlias: "", ColumnName: "", IsStar: true}
	}
	if column, isColumn := item.expression.(*columnExpression); isColumn {
		name := column.name
		if item.alias != "" {
			name = item.alias
		}
		return ast.RawOutputColumn{Expression: nil, Name: name, TableAlias: column.qualifier, ColumnName: column.name, IsStar: false}
	}
	return ast.RawOutputColumn{
		Expression: modelExpression(item.expression),
		Name:       item.alias,
		TableAlias: "",
		ColumnName: "",
		IsStar:     false,
	}
}

// modelExpression converts a Martian expression into the shared expression model, so the
// core can type it.
//
// Takes node (expression) which is the Martian expression.
//
// Returns ast.Expression which is the shared form.
func modelExpression(node expression) ast.Expression {
	switch typed := node.(type) {
	case *columnExpression:
		return &ast.ColumnRefExpression{TableAlias: typed.qualifier, ColumnName: typed.name, Span: typed.span}
	case *parameterExpression:
		return &ast.ParameterExpression{Number: typed.number}
	case *convertExpression:
		return &ast.CastExpression{Inner: modelExpression(typed.operand), TypeName: typed.typeName}
	case *literalExpression:
		return &ast.LiteralExpression{TypeName: typed.typeName}
	case *functionExpression:
		arguments := make([]ast.Expression, 0, len(typed.arguments))
		for _, argument := range typed.arguments {
			arguments = append(arguments, modelExpression(argument))
		}
		return &ast.FunctionCallExpression{
			FilterExpression: nil,
			FunctionName:     typed.name,
			Schema:           "",
			Arguments:        arguments,
			Span:             typed.nameSpan,
		}
	case *binaryExpression:
		return modelBinaryExpression(typed)
	case *nullTestExpression:
		return &ast.IsNullExpression{Inner: modelExpression(typed.operand), Negated: typed.negated}
	case *inListExpression:
		members := make([]ast.Expression, 0, len(typed.values))
		for _, member := range typed.values {
			members = append(members, modelExpression(member))
		}
		return &ast.InListExpression{Inner: modelExpression(typed.operand), Values: members}
	default:
		return &ast.UnknownExpression{}
	}
}

// modelBinaryExpression converts a Martian binary expression into the shared model:
// arithmetic, comparison or logical.
//
// Takes node (*binaryExpression) which is the Martian expression.
//
// Returns ast.Expression which is the shared form.
func modelBinaryExpression(node *binaryExpression) ast.Expression {
	left := modelExpression(node.left)
	right := modelExpression(node.right)
	switch {
	case slices.Contains(comparisonOperators, node.operator):
		return &ast.ComparisonExpression{Left: left, Right: right, Operator: node.operator, Quantifier: ""}
	case node.operator == "AND" || node.operator == "OR":
		return &ast.LogicalOpExpression{Operator: node.operator, Operands: []ast.Expression{left, right}}
	default:
		return &ast.BinaryOpExpression{Left: left, Right: right, Operator: node.operator, MayBeNull: false}
	}
}

// recordSpans records where the parts of an analysed statement are written, as offsets
// from its first byte.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the spans.
// Takes raw (*statement) which is the parsed statement.
// Takes parsed (ast.ParsedStatement) which locates it.
// Takes placeholders (dialect.Placeholders) which describes Martian's placeholder form.
func recordSpans(analysis *ast.RawQueryAnalysis, raw *statement, parsed ast.ParsedStatement, placeholders dialect.Placeholders) {
	recorder := dialectkit.NewSpanRecorder(parsed.Location)
	switch {
	case raw.selection != nil:
		recordSelectSpans(recorder, raw.selection)
		recorder.RecordOrderByPlace(orderByPlace(raw, raw.selection))
	case raw.insertion != nil:
		items := make([]ast.Span, 0, len(raw.insertion.valueSpans))
		for _, value := range raw.insertion.valueSpans {
			items = append(items, recorder.Span(value.Start, value.End))
		}
		recorder.AddValuesRow(ast.TupleSpan{Items: items, Span: recorder.Span(raw.insertion.row.Start, raw.insertion.row.End)})
	case raw.update != nil:
		recordWhereSpans(recorder, raw.update.whereClause)
	case raw.deletion != nil:
		recordWhereSpans(recorder, raw.deletion.whereClause)
	}
	spans := recorder.Finish(analysis)
	spans.Placeholders = dialectkit.PlaceholderSpans(spanTokens(raw), placeholderTokenKinds, placeholders, parsed.Location)
	spans.Terminator = dialectkit.TerminatorSpan(lexicalRules, raw.source, parsed.Location, parsed.Location+parsed.Length)
	analysis.Spans = spans
}

// recordSelectSpans records the clauses of a SELECT.
//
// Takes recorder (*dialectkit.SpanRecorder) which receives them.
// Takes selected (*selectStatement) which is the parsed SELECT.
func recordSelectSpans(recorder *dialectkit.SpanRecorder, selected *selectStatement) {
	recorder.Record(dialectkit.SpanProjection, selected.projection.Start, selected.projection.End)
	items := make([]ast.ItemSpan, 0, len(selected.items))
	for index := range selected.items {
		item := &selected.items[index]
		items = append(items, recorder.Item(item.start, item.expressionEnd, item.aliasStart, item.end, item.star))
	}
	recorder.RecordProjectionItems(items)
	recorder.Record(dialectkit.SpanFrom, selected.fromClause.Start, selected.fromClause.End)
	recordWhereSpans(recorder, selected.whereClause)
	recorder.Record(dialectkit.SpanOrderBy, selected.orderByClause.whole.Start, selected.orderByClause.whole.End)
}

// orderByPlace returns where a SELECT's ORDER BY starts, or would start: the end of the
// last token before its ORDER BY, or before the statement's terminator when it has none.
//
// Takes raw (*statement) which is the parsed statement.
// Takes selected (*selectStatement) which is its SELECT.
//
// Returns int which is the offset into the statement's source.
func orderByPlace(raw *statement, selected *selectStatement) int {
	limit := len(raw.source)
	if selected.orderByClause.whole.End > selected.orderByClause.whole.Start {
		limit = selected.orderByClause.whole.Start
	}
	place := 0
	for _, current := range raw.tokens {
		if current.end > limit || current.kind == tokenEnd || current.isSymbol(symbolTerminator) {
			break
		}
		place = current.end
	}
	return place
}

// recordWhereSpans records a WHERE clause and its condition.
//
// Takes recorder (*dialectkit.SpanRecorder) which receives them.
// Takes where (clause) which locates the clause, or is zero when there is none.
func recordWhereSpans(recorder *dialectkit.SpanRecorder, where clause) {
	recorder.Record(dialectkit.SpanWhere, where.whole.Start, where.whole.End)
	recorder.Record(dialectkit.SpanWhereCondition, where.body.Start, where.body.End)
}

// spanTokens turns a statement's tokens into the shared token form the placeholder
// locator reads.
//
// Takes raw (*statement) which is the parsed statement.
//
// Returns []dialectkit.Token[spanTokenKind] which holds the tokens in order.
func spanTokens(raw *statement) []dialectkit.Token[spanTokenKind] {
	tokens := make([]dialectkit.Token[spanTokenKind], 0, len(raw.tokens))
	for _, original := range raw.tokens {
		kind := spanOther
		switch {
		case original.kind == tokenIdentifier:
			kind = spanWord
		case original.kind == tokenPlaceholder:
			kind = spanParameter
		case original.isSymbol(symbolOpen):
			kind = spanOpen
		case original.isSymbol(symbolClose):
			kind = spanClose
		}
		tokens = append(tokens, dialectkit.Token[spanTokenKind]{
			Kind:   kind,
			Value:  original.value,
			Raw:    raw.source[original.offset:original.end],
			Start:  original.offset,
			End:    original.end,
			Quoted: original.kind == tokenQuotedIdentifier,
		})
	}
	return tokens
}
