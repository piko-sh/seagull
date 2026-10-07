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

package dynamic

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/methods"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// fieldParams is the builder field holding the query's parameters.
	fieldParams = "params"

	// fieldPredicates is the builder field holding the conditions Where added.
	fieldPredicates = "predicates"

	// fieldOrders is the builder field holding the orderings OrderBy added.
	fieldOrders = "orders"

	// fieldLimit is the builder field holding the row limit.
	fieldLimit = "limit"

	// fieldOffset is the builder field holding the rows skipped.
	fieldOffset = "offset"

	// fieldPendingError is the builder field holding the first error a chainable method met,
	// which the terminals return.
	fieldPendingError = "pendingError"

	// identColumn is the argument naming the column a chainable method filters or sorts.
	identColumn = "column"

	// identOperator is Where's operator argument.
	identOperator = "operator"

	// identValue is Where's value argument.
	identValue = "value"

	// identDirection is OrderBy's direction argument.
	identDirection = "direction"

	// identLimitArgument is the argument of Limit and Offset.
	identLimitArgument = "n"

	// identPredicate is the local holding the condition Where built.
	identPredicate = "predicate"

	// identOrder is the local holding the ordering OrderBy built.
	identOrder = "order"

	// identInt64 is the int64 type the Count terminal returns.
	identInt64 = "int64"

	// suffixPredicate names the helper type of one condition.
	suffixPredicate = "Predicate"

	// suffixNewPredicate names the helper that checks and builds a condition.
	suffixNewPredicate = "NewPredicate"

	// suffixNewOrder names the helper that checks and builds an ordering.
	suffixNewOrder = "NewOrder"
)

// builder is what one runtime builder's declarations read: the generation, the query, the
// parts of its SQL a caller edits, and where the builder holds what.
type builder struct {
	// emission is the package's generation.
	emission *gogen.Emission

	// query is the query.
	query *gogen.Query

	// filter is the query's filter mark, or nil when it has no place for a condition.
	filter *emitkit.FilterMark

	// order is the query's ORDER BY mark, or nil when it has no place for an ordering.
	order *emitkit.Range

	// pagination is the place for a limit and an offset, or nil when the query has none or
	// writes its own.
	pagination *emitkit.Range
}

// newBuilder reads what a runtime builder's declarations need from its query.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the runtime builder query.
//
// Returns *builder which holds them.
func newBuilder(emission *gogen.Emission, query *gogen.Query) *builder {
	marks := &query.Rendered.Marks
	built := &builder{emission: emission, query: query, filter: marks.Filter, order: marks.OrderBy, pagination: nil}
	if marks.Pagination != nil && marks.Pagination.Start == marks.Pagination.End {
		built.pagination = marks.Pagination
	}
	return built
}

// allowedColumns constructs the package-level map from each column the query selects to
// the quoted, qualified source the builder writes for it.
//
// The column entries are sorted before emission so two runs of the generator over the
// same analysed query produce byte-identical output even when the analyser walks tables
// in a different order.
//
// Returns ast.Decl which is the variable declaration.
func (b *builder) allowedColumns() ast.Decl {
	query := b.query.Analysed
	sourceByName := make(map[string]string, len(query.AllowedColumns))
	sortedNames := make([]string, 0, len(query.AllowedColumns))
	for index := range query.AllowedColumns {
		name := query.AllowedColumns[index].Name
		sourceByName[name] = quoteSourceExpression(query.AllowedColumns[index].SourceExpression, b.emission)
		sortedNames = append(sortedNames, name)
	}
	slices.Sort(sortedNames)
	elements := make([]ast.Expr, 0, len(sortedNames))
	for _, name := range sortedNames {
		elements = append(elements, &ast.KeyValueExpr{Key: goastutil.StrLit(name), Value: goastutil.StrLit(sourceByName[name])})
	}
	return &ast.GenDecl{
		Tok: token.VAR,
		Specs: []ast.Spec{
			&ast.ValueSpec{
				Names: []*ast.Ident{goastutil.CachedIdent(b.query.Naming.AllowedColumns)},
				Values: []ast.Expr{&ast.CompositeLit{
					Type: &ast.MapType{Key: goastutil.CachedIdent(goast.IdentString), Value: goastutil.CachedIdent(goast.IdentString)},
					Elts: elements,
				}},
			},
		},
	}
}

