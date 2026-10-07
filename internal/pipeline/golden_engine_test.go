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

package pipeline_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

type mockEngineConfig struct {
	BuiltinFunctions map[string][]mockFunctionSignature `json:"builtinFunctions"`
	BuiltinTypes     map[string]mockSQLType             `json:"builtinTypes"`
	Statements       []mockStatement                    `json:"statements"`
}

type mockFunctionSignature struct {
	Arguments         []mockFunctionArgument `json:"arguments"`
	ReturnType        mockSQLType            `json:"returnType"`
	IsAggregate       bool                   `json:"isAggregate"`
	ReturnsSet        bool                   `json:"returnsSet"`
	NullableBehaviour string                 `json:"nullableBehaviour"`
	Schema            string                 `json:"schema"`
}

type mockFunctionArgument struct {
	Name string      `json:"name"`
	Type mockSQLType `json:"type"`
}

type mockSQLType struct {
	Category   string       `json:"category"`
	EngineName string       `json:"engineName"`
	Schema     string       `json:"schema"`
	Precision  *int         `json:"precision,omitempty"`
	Scale      *int         `json:"scale,omitempty"`
	Length     *int         `json:"length,omitempty"`
	EnumValues []string     `json:"enumValues,omitempty"`
	Element    *mockSQLType `json:"element,omitempty"`
}

type mockStatement struct {
	SQL         string           `json:"sql"`
	Mutation    *json.RawMessage `json:"mutation,omitempty"`
	RawAnalysis *json.RawMessage `json:"rawAnalysis,omitempty"`
}

type mockMutation struct {
	Kind              string                 `json:"kind"`
	SchemaName        string                 `json:"schemaName"`
	TableName         string                 `json:"tableName"`
	Columns           []mockColumn           `json:"columns,omitempty"`
	ColumnName        string                 `json:"columnName,omitempty"`
	EnumName          string                 `json:"enumName,omitempty"`
	EnumValues        []string               `json:"enumValues,omitempty"`
	FunctionSignature *mockFunctionSignature `json:"functionSignature,omitempty"`
	NewName           string                 `json:"newName,omitempty"`
	PrimaryKey        []string               `json:"primaryKey,omitempty"`
}

type mockColumn struct {
	Name            string      `json:"name"`
	SQLType         mockSQLType `json:"sqlType"`
	Nullable        bool        `json:"nullable"`
	HasDefault      bool        `json:"hasDefault"`
	IsGenerated     bool        `json:"isGenerated"`
	IsArray         bool        `json:"isArray"`
	ArrayDimensions int         `json:"arrayDimensions"`
	Comment         string      `json:"comment"`
}

type mockRawAnalysis struct {
	OutputColumns       []mockRawOutputColumn       `json:"outputColumns"`
	ParameterReferences []mockRawParameterReference `json:"parameterReferences"`
	FromTables          []mockTableReference        `json:"fromTables"`
	JoinClauses         []mockJoinClause            `json:"joinClauses"`
	CTEDefinitions      []mockCTEDefinition         `json:"cteDefinitions"`
	HasReturning        bool                        `json:"hasReturning"`
	GroupByColumns      []mockColumnReference       `json:"groupByColumns"`
}

type mockRawOutputColumn struct {
	Name       string           `json:"name"`
	TableAlias string           `json:"tableAlias"`
	ColumnName string           `json:"columnName"`
	IsStar     bool             `json:"isStar"`
	Expression *json.RawMessage `json:"expression,omitempty"`
}

type mockRawParameterReference struct {
	Number          int                  `json:"number"`
	Context         string               `json:"context"`
	ColumnReference *mockColumnReference `json:"columnReference,omitempty"`
	CastType        *mockSQLType         `json:"castType,omitempty"`
}

type mockColumnReference struct {
	TableAlias string `json:"tableAlias"`
	ColumnName string `json:"columnName"`
}

type mockTableReference struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Alias  string `json:"alias"`
}

type mockJoinClause struct {
	Kind  string             `json:"kind"`
	Table mockTableReference `json:"table"`
}

