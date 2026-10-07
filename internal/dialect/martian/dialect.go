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
	"context"
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// Name is the dialect's name and family.
	Name = "martian"

	// searchPath is the one schema unqualified names resolve in, written unquoted and so
	// folded to DBO.
	searchPath = "dbo"

	// maxParameters is the most parameters one Martian statement binds.
	maxParameters = 2100

	// notOperator is Martian's logical negation.
	notOperator = "NOT"

	// expressionFeatures is the set of expression kinds Martian supports; string
	// concatenation is absent because Martian has no || operator.
	expressionFeatures = ast.SQLFeatureBinaryArithmetic |
		ast.SQLFeatureBinaryComparison |
		ast.SQLFeatureIsNull |
		ast.SQLFeatureInList |
		ast.SQLFeatureLogicalOp
)

// Dialect is the Martian engine. The zero value is not used; call New.
type Dialect struct {
	// functions is the built-in function catalogue.
	functions *ast.FunctionCatalogue
}

var (
	_ dialect.DialectPort = (*Dialect)(nil)

	// placeholder is Martian's only parameter form, @p followed by the parameter's number, a
	// prefix longer than one character.
	placeholder = dialect.PlaceholderSigil{Prefix: placeholderPrefix, Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}

	// castForms is Martian's one conversion, CONVERT(value TO type), which no real engine
	// writes, so a code generator that writes a cast of its own fails on Martian.
	castForms = []dialect.CastForm{{Before: "CONVERT(", Middle: " TO ", After: ")"}}

	// castTypes are the Martian types a runtime predicate's column may be converted to.
	castTypes = []string{typeFlag, typeWhole32, typeWhole64, typeReal64, typeTextual}

	// listSyntax is how Martian writes membership in a caller's list: IN and NOT IN its own
	// set with no rows for an empty list, and no array parameters.
	listSyntax = dialect.ListSyntax{
		EmptyMembership: "IN (SELECT NULL WHERE 1 = 0)",
		EmptyExclusion:  "NOT IN (SELECT NULL WHERE 1 = 0)",
		ArrayMembership: dialect.Affix{},
		ArrayExclusion:  dialect.Affix{},
	}

	// paginationSyntax is how Martian writes a row limit and an offset: the offset first,
	// OFFSET n ROWS, then FETCH NEXT n ROWS ONLY, each of which may stand alone, so a code
	// generator that writes LIMIT of its own fails on Martian.
	paginationSyntax = dialect.PaginationSyntax{
		Limit:          dialect.Affix{Before: "FETCH NEXT ", After: " ROWS ONLY"},
		Offset:         dialect.Affix{Before: "OFFSET ", After: " ROWS"},
		UnboundedLimit: "",
		OffsetFirst:    true,
	}

	// lexicalRules describes how Martian writes SQL text: '...' strings with doubled quotes,
	// [...] quoted identifiers, -- and non-nesting /* */ comments, @p placeholders and ";"
	// as the terminator. Martian has no lexical package of its own, so it declares the rules
	// here.
	lexicalRules = sqllex.Rules{
		ProceduralBlock:       nil,
		RequiresNoTransaction: nil,
		Terminator:            sqllex.DefaultTerminator,
		Quotes: sqllex.QuoteRules{
			StringPrefixes:    "",
			SingleQuoteEscape: sqllex.EscapeDoubled,
			DoubleQuote:       sqllex.DoubleQuoteIdentifier,
			DoubleQuoteEscape: sqllex.EscapeDoubled,
			BacktickEscape:    sqllex.EscapeDoubled,
			Backticks:         false,
			Brackets:          true,
			DollarQuoting:     false,
			EscapeStrings:     false,
		},
		Parameters: sqllex.ParameterRules{
			NamedSigils:                  placeholderPrefix[:1],
			NoNamedParameterAfterOperand: false,
			QuestionMark:                 false,
			NumberedQuestionMark:         false,
			DollarNumbers:                false,
			Braced:                       false,
			BracedLiteralWords:           nil,
		},
		Comments: sqllex.CommentRules{
			DoubleDashRequiresWhitespace: false,
			HashLineComments:             false,
			NestedBlockComments:          false,
			ExecutableComments:           false,
		},
		DelimiterCommand:             false,
		MetaCommands:                 false,
		TerminatorOutsideParentheses: false,
		DollarInIdentifiers:          false,
	}
)

// New returns the Martian dialect.
//
// Returns *Dialect which is ready to use.
func New() *Dialect {
	return &Dialect{functions: builtinFunctions()}
}