// structDecl constructs the builder struct: the querier, the query's parameters, what the
// chainable methods add, and the first error one met.
//
// Returns ast.Decl which is the struct type declaration.
func (b *builder) structDecl() ast.Decl {
	fields := []*ast.Field{goastutil.Field(goast.IdentQueriesReceiver, goastutil.StarExpr(goastutil.CachedIdent(goast.IdentQueries)))}
	if parameters := b.parametersType(); parameters != nil {
		fields = append(fields, goastutil.Field(fieldParams, parameters))
	}
	if b.filter != nil {
		fields = append(fields, goastutil.Field(fieldPredicates, &ast.ArrayType{Elt: goastutil.CachedIdent(b.helper(suffixPredicate))}))
	}
	if b.order != nil {
		fields = append(fields, goastutil.Field(fieldOrders, &ast.ArrayType{Elt: goastutil.CachedIdent(goast.IdentString)}))
	}
	if b.pagination != nil {
		fields = append(fields,
			goastutil.Field(fieldLimit, goastutil.CachedIdent(goast.IdentInt)),
			goastutil.Field(fieldOffset, goastutil.CachedIdent(goast.IdentInt)),
		)
	}
	fields = append(fields, goastutil.Field(fieldPendingError, goastutil.CachedIdent(goast.IdentError)))
	return goastutil.GenDeclType(b.query.Naming.Builder, goastutil.StructType(fields...))
}

// parametersType returns the type of the builder field holding the query's parameters:
// its Params struct, the type of its one parameter, or nil when it has none.
//
// Returns ast.Expr which is the type, or nil.
func (b *builder) parametersType() ast.Expr {
	switch {
	case len(b.query.Analysed.Parameters) == 0:
		return nil
	case b.query.Naming.Params == "":
		return methods.ParameterType(b.query, &b.query.Analysed.Parameters[0])
	default:
		return goastutil.CachedIdent(b.query.Naming.Params)
	}
}

// entryPoint constructs the Queries method that creates the builder over the caller's
// parameters. It takes no context: each terminal takes the context its query runs with.
//
// Returns *ast.FuncDecl which is the entry point method declaration.
func (b *builder) entryPoint() *ast.FuncDecl {
	elements := []ast.Expr{goastutil.KeyValueIdent(goast.IdentQueriesReceiver, goastutil.CachedIdent(goast.IdentQueriesReceiver))}
	switch {
	case len(b.query.Analysed.Parameters) == 0:
	case b.query.Naming.Params == "":
		elements = append(elements, goastutil.KeyValueIdent(fieldParams, goastutil.CachedIdent(b.query.Naming.ParameterFields[0])))
	default:
		elements = append(elements, goastutil.KeyValueIdent(fieldParams, goastutil.CachedIdent(b.query.Naming.ParamsArgument)))
	}
	return &ast.FuncDecl{
		Recv: methods.Receiver(),
		Name: goastutil.CachedIdent(b.query.Naming.Method),
		Type: &ast.FuncType{
			Params:  methods.BuildArgumentParams(b.query),
			Results: goastutil.FieldList(goastutil.Field("", b.pointer())),
		},
		Body: goastutil.BlockStmt(goastutil.ReturnStmt(goastutil.AddressExpr(&ast.CompositeLit{
			Type: goastutil.CachedIdent(b.query.Naming.Builder),
			Elts: elements,
		}))),
	}
}

// whereMethod constructs Where(column, operator, value), which adds a condition the
// helper checks against the query's columns and the operator allow-list, or records the
// first error it meets for the terminals to return, so the chain never panics.
//
// Returns *ast.FuncDecl which is the Where method declaration.
func (b *builder) whereMethod() *ast.FuncDecl {
	return b.chainable("Where",
		goastutil.FieldList(
			goastutil.Field(identColumn, goastutil.CachedIdent(goast.IdentString)),
			goastutil.Field(identOperator, goastutil.CachedIdent(goast.IdentString)),
			goastutil.Field(identValue, goastutil.CachedIdent(goast.IdentAny)),
		),
		identPredicate, fieldPredicates,
		goastutil.CallExpr(goastutil.CachedIdent(b.helper(suffixNewPredicate)),
			goastutil.CachedIdent(identColumn), goastutil.CachedIdent(identOperator), goastutil.CachedIdent(identValue),
			goastutil.CachedIdent(b.query.Naming.AllowedColumns)),
	)
}