type mockCTEDefinition struct {
	Name          string                `json:"name"`
	OutputColumns []mockRawOutputColumn `json:"outputColumns"`
	FromTables    []mockTableReference  `json:"fromTables"`
	IsRecursive   bool                  `json:"isRecursive"`
}

type mockStatementIndex int

func (mockStatementIndex) IsParsedStatement() {}

type mockEngine struct {
	config            mockEngineConfig
	recordedCatalogue *sqlmodel.Catalogue
}

func newMockEngine(config mockEngineConfig) *mockEngine {
	return &mockEngine{config: config}
}

func (engine *mockEngine) ParseStatements(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
	normalised := normaliseSQLForMatching(sql)
	var results []ast.ParsedStatement

	for index, statement := range engine.config.Statements {
		statementNormalised := normaliseSQLForMatching(statement.SQL)
		if strings.Contains(normalised, statementNormalised) {
			results = append(results, ast.ParsedStatement{
				Raw:      mockStatementIndex(index),
				Location: 0,
				Length:   len(sql),
			})
		}
	}

	return results, nil
}

func (engine *mockEngine) ApplyDDL(_ context.Context, statement ast.ParsedStatement) (*ast.CatalogueMutation, error) {
	rawIndex, ok := statement.Raw.(mockStatementIndex)
	if !ok {
		return nil, fmt.Errorf("mock engine: unexpected statement raw type %T", statement.Raw)
	}
	index := int(rawIndex)

	if index < 0 || index >= len(engine.config.Statements) {
		return nil, fmt.Errorf("mock engine: statement index %d out of range", index)
	}

	entry := engine.config.Statements[index]
	if entry.Mutation == nil {
		return nil, nil
	}

	var mutation mockMutation
	if err := json.Unmarshal(*entry.Mutation, &mutation); err != nil {
		return nil, fmt.Errorf("mock engine: unmarshalling mutation: %w", err)
	}

	return convertMutation(mutation), nil
}

func (engine *mockEngine) AnalyseQuery(
	_ context.Context,
	catalogue *sqlmodel.Catalogue,
	statement ast.ParsedStatement,
) (*ast.RawQueryAnalysis, error) {
	engine.recordedCatalogue = catalogue

	rawIndex, ok := statement.Raw.(mockStatementIndex)
	if !ok {
		return nil, fmt.Errorf("mock engine: unexpected statement raw type %T", statement.Raw)
	}
	index := int(rawIndex)

	if index < 0 || index >= len(engine.config.Statements) {
		return nil, fmt.Errorf("mock engine: statement index %d out of range", index)
	}

	entry := engine.config.Statements[index]
	if entry.RawAnalysis == nil {
		return &ast.RawQueryAnalysis{}, nil
	}

	var analysis mockRawAnalysis
	if err := json.Unmarshal(*entry.RawAnalysis, &analysis); err != nil {
		return nil, fmt.Errorf("mock engine: unmarshalling raw analysis: %w", err)
	}

	return convertRawAnalysis(analysis), nil
}

func (engine *mockEngine) BuiltinFunctions() *ast.FunctionCatalogue {
	catalogue := &ast.FunctionCatalogue{
		Functions: make(map[string][]*sqlmodel.Function),
	}

	for name, signatures := range engine.config.BuiltinFunctions {
		for _, signature := range signatures {
			catalogue.Functions[name] = append(catalogue.Functions[name], convertFunctionSignature(name, signature))
		}
	}

	return catalogue
}

func (engine *mockEngine) BuiltinTypes() *ast.TypeCatalogue {
	catalogue := &ast.TypeCatalogue{
		Types: make(map[string]sqlmodel.SQLType),
	}

	for name, sqlType := range engine.config.BuiltinTypes {
		catalogue.Types[name] = convertSQLType(sqlType)
	}

	return catalogue
}