// Info describes Martian's lexical rules, placeholders, capabilities and generated SQL
// syntax, built afresh on each call so the caller may change it.
//
// Returns dialect.Info which holds the dialect's identity, syntax and capabilities.
func (*Dialect) Info() dialect.Info {
	return dialect.Info{
		Comments:               ast.CommentStyle{LinePrefix: lineCommentPrefix},
		Lexical:                lexicalRules,
		Name:                   Name,
		Family:                 Name,
		SchemaSearchPath:       []string{searchPath},
		RowIDAliases:           nil,
		SystemColumns:          nil,
		SelectAliases:          ast.ClauseWhere | ast.ClauseOrderBy,
		SelectAliasOperands:    ast.ClauseWhere,
		OperatorFeatures:       nil,
		Placeholders:           dialect.Placeholders{Sigils: []dialect.PlaceholderSigil{placeholder}, Execution: placeholder, Positional: dialect.PlaceholderSigil{}, NamePrefix: ""},
		Expressions:            expressionFeatures,
		Identifiers:            sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingUpper, Insensitive: false},
		Returning:              0,
		AsyncMutations:         false,
		ScalarFunctionsInFrom:  true,
		UngroupedColumns:       dialect.UngroupedColumnsNullable,
		ViewsFollowTheirTables: false,
		IdentifierQuote:        dialect.IdentifierQuote{Open: "[", Close: "]"},
		Drivers:                nil,
		ToJSON:                 "",
		TextType:               "",
		Filters:                dialect.FilterSyntax{Operators: nil, JSONPathFunctions: nil, Casts: slices.Clone(castForms), CastTypes: slices.Clone(castTypes)},
		Lists:                  listSyntax,
		Pagination:             paginationSyntax,
		MaxParameters:          maxParameters,
	}
}

// ParseStatements parses Martian SQL into statements, stopping before the next statement
// with the context's cause once the context is done. A panic is recovered and returned as
// a dialectkit.PanicError, and its stack is logged once at Warn through the context's
// logger.
//
// Takes sql (string) which is the SQL text.
//
// Returns statements ([]ast.ParsedStatement) which holds one entry per statement.
// Returns err (error) when the context is done, the SQL is not valid Martian, or parsing
// panics.
func (*Dialect) ParseStatements(ctx context.Context, sql string) (statements []ast.ParsedStatement, err error) {
	defer func() { dialectkit.Recover(ctx, "martian: parse statements", recover(), &statements, &err) }()

	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}
	return parseStatements(ctx, sql)
}

// ApplyDDL returns the catalogue mutation of CREATE TABLE, and nil for any other
// statement. A panic is recovered and returned as a dialectkit.PanicError, and its stack
// is logged once at Warn through the context's logger.
//
// Takes parsed (ast.ParsedStatement) which is a statement from ParseStatements.
//
// Returns mutation (*ast.CatalogueMutation) which is the mutation, or nil for DML.
// Returns err (error) when the context is done, the statement was parsed by another
// dialect (wrapping dialect.ErrForeignStatement), or applying it panics.
func (*Dialect) ApplyDDL(ctx context.Context, parsed ast.ParsedStatement) (mutation *ast.CatalogueMutation, err error) {
	defer func() { dialectkit.Recover(ctx, "martian: apply DDL", recover(), &mutation, &err) }()

	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}
	raw, err := martianStatement(parsed)
	if err != nil {
		return nil, err
	}
	if raw.createTable == nil {
		return nil, nil
	}
	return createTableMutation(raw.createTable), nil
}

// AnalyseQuery returns the raw analysis of a SELECT, INSERT, UPDATE or DELETE, with the
// expressions of its clauses and the spans of its parts, stopping with the context's
// cause once the context is done, before or while it walks a select list.
//
// A panic is recovered and returned as a dialectkit.PanicError, and its stack is logged
// once at Warn through the context's logger.
//
// Takes parsed (ast.ParsedStatement) which is a statement from ParseStatements.
//
// Returns analysis (*ast.RawQueryAnalysis) which describes the query.
// Returns err (error) when the context is done, the statement is DDL or was parsed by
// another dialect (wrapping dialect.ErrForeignStatement), or analysing it panics.
func (d *Dialect) AnalyseQuery(
	ctx context.Context,
	_ *sqlmodel.Catalogue,
	parsed ast.ParsedStatement,
) (analysis *ast.RawQueryAnalysis, err error) {
	defer func() { dialectkit.Recover(ctx, "martian: analyse query", recover(), &analysis, &err) }()

	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}
	raw, err := martianStatement(parsed)
	if err != nil {
		return nil, err
	}
	switch {
	case raw.selection != nil:
		if analysis, err = analyseSelect(ctx, raw.selection); err != nil {
			return nil, err
		}
	case raw.insertion != nil:
		analysis = analyseInsert(raw.insertion)
	case raw.update != nil:
		analysis = analyseUpdate(raw.update)
	case raw.deletion != nil:
		analysis = analyseDelete(raw.deletion)
	default:
		return nil, raw.syntaxErrorAtStart("CREATE TABLE is not a query")
	}
	recordSpans(analysis, raw, parsed, d.Info().Placeholders)
	return analysis, nil
}