// orderByMethod constructs OrderBy(column, direction), which adds an ordering the helper
// checks against the query's columns and the direction allow-list, so callers compose
// multi-key sorts with repeated calls.
//
// Returns *ast.FuncDecl which is the OrderBy method declaration.
func (b *builder) orderByMethod() *ast.FuncDecl {
	return b.chainable("OrderBy",
		goastutil.FieldList(
			goastutil.Field(identColumn, goastutil.CachedIdent(goast.IdentString)),
			goastutil.Field(identDirection, goastutil.CachedIdent(goast.IdentString)),
		),
		identOrder, fieldOrders,
		goastutil.CallExpr(goastutil.CachedIdent(b.helper(suffixNewOrder)),
			goastutil.CachedIdent(identColumn), goastutil.CachedIdent(identDirection),
			goastutil.CachedIdent(b.query.Naming.AllowedColumns)),
	)
}

// chainable constructs a chainable method that builds one part with a helper call and
// appends it to a builder field, or records the helper's error as the pending error when
// there is none yet, and returns the builder.
//
// Takes name (string) which names the method.
// Takes parameters (*ast.FieldList) which are its arguments.
// Takes local (string) which names the part the call returns.
// Takes field (string) which is the builder field the part is appended to.
// Takes call (ast.Expr) which builds the part.
//
// Returns *ast.FuncDecl which is the method declaration.
func (b *builder) chainable(name string, parameters *ast.FieldList, local, field string, call ast.Expr) *ast.FuncDecl {
	return &ast.FuncDecl{
		Recv: b.receiver(),
		Name: goastutil.CachedIdent(name),
		Type: &ast.FuncType{Params: parameters, Results: goastutil.FieldList(goastutil.Field("", b.pointer()))},
		Body: goastutil.BlockStmt(
			goastutil.DefineStmtMulti([]string{local, goast.IdentErr}, call),
			&ast.IfStmt{
				Cond: &ast.BinaryExpr{X: goastutil.CachedIdent(goast.IdentErr), Op: token.NEQ, Y: goastutil.CachedIdent(goast.IdentNil)},
				Body: goastutil.BlockStmt(
					&ast.IfStmt{
						Cond: &ast.BinaryExpr{X: builderField(fieldPendingError), Op: token.EQL, Y: goastutil.CachedIdent(goast.IdentNil)},
						Body: goastutil.BlockStmt(goastutil.AssignStmt(builderField(fieldPendingError), goastutil.CachedIdent(goast.IdentErr))),
					},
					goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentBuilder)),
				),
			},
			goastutil.AssignStmt(builderField(field),
				goastutil.CallExpr(goastutil.CachedIdent("append"), builderField(field), goastutil.CachedIdent(local))),
			goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentBuilder)),
		),
	}
}

// setterMethod constructs a chainable method that sets an integer field on the builder.
//
// Takes name (string) which names the method.
// Takes field (string) which is the builder field to set.
//
// Returns *ast.FuncDecl which is the setter method declaration.
func (b *builder) setterMethod(name, field string) *ast.FuncDecl {
	return &ast.FuncDecl{
		Recv: b.receiver(),
		Name: goastutil.CachedIdent(name),
		Type: &ast.FuncType{
			Params:  goastutil.FieldList(goastutil.Field(identLimitArgument, goastutil.CachedIdent(goast.IdentInt))),
			Results: goastutil.FieldList(goastutil.Field("", b.pointer())),
		},
		Body: goastutil.BlockStmt(
			goastutil.AssignStmt(builderField(field), goastutil.CachedIdent(identLimitArgument)),
			goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentBuilder)),
		),
	}
}