func (engine *mockEngine) NormaliseTypeName(name string, _ ...int) sqlmodel.SQLType {
	lowered := strings.ToLower(name)
	if sqlType, exists := engine.config.BuiltinTypes[lowered]; exists {
		return convertSQLType(sqlType)
	}
	return sqlmodel.SQLType{
		Category:   sqlmodel.TypeCategoryUnknown,
		EngineName: name,
	}
}

func (*mockEngine) Info() dialect.Info {
	return dialect.Info{
		Comments:              ast.DefaultSQLCommentStyle(),
		Name:                  "mock",
		Family:                "mock",
		SchemaSearchPath:      []string{"public"},
		Placeholders:          dialect.Placeholders{Sigils: []dialect.PlaceholderSigil{dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}}, Execution: dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}, Positional: dialect.PlaceholderSigil{}, NamePrefix: ""},
		Expressions:           ^ast.ExpressionFeature(0),
		Identifiers:           sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
		Returning:             ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete,
		AsyncMutations:        false,
		ScalarFunctionsInFrom: false,
		IdentifierQuote:       dialect.IdentifierQuote{Open: `"`, Close: `"`},
		Drivers:               nil,
		ToJSON:                "",
		TextType:              "",
		Filters:               dialect.FilterSyntax{Operators: nil, JSONPathFunctions: nil},
		MaxParameters:         0,
	}
}

func (engine *mockEngine) CoreTypes() dialect.CoreTypes {
	return dialect.CoreTypes{
		Boolean:  sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "boolean"),
		Text:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		Count:    engine.NormaliseTypeName("int8"),
		JSON:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"),
		RowCount: engine.NormaliseTypeName("int4"),
		RowID:    sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
	}
}

func (*mockEngine) BinaryResult(string, sqlmodel.SQLType, sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
}

func (*mockEngine) UnaryResult(operator string, _ sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	if strings.EqualFold(operator, "NOT") {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "boolean"), sqlmodel.FunctionNullableNeverNull, true
	}
	return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
}

func (*mockEngine) PromoteType(left sqlmodel.SQLType, right sqlmodel.SQLType) sqlmodel.SQLType {
	numericRank := map[string]int{"int2": 1, "int4": 2, "int8": 3, "numeric": 4, "float4": 5, "float8": 6}
	textRank := map[string]int{"char": 1, "varchar": 2, "text": 3}
	temporalRank := map[string]int{"date": 1, "time": 2, "timestamp": 3, "timestamptz": 4}

	switch left.Category {
	case sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal:
		leftRank := numericRank[strings.ToLower(left.EngineName)]
		rightRank := numericRank[strings.ToLower(right.EngineName)]
		if rightRank > leftRank {
			return right
		}
		return left

	case sqlmodel.TypeCategoryText:
		leftRank := textRank[strings.ToLower(left.EngineName)]
		rightRank := textRank[strings.ToLower(right.EngineName)]
		if rightRank > leftRank {
			return right
		}
		return left

	case sqlmodel.TypeCategoryTemporal:
		leftRank := temporalRank[strings.ToLower(left.EngineName)]
		rightRank := temporalRank[strings.ToLower(right.EngineName)]
		if rightRank > leftRank {
			return right
		}
		return left
	}

	return left
}

func (*mockEngine) CanImplicitCast(from sqlmodel.SQLType, to sqlmodel.SQLType) bool {
	switch {
	case from.Category == sqlmodel.TypeCategoryInteger && to.Category == sqlmodel.TypeCategoryDecimal:
		return true
	case from.Category == sqlmodel.TypeCategoryInteger && to.Category == sqlmodel.TypeCategoryFloat:
		return true
	case from.Category == sqlmodel.TypeCategoryDecimal && to.Category == sqlmodel.TypeCategoryFloat:
		return true
	case from.Category == sqlmodel.TypeCategoryText && to.Category == sqlmodel.TypeCategoryText:
		return true
	default:
		return false
	}
}

func (*mockEngine) TableValuedFunctionColumns(_ string) []ast.ScopedColumn {
	return nil
}

