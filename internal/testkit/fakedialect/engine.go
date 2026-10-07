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

package fakedialect

import (
	"context"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// allExpressionFeatures enables every expression feature, so the fake dialect exercises
	// every code path the type resolver has.
	allExpressionFeatures = ^ast.ExpressionFeature(0)

	// widthWord is the width in bits of the fake dialect's row-count integer.
	widthWord = 32

	// widthLong is the width in bits of the fake dialect's count and row identifier
	// integers.
	widthLong = 64
)

// Engine is a programmable dialect. Each method calls the function field named after it
// when that field is set, and otherwise returns the default its own documentation
// describes; Info assembles its fields from the function fields named after the metadata
// it reports.
//
// A panic in a function field is not recovered and reaches the caller, so the fake can
// stand in for a dialect that breaks the panic contract; a field that returns a
// *dialectkit.PanicError stands in for one that keeps it.
type Engine struct {
	// ParseStatementsFunc replaces ParseStatements.
	ParseStatementsFunc func(ctx context.Context, sql string) ([]ast.ParsedStatement, error)

	// ApplyDDLFunc replaces ApplyDDL.
	ApplyDDLFunc func(ctx context.Context, statement ast.ParsedStatement) (*ast.CatalogueMutation, error)

	// AnalyseQueryFunc replaces AnalyseQuery.
	AnalyseQueryFunc func(ctx context.Context, catalogue *sqlmodel.Catalogue, statement ast.ParsedStatement) (*ast.RawQueryAnalysis, error)

	// NormaliseTypeNameFunc replaces NormaliseTypeName.
	NormaliseTypeNameFunc func(name string, modifiers ...int) sqlmodel.SQLType

	// PromoteTypeFunc replaces PromoteType.
	PromoteTypeFunc func(left sqlmodel.SQLType, right sqlmodel.SQLType) sqlmodel.SQLType

	// CanImplicitCastFunc replaces CanImplicitCast.
	CanImplicitCastFunc func(from sqlmodel.SQLType, to sqlmodel.SQLType) bool

	// BinaryResultFunc replaces BinaryResult.
	BinaryResultFunc func(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool)

	// UnaryResultFunc replaces UnaryResult.
	UnaryResultFunc func(operator string, operand sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool)

	// CoreTypesFunc replaces CoreTypes.
	CoreTypesFunc func() dialect.CoreTypes

	// SupportedExpressionsFunc sets Info.Expressions.
	SupportedExpressionsFunc func() ast.ExpressionFeature

	// CommentStyleFunc sets Info.Comments.
	CommentStyleFunc func() ast.CommentStyle

	// LexicalFunc sets Info.Lexical.
	LexicalFunc func() sqllex.Rules

	// PlaceholdersFunc sets Info.Placeholders.
	PlaceholdersFunc func() dialect.Placeholders

	// SchemaSearchPathFunc sets Info.SchemaSearchPath.
	SchemaSearchPathFunc func() []string

	// BuiltinFunctionsFunc replaces BuiltinFunctions.
	BuiltinFunctionsFunc func() *ast.FunctionCatalogue

	// BuiltinTypesFunc replaces BuiltinTypes.
	BuiltinTypesFunc func() *ast.TypeCatalogue

	// TableValuedFunctionColumnsFunc replaces TableValuedFunctionColumns.
	TableValuedFunctionColumnsFunc func(functionName string) []ast.ScopedColumn

	// ReturningFunc sets Info.Returning.
	ReturningFunc func() ast.DataChange

	// DialectFunc sets the dialect name.
	DialectFunc func() string

	// SupportsAsyncMutationsFunc sets Info.AsyncMutations.
	SupportsAsyncMutationsFunc func() bool

	// InfoFunc replaces Info entirely.
	InfoFunc func() dialect.Info
}

// ParseStatements calls ParseStatementsFunc when it is set, and otherwise returns no
// statements.
//
// Takes sql (string) which is the statement text.
//
// Returns []ast.ParsedStatement which holds parsed statements, or nil without a callback.
// Returns error when the callback fails.
func (engine *Engine) ParseStatements(ctx context.Context, sql string) ([]ast.ParsedStatement, error) {
	if engine.ParseStatementsFunc != nil {
		return engine.ParseStatementsFunc(ctx, sql)
	}
	return nil, nil
}