// NormaliseTypeName turns a Martian type name and its modifiers into a structured type.
//
// Takes name (string) which is the type name in any case.
// Takes modifiers (...int) which are a TEXTUAL length or an EXACT precision and scale.
//
// Returns sqlmodel.SQLType which is the structured type.
func (*Dialect) NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	return normaliseTypeName(name, modifiers)
}

// PromoteType returns the wider of two types.
//
// Takes left (sqlmodel.SQLType) which is the first operand's type.
// Takes right (sqlmodel.SQLType) which is the second operand's type.
//
// Returns sqlmodel.SQLType which is the promoted type.
func (*Dialect) PromoteType(left sqlmodel.SQLType, right sqlmodel.SQLType) sqlmodel.SQLType {
	return promoteType(left, right)
}

// CanImplicitCast reports whether Martian widens one type to another without a cast.
//
// Takes from (sqlmodel.SQLType) which is the source type.
// Takes to (sqlmodel.SQLType) which is the target type.
//
// Returns bool which is true when the cast is implicit.
func (*Dialect) CanImplicitCast(from sqlmodel.SQLType, to sqlmodel.SQLType) bool {
	return canImplicitCast(from, to)
}

// CoreTypes returns Martian's core types.
//
// Returns dialect.CoreTypes which holds FLAG, TEXTUAL and WHOLE64, and no JSON type.
func (*Dialect) CoreTypes() dialect.CoreTypes {
	return coreTypes
}

// BinaryResult leaves every Martian binary operator to the structural default: its
// arithmetic promotes its operands and its comparisons are typed by the analyser.
//
// Returns sqlmodel.SQLType which is unset.
// Returns sqlmodel.FunctionNullableBehaviour which is unset.
// Returns bool which is always false.
func (*Dialect) BinaryResult(_ string, _ sqlmodel.SQLType, _ sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
}

// UnaryResult types NOT as a FLAG that is NULL when its operand is, and leaves every
// other prefix operator to the structural default.
//
// Takes operator (string) which is the operator.
//
// Returns sqlmodel.SQLType which is FLAG for NOT.
// Returns sqlmodel.FunctionNullableBehaviour which is NULL on a NULL operand for NOT.
// Returns bool which is true only for NOT.
func (*Dialect) UnaryResult(operator string, _ sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	if strings.EqualFold(operator, notOperator) {
		return builtinTypes[typeFlag], sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
}

// BuiltinFunctions returns Martian's built-in functions.
//
// Returns *ast.FunctionCatalogue which maps function names to signatures.
func (d *Dialect) BuiltinFunctions() *ast.FunctionCatalogue {
	return d.functions
}

// BuiltinTypes returns Martian's built-in types.
//
// Returns *ast.TypeCatalogue which maps type names to structured types.
func (*Dialect) BuiltinTypes() *ast.TypeCatalogue {
	return typeCatalogue()
}

// TableValuedFunctionColumns returns nil: Martian has no table-valued functions.
//
// Returns []ast.ScopedColumn which is always nil.
func (*Dialect) TableValuedFunctionColumns(_ string) []ast.ScopedColumn {
	return nil
}

// martianStatement unwraps a parsed statement produced by ParseStatements.
//
// Takes parsed (ast.ParsedStatement) which is the statement.
//
// Returns *statement which is the Martian statement.
// Returns error which wraps dialect.ErrForeignStatement when the statement did not come
// from Martian's parser.
func martianStatement(parsed ast.ParsedStatement) (*statement, error) {
	raw, isMartian := parsed.Raw.(*statement)
	if !isMartian || raw == nil {
		return nil, fmt.Errorf("%w: %T", dialect.ErrForeignStatement, parsed.Raw)
	}
	return raw, nil
}

// builtinFunctions builds Martian's function catalogue: CONCAT, LENGTH, UPPER, NOW and
// COUNT.
//
// Returns *ast.FunctionCatalogue which holds the signatures.
func builtinFunctions() *ast.FunctionCatalogue {
	builder := dialectkit.NewCatalogueBuilder()
	textual := builtinTypes[typeTextual]
	whole64 := builtinTypes[typeWhole64]
	text := dialectkit.Arg{Name: "value", Type: textual}
	concatenation := builder.NullOnNull("CONCAT", builder.Args(text, text), textual)
	concatenation.IsVariadic = true
	concatenation.MinArguments = 1
	builder.NullOnNull("LENGTH", builder.Args(text), whole64)
	builder.NullOnNull("UPPER", builder.Args(text), textual)
	builder.NeverNull("NOW", nil, builtinTypes[typeMoment])
	counter := builder.NeverNull("COUNT", nil, whole64)
	counter.IsAggregate = true
	counter.IsVariadic = true
	return builder.Catalogue
}