func normaliseSQLForMatching(sql string) string {
	lines := strings.Split(sql, "\n")
	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	joined := strings.Join(filtered, " ")

	var builder strings.Builder
	previousSpace := false
	for _, character := range joined {
		if character == ' ' || character == '\t' || character == '\n' || character == '\r' {
			if !previousSpace {
				builder.WriteByte(' ')
				previousSpace = true
			}
		} else {
			builder.WriteRune(character)
			previousSpace = false
		}
	}
	return strings.TrimSpace(builder.String())
}

func convertSQLType(source mockSQLType) sqlmodel.SQLType {
	result := sqlmodel.SQLType{
		Category:   parseSQLTypeCategory(source.Category),
		EngineName: source.EngineName,
		Schema:     source.Schema,
		Precision:  source.Precision,
		Scale:      source.Scale,
		Length:     source.Length,
		EnumValues: source.EnumValues,
	}
	if source.Element != nil {
		result.ElementType = new(convertSQLType(*source.Element))
	}
	applyMockAttributes(&result)
	return result
}

func applyMockAttributes(sqlType *sqlmodel.SQLType) {
	widths := map[string]int{"int2": 16, "int4": 32, "int8": 64, "float4": 32, "float8": 64}
	kinds := map[string]sqlmodel.TemporalKind{
		"date": sqlmodel.TemporalKindDate, "time": sqlmodel.TemporalKindTime,
		"timestamp": sqlmodel.TemporalKindTimestamp, "timestamptz": sqlmodel.TemporalKindTimestamp,
		"interval": sqlmodel.TemporalKindInterval,
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat:
		sqlType.Width = widths[sqlType.EngineName]
	case sqlmodel.TypeCategoryTemporal:
		sqlType.Temporal = kinds[sqlType.EngineName]
		sqlType.TimeZone = sqlType.EngineName == "timestamptz"
	default:
	}
}

func convertColumn(source mockColumn) sqlmodel.Column {
	return sqlmodel.Column{
		Name:            source.Name,
		SQLType:         convertSQLType(source.SQLType),
		Nullable:        source.Nullable,
		HasDefault:      source.HasDefault,
		IsGenerated:     source.IsGenerated,
		IsArray:         source.IsArray,
		ArrayDimensions: source.ArrayDimensions,
		Comment:         source.Comment,
	}
}

func convertMutation(source mockMutation) *ast.CatalogueMutation {
	mutation := &ast.CatalogueMutation{
		Kind:       parseMutationKind(source.Kind),
		SchemaName: source.SchemaName,
		TableName:  source.TableName,
		ColumnName: source.ColumnName,
		EnumName:   source.EnumName,
		EnumValues: source.EnumValues,
		NewName:    source.NewName,
		PrimaryKey: source.PrimaryKey,
	}

	for _, column := range source.Columns {
		mutation.Columns = append(mutation.Columns, convertColumn(column))
	}

	if source.FunctionSignature != nil {
		mutation.Function = ast.NewFunctionDefinition(convertFunctionSignature(source.TableName, *source.FunctionSignature))
	}

	return mutation
}

func convertFunctionSignature(name string, source mockFunctionSignature) *sqlmodel.Function {
	signature := &sqlmodel.Function{
		Name:              name,
		Schema:            source.Schema,
		ReturnType:        convertSQLType(source.ReturnType),
		ReturnsSet:        source.ReturnsSet,
		IsAggregate:       source.IsAggregate,
		NullableBehaviour: parseNullableBehaviour(source.NullableBehaviour),
	}

	for _, argument := range source.Arguments {
		signature.Arguments = append(signature.Arguments, sqlmodel.FunctionArgument{
			Name: argument.Name,
			Type: convertSQLType(argument.Type),
		})
	}

	return signature
}