// ApplyDDL calls ApplyDDLFunc when it is set, and otherwise returns no mutation.
//
// Takes statement (ast.ParsedStatement) which is the parsed SQL statement.
//
// Returns *ast.CatalogueMutation which is the schema mutation, or nil without a callback.
// Returns error when the callback fails.
func (engine *Engine) ApplyDDL(ctx context.Context, statement ast.ParsedStatement) (*ast.CatalogueMutation, error) {
	if engine.ApplyDDLFunc != nil {
		return engine.ApplyDDLFunc(ctx, statement)
	}
	return nil, nil
}

// AnalyseQuery calls AnalyseQueryFunc when it is set, and otherwise returns no analysis.
//
// Takes catalogue (*sqlmodel.Catalogue) which supplies the schema objects available to
// the query.
// Takes statement (ast.ParsedStatement) which is the parsed SQL statement.
//
// Returns *ast.RawQueryAnalysis which is the query analysis, or nil without a callback.
// Returns error when the callback fails.
func (engine *Engine) AnalyseQuery(ctx context.Context, catalogue *sqlmodel.Catalogue, statement ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
	if engine.AnalyseQueryFunc != nil {
		return engine.AnalyseQueryFunc(ctx, catalogue, statement)
	}
	return nil, nil
}

// NormaliseTypeName calls NormaliseTypeNameFunc when it is set, and otherwise returns an
// unknown-category type carrying the given name.
//
// Takes name (string) which is the name to resolve.
// Takes modifiers (...int) which holds the type width, precision or scale.
//
// Returns sqlmodel.SQLType which is the resolved type, or an unknown type carrying name.
func (engine *Engine) NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	if engine.NormaliseTypeNameFunc != nil {
		return engine.NormaliseTypeNameFunc(name, modifiers...)
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, name)
}

// PromoteType calls PromoteTypeFunc when it is set, and otherwise returns the left type.
//
// Takes left (sqlmodel.SQLType) which is the left operand type.
// Takes right (sqlmodel.SQLType) which is the right operand type.
//
// Returns sqlmodel.SQLType which is the promoted type, or left without a callback.
func (engine *Engine) PromoteType(left sqlmodel.SQLType, right sqlmodel.SQLType) sqlmodel.SQLType {
	if engine.PromoteTypeFunc != nil {
		return engine.PromoteTypeFunc(left, right)
	}
	return left
}

// CanImplicitCast calls CanImplicitCastFunc when it is set, and otherwise reports false.
//
// Takes from (sqlmodel.SQLType) which is the source SQL type.
// Takes to (sqlmodel.SQLType) which is the destination SQL type.
//
// Returns bool which is true when the callback permits the conversion.
func (engine *Engine) CanImplicitCast(from sqlmodel.SQLType, to sqlmodel.SQLType) bool {
	if engine.CanImplicitCastFunc != nil {
		return engine.CanImplicitCastFunc(from, to)
	}
	return false
}

// BinaryResult calls BinaryResultFunc when it is set, and otherwise leaves every operator
// to the structural default.
//
// Takes operator (string) which is the SQL operator.
// Takes left (sqlmodel.SQLType) which is the left operand type.
// Takes right (sqlmodel.SQLType) which is the right operand type.
//
// Returns sqlmodel.SQLType which is the callback's result type.
// Returns sqlmodel.FunctionNullableBehaviour which describes the result nullability.
// Returns bool which is true when the operator was handled.
func (engine *Engine) BinaryResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	if engine.BinaryResultFunc != nil {
		return engine.BinaryResultFunc(operator, left, right)
	}
	return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
}