// helper returns the name of a generated helper.
//
// Takes suffix (string) which is the helper's name without its prefix.
//
// Returns string which is the helper's name.
func (b *builder) helper(suffix string) string {
	return b.emission.Vocabulary.Helper(suffix)
}

// pointer returns the type *Builder.
//
// Returns ast.Expr which is the type.
func (b *builder) pointer() ast.Expr {
	return goastutil.StarExpr(goastutil.CachedIdent(b.query.Naming.Builder))
}

// receiver returns the receiver of the builder's methods.
//
// Returns *ast.FieldList which is the receiver declaration.
func (b *builder) receiver() *ast.FieldList {
	return goastutil.FieldList(goastutil.Field(goast.IdentBuilder, b.pointer()))
}

// parameterAccess returns the expression that reads one of the query's parameters from
// the builder.
//
// Takes index (int) which is the index into AnalysedQuery.Parameters.
//
// Returns ast.Expr which reads it.
func (b *builder) parameterAccess(index int) ast.Expr {
	return methods.ParameterFrom(b.query, builderField(fieldParams), index)
}

// BuildRuntimeBuilderDeclarations constructs all AST declarations for a runtime query
// builder: the allowed-columns variable, the count constant, the builder struct, the
// entry point, the chainable methods and the terminals.
//
// A builder gets Where when its query has a place for a condition, OrderBy when it has a
// place for an ordering, and Limit and Offset when it has a place for pagination and
// writes none of its own; its terminals write what the caller added at the places the
// query's rendering marked, so a condition joins the query's own inside parentheses, an
// ordering follows the query's ORDER BY keys, and pagination comes before any locking or
// trailing clause.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the runtime builder query.
//
// Returns []ast.Decl which contains the builder declarations.
func BuildRuntimeBuilderDeclarations(emission *gogen.Emission, query *gogen.Query) []ast.Decl {
	built := newBuilder(emission, query)
	declarations := []ast.Decl{built.allowedColumns()}
	if query.Count != nil {
		declarations = append(declarations, methods.BuildConstant(query.Naming.CountConstant, query.Count.SQL))
	}
	declarations = append(declarations, built.structDecl(), built.entryPoint())
	if built.filter != nil {
		declarations = append(declarations, built.whereMethod())
	}
	if built.order != nil {
		declarations = append(declarations, built.orderByMethod())
	}
	if built.pagination != nil {
		declarations = append(declarations, built.setterMethod("Limit", fieldLimit), built.setterMethod("Offset", fieldOffset))
	}
	declarations = append(declarations, built.buildQueryMethod(), built.allMethod(), built.oneMethod())
	if query.Count != nil {
		declarations = append(declarations, built.buildCountQueryMethod(), built.countMethod())
	}
	return declarations
}

// UsesRuntimeBuilder reports whether any query is a runtime builder, so its package needs
// the runtime helpers.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the package's queries.
//
// Returns bool which is true when some query is a runtime builder.
func UsesRuntimeBuilder(queries []*sqlmodel.AnalysedQuery) bool {
	for _, query := range queries {
		if query.DynamicRuntime {
			return true
		}
	}
	return false
}

// quoteSourceExpression quotes a runtime-builder source reference, so a reserved-word or
// special-character identifier stays valid in the SQL the builder writes. The reference
// is a bare "column" or a "qualifier.column"; each identifier is wrapped with the
// engine's quote characters.
//
// Takes sourceExpression (string) which is the unquoted source reference.
// Takes emission (*gogen.Emission) which supplies the engine's identifier quoting.
//
// Returns string which is the quoted reference.
func quoteSourceExpression(sourceExpression string, emission *gogen.Emission) string {
	qualifier, column, qualified := strings.Cut(sourceExpression, ".")
	if !qualified {
		return methods.QuoteIdentifier(emission, sourceExpression)
	}
	return methods.QuoteIdentifier(emission, qualifier) + "." + methods.QuoteIdentifier(emission, column)
}

// builderField constructs a builder.{fieldName} selector expression.
//
// Takes fieldName (string) which is the field to select.
//
// Returns ast.Expr which is the selector expression.
func builderField(fieldName string) ast.Expr {
	return goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentBuilder), fieldName)
}