func convertRawAnalysis(source mockRawAnalysis) *ast.RawQueryAnalysis {
	analysis := &ast.RawQueryAnalysis{
		HasReturning: source.HasReturning,
	}

	for _, column := range source.OutputColumns {
		rawColumn := ast.RawOutputColumn{
			Name:       column.Name,
			TableAlias: column.TableAlias,
			ColumnName: column.ColumnName,
			IsStar:     column.IsStar,
		}
		if column.Expression != nil {
			var raw any
			if err := json.Unmarshal(*column.Expression, &raw); err == nil {
				rawColumn.Expression = deserialiseExpression(raw)
			}
		}
		analysis.OutputColumns = append(analysis.OutputColumns, rawColumn)
	}

	for _, parameter := range source.ParameterReferences {
		reference := ast.RawParameterReference{
			Number:  parameter.Number,
			Context: parseParameterContext(parameter.Context),
		}
		if parameter.ColumnReference != nil {
			reference.ColumnReference = &ast.ColumnReference{
				TableAlias: parameter.ColumnReference.TableAlias,
				ColumnName: parameter.ColumnReference.ColumnName,
			}
		}
		if parameter.CastType != nil {
			reference.CastType = new(convertSQLType(*parameter.CastType))
		}
		analysis.ParameterReferences = append(analysis.ParameterReferences, reference)
	}

	for _, table := range source.FromTables {
		analysis.FromTables = append(analysis.FromTables, ast.TableReference{
			Schema: table.Schema,
			Name:   table.Name,
			Alias:  table.Alias,
		})
	}

	for _, join := range source.JoinClauses {
		analysis.JoinClauses = append(analysis.JoinClauses, ast.JoinClause{
			Kind: parseJoinKind(join.Kind),
			Table: ast.TableReference{
				Schema: join.Table.Schema,
				Name:   join.Table.Name,
				Alias:  join.Table.Alias,
			},
		})
	}

	for _, cte := range source.CTEDefinitions {
		definition := ast.RawCTEDefinition{
			Name:        cte.Name,
			IsRecursive: cte.IsRecursive,
		}
		for _, column := range cte.OutputColumns {
			rawColumn := ast.RawOutputColumn{
				Name:       column.Name,
				TableAlias: column.TableAlias,
				ColumnName: column.ColumnName,
				IsStar:     column.IsStar,
			}
			if column.Expression != nil {
				var raw any
				if err := json.Unmarshal(*column.Expression, &raw); err == nil {
					rawColumn.Expression = deserialiseExpression(raw)
				}
			}
			definition.OutputColumns = append(definition.OutputColumns, rawColumn)
		}
		for _, table := range cte.FromTables {
			definition.FromTables = append(definition.FromTables, ast.TableReference{
				Schema: table.Schema,
				Name:   table.Name,
				Alias:  table.Alias,
			})
		}
		analysis.CTEDefinitions = append(analysis.CTEDefinitions, definition)
	}

	for _, groupByColumn := range source.GroupByColumns {
		analysis.GroupByColumns = append(analysis.GroupByColumns, ast.ColumnReference{
			TableAlias: groupByColumn.TableAlias,
			ColumnName: groupByColumn.ColumnName,
		})
	}

	return analysis
}

func stringField(fields map[string]any, key string) string {
	if value, ok := fields[key].(string); ok {
		return value
	}
	return ""
}

func boolField(fields map[string]any, key string) bool {
	if value, ok := fields[key].(bool); ok {
		return value
	}
	return false
}