// UnaryResult calls UnaryResultFunc when it is set, and otherwise types NOT as a boolean
// that is NULL when its operand is and leaves every other operator to the structural
// default.
//
// Takes operator (string) which is the SQL operator.
// Takes operand (sqlmodel.SQLType) which is the operand type.
//
// Returns sqlmodel.SQLType which is the callback or default result type.
// Returns sqlmodel.FunctionNullableBehaviour which describes the result nullability.
// Returns bool which is true when the operator was handled.
func (engine *Engine) UnaryResult(operator string, operand sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	if engine.UnaryResultFunc != nil {
		return engine.UnaryResultFunc(operator, operand)
	}
	if strings.EqualFold(operator, "NOT") {
		return engine.CoreTypes().Boolean, sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
}

// CoreTypes calls CoreTypesFunc when it is set, and otherwise returns DefaultCoreTypes.
//
// Returns dialect.CoreTypes which holds the callback or default core types.
func (engine *Engine) CoreTypes() dialect.CoreTypes {
	if engine.CoreTypesFunc != nil {
		return engine.CoreTypesFunc()
	}
	return DefaultCoreTypes()
}

// BuiltinFunctions calls BuiltinFunctionsFunc when it is set, and otherwise returns an
// empty function catalogue.
//
// Returns *ast.FunctionCatalogue which is the callback catalogue, or an empty catalogue.
func (engine *Engine) BuiltinFunctions() *ast.FunctionCatalogue {
	if engine.BuiltinFunctionsFunc != nil {
		return engine.BuiltinFunctionsFunc()
	}
	return &ast.FunctionCatalogue{
		Functions: make(map[string][]*sqlmodel.Function),
	}
}

// BuiltinTypes calls BuiltinTypesFunc when it is set, and otherwise returns an empty type
// catalogue.
//
// Returns *ast.TypeCatalogue which is the callback catalogue, or an empty catalogue.
func (engine *Engine) BuiltinTypes() *ast.TypeCatalogue {
	if engine.BuiltinTypesFunc != nil {
		return engine.BuiltinTypesFunc()
	}
	return &ast.TypeCatalogue{
		Types: make(map[string]sqlmodel.SQLType),
	}
}

// TableValuedFunctionColumns calls TableValuedFunctionColumnsFunc when it is set, and
// otherwise returns no columns.
//
// Takes functionName (string) which names the table-valued function.
//
// Returns []ast.ScopedColumn which holds the callback columns, or nil.
func (engine *Engine) TableValuedFunctionColumns(functionName string) []ast.ScopedColumn {
	if engine.TableValuedFunctionColumnsFunc != nil {
		return engine.TableValuedFunctionColumnsFunc(functionName)
	}
	return nil
}

// Info describes the fake dialect.
//
// InfoFunc replaces all metadata when set; otherwise individual callbacks override
// defaultInfo's fields.
//
// Returns dialect.Info which holds the configured dialect metadata.
func (engine *Engine) Info() dialect.Info {
	if engine.InfoFunc != nil {
		return engine.InfoFunc()
	}
	info := defaultInfo()
	if engine.SupportedExpressionsFunc != nil {
		info.Expressions = engine.SupportedExpressionsFunc()
	}
	if engine.CommentStyleFunc != nil {
		info.Comments = engine.CommentStyleFunc()
	}
	if engine.LexicalFunc != nil {
		info.Lexical = engine.LexicalFunc()
	}
	if engine.PlaceholdersFunc != nil {
		info.Placeholders = engine.PlaceholdersFunc()
	}
	if engine.SchemaSearchPathFunc != nil {
		info.SchemaSearchPath = engine.SchemaSearchPathFunc()
	}
	if engine.ReturningFunc != nil {
		info.Returning = engine.ReturningFunc()
	}
	if engine.DialectFunc != nil {
		info.Name = engine.DialectFunc()
	}
	if engine.SupportsAsyncMutationsFunc != nil {
		info.AsyncMutations = engine.SupportsAsyncMutationsFunc()
	}
	return info
}

// Emitter is a programmable code emitter. Emit calls EmitFunc when it is set, and
// otherwise records the request and returns no output.
type Emitter struct {
	// EmitFunc replaces the default behaviour of Emit when set.
	EmitFunc func(ctx context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error)

	// Requests records every request Emit received, in order.
	Requests []*emit.Request
}

// Emit records the request and calls EmitFunc when it is set, otherwise returning no
// files.
//
// Takes request (*emit.Request) which describes the package and queries to emit.
//
// Returns []sqlmodel.GeneratedFile which holds the callback-generated files, or nil.
// Returns error when the callback fails.
func (emitter *Emitter) Emit(ctx context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	emitter.Requests = append(emitter.Requests, request)
	if emitter.EmitFunc != nil {
		return emitter.EmitFunc(ctx, request)
	}
	return nil, nil
}

// CatalogueResolverEngine is an Engine that also resolves table-valued functions from the
// catalogue.
type CatalogueResolverEngine struct {
	*Engine

	// FromCatalogueFunc replaces TableValuedFunctionColumnsFromCatalogue.
	FromCatalogueFunc func(ctx context.Context, catalogue *sqlmodel.Catalogue, functionName string) ([]ast.ScopedColumn, error)
}

// TableValuedFunctionColumnsFromCatalogue calls FromCatalogueFunc when it is set, and
// otherwise returns no columns. A panic in FromCatalogueFunc is not recovered.
//
// Takes catalogue (*sqlmodel.Catalogue) which supplies the schema objects available to
// the query.
// Takes functionName (string) which names the table-valued function.
//
// Returns []ast.ScopedColumn which holds the callback columns, or nil.
// Returns error when the callback fails.
func (e *CatalogueResolverEngine) TableValuedFunctionColumnsFromCatalogue(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	functionName string,
) ([]ast.ScopedColumn, error) {
	if e.FromCatalogueFunc != nil {
		return e.FromCatalogueFunc(ctx, catalogue, functionName)
	}
	return nil, nil
}

// FunctionResolverEngine is an Engine that also resolves function calls itself.
type FunctionResolverEngine struct {
	*Engine

	// ResolveFunc replaces ResolveFunctionCall.
	ResolveFunc func(ctx context.Context, name string, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error)
}

// ResolveFunctionCall calls ResolveFunc when it is set, and otherwise returns no
// resolution. A panic in ResolveFunc is not recovered.
//
// Takes name (string) which is the name to resolve.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds argument types in call order.
//
// Returns *ast.FunctionResolution which is the callback resolution, or nil.
// Returns error when the callback fails.
func (e *FunctionResolverEngine) ResolveFunctionCall(
	ctx context.Context,
	_ *sqlmodel.Catalogue,
	name string,
	_ string,
	argumentTypes []sqlmodel.SQLType,
) (*ast.FunctionResolution, error) {
	if e.ResolveFunc == nil {
		return nil, nil
	}
	return e.ResolveFunc(ctx, name, argumentTypes)
}

// DefaultCoreTypes returns the fake dialect's core types: a boolean, text, a 64-bit
// count, JSON, a 32-bit row count and a 64-bit row identifier, named as the type
// resolver's tests spell them, and no void type.
//
// Returns dialect.CoreTypes which holds the default core types.
func DefaultCoreTypes() dialect.CoreTypes {
	return dialect.CoreTypes{
		Boolean:  sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "boolean"),
		Text:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		Count:    sqlmodel.NewIntegerType("int8", widthLong, false),
		JSON:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"),
		RowCount: sqlmodel.NewIntegerType("int4", widthWord, false),
		RowID:    sqlmodel.NewIntegerType("integer", widthLong, false),
		Void:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
	}
}

// NewCatalogue returns an empty catalogue whose default schema, search path and only
// schema is the named one, matching identifiers regardless of case as the fake dialect
// does.
//
// Takes schema (string) which names the default schema.
//
// Returns *sqlmodel.Catalogue which holds the empty schema.
func NewCatalogue(schema string) *sqlmodel.Catalogue {
	return &sqlmodel.Catalogue{
		DefaultSchema: schema,
		SearchPath:    []string{schema},
		Identifiers:   sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
		Schemas: map[string]*sqlmodel.Schema{
			schema: {
				Name:           schema,
				Tables:         make(map[string]*sqlmodel.Table),
				Views:          make(map[string]*sqlmodel.View),
				Enums:          make(map[string]*sqlmodel.Enum),
				Functions:      make(map[string][]*sqlmodel.Function),
				CompositeTypes: make(map[string]*sqlmodel.CompositeType),
				Sequences:      make(map[string]*sqlmodel.Sequence),
				Domains:        make(map[string]*sqlmodel.Domain),
			},
		},
		Extensions: make(map[string]struct{}),
	}
}

// NewTable returns a table with the given name and columns.
//
// Takes name (string) which names the table.
// Takes columns (...sqlmodel.Column) which are the table's columns.
//
// Returns *sqlmodel.Table which holds the table.
func NewTable(name string, columns ...sqlmodel.Column) *sqlmodel.Table {
	return &sqlmodel.Table{
		Name:              name,
		Schema:            "",
		Comment:           "",
		VirtualModuleName: "",
		Columns:           columns,
		PrimaryKey:        nil,
		Indexes:           nil,
		Constraints:       nil,
		PartitionOf:       nil,
		Origin:            sqlmodel.MigrationOrigin{},
		IsVirtual:         false,
		IsWithoutRowID:    false,
	}
}