func deserialiseExpression(raw any) ast.Expression {
	if raw == nil {
		return nil
	}

	expressionMap, ok := raw.(map[string]any)
	if !ok {
		return &ast.UnknownExpression{}
	}

	kind := stringField(expressionMap, "kind")
	switch kind {
	case "column_ref":
		tableAlias := stringField(expressionMap, "tableAlias")
		columnName := stringField(expressionMap, "columnName")
		return &ast.ColumnRefExpression{TableAlias: tableAlias, ColumnName: columnName}

	case "function_call":
		functionName := stringField(expressionMap, "functionName")
		schema := stringField(expressionMap, "schema")
		return &ast.FunctionCallExpression{
			FunctionName: functionName,
			Schema:       schema,
			Arguments:    deserialiseExpressionList(expressionMap["arguments"]),
		}

	case "coalesce":
		return &ast.CoalesceExpression{
			Arguments: deserialiseExpressionList(expressionMap["arguments"]),
		}

	case "cast":
		typeName := stringField(expressionMap, "typeName")
		return &ast.CastExpression{
			TypeName: typeName,
			Inner:    deserialiseExpression(expressionMap["expression"]),
		}

	case "literal":
		typeName := stringField(expressionMap, "typeName")
		return &ast.LiteralExpression{TypeName: typeName}

	case "binary_op":
		operator := stringField(expressionMap, "operator")
		return &ast.BinaryOpExpression{
			Operator: operator,
			Left:     deserialiseExpression(expressionMap["left"]),
			Right:    deserialiseExpression(expressionMap["right"]),
		}

	case "comparison":
		operator := stringField(expressionMap, "operator")
		return &ast.ComparisonExpression{
			Operator: operator,
			Left:     deserialiseExpression(expressionMap["left"]),
			Right:    deserialiseExpression(expressionMap["right"]),
		}

	case "is_null":
		negated := boolField(expressionMap, "negated")
		return &ast.IsNullExpression{
			Inner:   deserialiseExpression(expressionMap["expression"]),
			Negated: negated,
		}

	case "in_list":
		return &ast.InListExpression{
			Inner:  deserialiseExpression(expressionMap["expression"]),
			Values: deserialiseExpressionList(expressionMap["values"]),
		}

	case "between":
		return &ast.BetweenExpression{
			Inner: deserialiseExpression(expressionMap["expression"]),
			Low:   deserialiseExpression(expressionMap["low"]),
			High:  deserialiseExpression(expressionMap["high"]),
		}

	case "logical_op":
		operator := stringField(expressionMap, "operator")
		return &ast.LogicalOpExpression{
			Operator: operator,
			Operands: deserialiseExpressionList(expressionMap["operands"]),
		}

	case "unary_op":
		operator := stringField(expressionMap, "operator")
		return &ast.UnaryOpExpression{
			Operator: operator,
			Operand:  deserialiseExpression(expressionMap["operand"]),
		}

	case "case_when":
		expression := &ast.CaseWhenExpression{}
		if rawBranches, ok := expressionMap["branches"].([]any); ok {
			for _, rawBranch := range rawBranches {
				if branch, ok := rawBranch.(map[string]any); ok {
					expression.Branches = append(expression.Branches, ast.CaseWhenBranch{
						Condition: deserialiseExpression(branch["condition"]),
						Result:    deserialiseExpression(branch["result"]),
					})
				}
			}
		}
		if elseResult, hasElse := expressionMap["elseResult"]; hasElse && elseResult != nil {
			expression.ElseResult = deserialiseExpression(elseResult)
		}
		return expression

	case "exists":
		return &ast.ExistsExpression{}

	case "window_function":
		innerFunction := deserialiseExpression(expressionMap["function"])
		if functionCall, ok := innerFunction.(*ast.FunctionCallExpression); ok {
			return &ast.WindowFunctionExpression{Function: functionCall}
		}
		return &ast.UnknownExpression{}

	case "array_subscript":
		return &ast.ArraySubscriptExpression{
			Array: deserialiseExpression(expressionMap["array"]),
			Index: deserialiseExpression(expressionMap["index"]),
		}

	default:
		return &ast.UnknownExpression{}
	}
}

func deserialiseExpressionList(raw any) []ast.Expression {
	rawList, ok := raw.([]any)
	if !ok {
		return nil
	}

	result := make([]ast.Expression, len(rawList))
	for index, item := range rawList {
		result[index] = deserialiseExpression(item)
	}
	return result
}