// defaultInfo returns the fake dialect's Info before the function fields replace any of
// it; Info documents the defaults.
//
// Returns dialect.Info which holds the defaults.
func defaultInfo() dialect.Info {
	return dialect.Info{
		Comments:               ast.CommentStyle{LinePrefix: "--"},
		Lexical:                dollarLexicalRules(),
		Name:                   "mock",
		Family:                 "mock",
		SchemaSearchPath:       []string{"public"},
		RowIDAliases:           []string{"rowid", "_rowid_", "oid"},
		SystemColumns:          nil,
		SelectAliases:          ast.ClauseGroupBy | ast.ClauseOrderBy,
		SelectAliasOperands:    0,
		OperatorFeatures:       nil,
		Placeholders:           dollarPlaceholders(),
		Expressions:            allExpressionFeatures,
		Identifiers:            sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
		Returning:              ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete,
		AsyncMutations:         false,
		ScalarFunctionsInFrom:  false,
		UngroupedColumns:       dialect.UngroupedColumnsNullable,
		ViewsFollowTheirTables: false,
		IdentifierQuote:        dialect.IdentifierQuote{Open: `"`, Close: `"`},
		Drivers:                nil,
		ToJSON:                 "",
		TextType:               "",
		Filters: dialect.FilterSyntax{
			Operators:         nil,
			JSONPathFunctions: nil,
			Casts:             []dialect.CastForm{{Before: "CAST(", Middle: " AS ", After: ")"}},
			CastTypes:         []string{"integer", "text"},
		},
		Lists: dialect.ListSyntax{
			EmptyMembership: "IN (SELECT NULL WHERE FALSE)",
			EmptyExclusion:  "NOT IN (SELECT NULL WHERE FALSE)",
			ArrayMembership: dialect.Affix{},
			ArrayExclusion:  dialect.Affix{},
		},
		Pagination: dialect.PaginationSyntax{
			Limit:          dialect.Affix{Before: "LIMIT ", After: ""},
			Offset:         dialect.Affix{Before: "OFFSET ", After: ""},
			UnboundedLimit: "",
			OffsetFirst:    false,
		},
		MaxParameters: 0,
	}
}

// dollarLexicalRules returns the default lexical rules: standard SQL, with $1 read as a
// parameter.
//
// Returns sqllex.Rules which holds the rules.
func dollarLexicalRules() sqllex.Rules {
	var rules sqllex.Rules
	rules.Parameters.DollarNumbers = true
	return rules
}

// dollarPlaceholders returns the default placeholder description: numbered $1
// placeholders, written and executed alike.
//
// Returns dialect.Placeholders which is the description.
func dollarPlaceholders() dialect.Placeholders {
	numbered := dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}
	return dialect.Placeholders{Sigils: []dialect.PlaceholderSigil{numbered}, Execution: numbered, Positional: dialect.PlaceholderSigil{}, NamePrefix: ""}
}