func parseSQLTypeCategory(category string) sqlmodel.SQLTypeCategory {
	switch strings.ToLower(category) {
	case "integer":
		return sqlmodel.TypeCategoryInteger
	case "float":
		return sqlmodel.TypeCategoryFloat
	case "decimal":
		return sqlmodel.TypeCategoryDecimal
	case "boolean":
		return sqlmodel.TypeCategoryBoolean
	case "text":
		return sqlmodel.TypeCategoryText
	case "binary":
		return sqlmodel.TypeCategoryBinary
	case "temporal":
		return sqlmodel.TypeCategoryTemporal
	case "json":
		return sqlmodel.TypeCategoryJSON
	case "uuid":
		return sqlmodel.TypeCategoryUUID
	case "network":
		return sqlmodel.TypeCategoryNetwork
	case "geometric":
		return sqlmodel.TypeCategoryGeometric
	case "enum":
		return sqlmodel.TypeCategoryEnum
	case "composite":
		return sqlmodel.TypeCategoryComposite
	case "array":
		return sqlmodel.TypeCategoryArray
	case "range":
		return sqlmodel.TypeCategoryRange
	default:
		return sqlmodel.TypeCategoryUnknown
	}
}

func parseMutationKind(kind string) ast.MutationKind {
	switch strings.ToLower(kind) {
	case "droptable":
		return ast.MutationDropTable
	case "altertableaddcolumn":
		return ast.MutationAlterTableAddColumn
	case "altertabledropcolumn":
		return ast.MutationAlterTableDropColumn
	case "altertablealtercolumn":
		return ast.MutationAlterTableAlterColumn
	case "altertablerenamecolumn":
		return ast.MutationAlterTableRenameColumn
	case "altertablerenametable":
		return ast.MutationAlterTableRenameTable
	case "altertablesetschema":
		return ast.MutationAlterTableSetSchema
	case "createenum":
		return ast.MutationCreateEnum
	case "alterenumaddvalue":
		return ast.MutationAlterEnumAddValue
	case "alterenumrenamevalue":
		return ast.MutationAlterEnumRenameValue
	case "dropenum":
		return ast.MutationDropEnum
	case "createcompositetype":
		return ast.MutationCreateCompositeType
	case "droptype":
		return ast.MutationDropType
	case "createfunction":
		return ast.MutationCreateFunction
	case "dropfunction":
		return ast.MutationDropFunction
	case "createschema":
		return ast.MutationCreateSchema
	case "dropschema":
		return ast.MutationDropSchema
	case "createview":
		return ast.MutationCreateView
	case "dropview":
		return ast.MutationDropView
	case "createindex":
		return ast.MutationCreateIndex
	case "dropindex":
		return ast.MutationDropIndex
	case "createextension":
		return ast.MutationCreateExtension
	case "comment":
		return ast.MutationComment
	default:
		return ast.MutationCreateTable
	}
}

func parseJoinKind(kind string) ast.JoinKind {
	switch strings.ToLower(kind) {
	case "left":
		return ast.JoinLeft
	case "right":
		return ast.JoinRight
	case "full":
		return ast.JoinFull
	case "cross":
		return ast.JoinCross
	default:
		return ast.JoinInner
	}
}

func parseParameterContext(parameterContext string) sqlmodel.ParameterContext {
	switch strings.ToLower(parameterContext) {
	case "comparison":
		return sqlmodel.ParameterContextComparison
	case "assignment":
		return sqlmodel.ParameterContextAssignment
	case "functionargument":
		return sqlmodel.ParameterContextFunctionArgument
	case "cast":
		return sqlmodel.ParameterContextCast
	case "inlist":
		return sqlmodel.ParameterContextInList
	case "limit":
		return sqlmodel.ParameterContextLimit
	case "offset":
		return sqlmodel.ParameterContextOffset
	case "like":
		return sqlmodel.ParameterContextLike
	default:
		return sqlmodel.ParameterContextUnknown
	}
}

func parseNullableBehaviour(behaviour string) sqlmodel.FunctionNullableBehaviour {
	switch strings.ToLower(behaviour) {
	case "returnsnullonnull":
		return sqlmodel.FunctionNullableReturnsNullOnNull
	case "nevernull":
		return sqlmodel.FunctionNullableNeverNull
	default:
		return sqlmodel.FunctionNullableCalledOnNull
	}
}
