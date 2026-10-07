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
	"errors"
	"fmt"
	"go/ast"
	"go/token"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/methods"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// orderByKeyword starts the ORDER BY a sortable query writes.
	orderByKeyword = " ORDER BY "

	// directionSeparator separates a sortable column from its direction.
	directionSeparator = " "

	// identOrderAscending is the ascending sort direction declared in sortable.go.
	identOrderAscending = "OrderAsc"

	// identOrderDescending is the descending sort direction declared in sortable.go.
	identOrderDescending = "OrderDesc"
)

var (
	// errListAfterOrderBy reports a slice list a sortable query writes after the place of
	// its ORDER BY, whose offset the ORDER BY would move.
	errListAfterOrderBy = errors.New("a slice list after the place of a sortable query's ORDER BY is not supported")
)

// BuildDynamicDeclarations generates the params struct and the ORDER BY enum of each
// sortable parameter of a dynamic query, named by the naming plan. The OrderDirection
// type the enums' direction fields use is declared once per package, in sortable.go.
//
// Takes query (*gogen.Query) which is the dynamic query.
//
// Returns []ast.Decl which holds the params struct and any enum declarations.
func BuildDynamicDeclarations(query *gogen.Query) []ast.Decl {
	declarations := []ast.Decl{buildDynamicParamsStruct(query)}
	if sortable := findSortableParameter(query.Analysed); sortable != nil {
		declarations = append(declarations, buildOrderByEnum(query, sortable.SortableColumns)...)
	}
	return declarations
}

// UsesSortable reports whether any query has a sortable parameter, so its package needs
// sortable.go.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the package's queries.
//
// Returns bool which is true when some query is sortable.
func UsesSortable(queries []*sqlmodel.AnalysedQuery) bool {
	for _, query := range queries {
		if query.IsDynamic && findSortableParameter(query) != nil {
			return true
		}
	}
	return false
}

// Prelude returns what the method of a dynamic query does before it runs the query: it
// settles the pagination parameters and, for a sortable query, writes the ORDER BY the
// caller chose into the query local at the place rendering marked, and runs that local.
//
// Takes query (*gogen.Query) which is the dynamic query.
//
// Returns *methods.Prelude which holds the statements and the SQL they leave.
// Returns error when a slice list follows the ORDER BY place, whose offset the ORDER BY
// would move.
func Prelude(query *gogen.Query) (*methods.Prelude, error) {
	statements := buildParamsInitStatements(query)
	index := sortableIndex(query.Analysed)
	if index < 0 {
		return &methods.Prelude{Source: methods.SQLConstant(query), Statements: statements}, nil
	}
	point := len(query.Rendered.SQL)
	if mark := query.Rendered.Marks.OrderBy; mark != nil {
		point = mark.Start
	}
	for _, list := range query.Rendered.Marks.Lists {
		if list.Operator.Start >= point {
			return nil, fmt.Errorf("query %q: %w", query.Analysed.Name, errListAfterOrderBy)
		}
	}
	statements = append(statements, buildSortableQueryInit(query, index, point)...)
	return &methods.Prelude{Source: goastutil.CachedIdent(goast.IdentQuery), Statements: statements}, nil
}

// buildDynamicParamsStruct constructs the exported Params struct containing all
// parameters, named by the naming plan.
//
// Optional parameters are pointer types, LIMIT/OFFSET parameters are int, and sortable
// parameters produce an enum field plus a direction companion field. Other parameters use
// their mapped or overridden Go types.
//
// Takes query (*gogen.Query) which is the dynamic query.
//
// Returns ast.Decl which holds the params struct type declaration.
func buildDynamicParamsStruct(query *gogen.Query) ast.Decl {
	parameters := query.Analysed.Parameters
	fields := make([]*ast.Field, 0, len(parameters)+1)
	for index := range parameters {
		parameter := &parameters[index]
		field := query.Naming.ParameterFields[index]
		switch {
		case parameter.IsOptional:
			goType := goast.ResolveGoType(parameter.SQLType, false, query.Types)
			if parameter.GoTypeOverride != nil {
				goType = *parameter.GoTypeOverride
			}
			fields = append(fields, goastutil.Field(field, goastutil.StarExpr(query.Imports.AddType(goType))))
		case parameter.Kind == sqlmodel.ParameterDirectiveSortable:
			fields = append(fields,
				goastutil.Field(field, goastutil.CachedIdent(query.Naming.OrderBy)),
				goastutil.Field(query.Naming.DirectionField, goastutil.CachedIdent(goast.IdentOrderDirection)),
			)
		default:
			fields = append(fields, goastutil.Field(field, methods.ParameterType(query, parameter)))
		}
	}
	return goastutil.GenDeclType(query.Naming.Params, goastutil.StructType(fields...))
}

// buildParamsInitStatements generates inline default and clamping statements for limit
// parameters operating directly on the params argument.
//
// Takes query (*gogen.Query) which is the query.
//
// Returns []ast.Stmt which holds the default-value and max-clamping if statements.
func buildParamsInitStatements(query *gogen.Query) []ast.Stmt {
	var statements []ast.Stmt
	parameters := query.Analysed.Parameters
	for index := range parameters {
		parameter := &parameters[index]
		if !parameter.IsPaginationBound() || parameter.IsOptional {
			continue
		}
		fieldAccess := methods.ParameterAccess(query, index)
		if parameter.DefaultLimit != nil {
			statements = append(statements, assignIf(fieldAccess, token.EQL, 0, *parameter.DefaultLimit))
		}
		statements = append(statements, assignIf(fieldAccess, token.LSS, 0, 0))
		if parameter.MaxLimit != nil {
			statements = append(statements, assignIf(fieldAccess, token.GTR, *parameter.MaxLimit, *parameter.MaxLimit))
		}
	}
	return statements
}

// assignIf returns the statement if access <operator> bound { access = value }.
//
// Takes access (ast.Expr) which reads and writes the parameter.
// Takes operator (token.Token) which compares it with the bound.
// Takes bound (int) which it is compared with.
// Takes value (int) which it is set to.
//
// Returns ast.Stmt which is the statement.
func assignIf(access ast.Expr, operator token.Token, bound, value int) ast.Stmt {
	return &ast.IfStmt{
		Cond: &ast.BinaryExpr{X: access, Op: operator, Y: goastutil.IntLit(bound)},
		Body: goastutil.BlockStmt(goastutil.AssignStmt(access, goastutil.IntLit(value))),
	}
}

// findSortableParameter returns the first sortable parameter from the query, or nil if
// none exists.
//
// Takes query (*sqlmodel.AnalysedQuery) which holds the analysed query to search.
//
// Returns *sqlmodel.QueryParameter which holds the first sortable parameter, or nil if
// none exists.
func findSortableParameter(query *sqlmodel.AnalysedQuery) *sqlmodel.QueryParameter {
	for index := range query.Parameters {
		if query.Parameters[index].Kind == sqlmodel.ParameterDirectiveSortable {
			return &query.Parameters[index]
		}
	}
	return nil
}

// sortableIndex returns the index of a query's first sortable parameter, or -1.
//
// Takes query (*sqlmodel.AnalysedQuery) which holds the parameters.
//
// Returns int which is the index.
func sortableIndex(query *sqlmodel.AnalysedQuery) int {
	for index := range query.Parameters {
		if query.Parameters[index].Kind == sqlmodel.ParameterDirectiveSortable {
			return index
		}
	}
	return -1
}

// buildOrderByEnum generates the ORDER BY enum type and constants, named by the naming
// plan, each constant holding its column's name.
//
// Takes query (*gogen.Query) which names the type and constants.
// Takes columns ([]string) which holds the sortable column names.
//
// Returns []ast.Decl which holds the type declaration and const block.
func buildOrderByEnum(query *gogen.Query, columns []string) []ast.Decl {
	enumType := query.Naming.OrderBy
	specs := make([]ast.Spec, len(columns))
	for index, column := range columns {
		specs[index] = &ast.ValueSpec{
			Names:  []*ast.Ident{goastutil.CachedIdent(query.Naming.OrderByConstants[index])},
			Type:   goastutil.CachedIdent(enumType),
			Values: []ast.Expr{goastutil.StrLit(column)},
		}
	}
	return []ast.Decl{
		goastutil.GenDeclType(enumType, goastutil.CachedIdent(goast.IdentString)),
		&ast.GenDecl{Tok: token.CONST, Lparen: 1, Specs: specs},
	}
}

// buildSortableQueryInit constructs the query local and the ORDER BY written into it.
//
// When the ORDER BY goes before the end of the SQL constant, such as before a LIMIT, a
// locking clause or the statement's terminator, the constant is split at the place: the
// local starts from the head, the ORDER BY is appended, and the tail follows it.
// Otherwise the ORDER BY is appended to the whole constant.
//
// Takes query (*gogen.Query) which is the sortable query.
// Takes sortable (int) which is the index of its sortable parameter.
// Takes point (int) which is the offset of the ORDER BY in the SQL constant.
//
// Returns []ast.Stmt which holds the query local's definition and the ORDER BY append.
func buildSortableQueryInit(query *gogen.Query, sortable, point int) []ast.Stmt {
	constant := methods.SQLConstant(query)
	orderBy := buildSortableOrderByAppend(query, sortable)
	if point >= len(query.Rendered.SQL) {
		return append([]ast.Stmt{goastutil.DefineStmt(goast.IdentQuery, constant)}, orderBy...)
	}
	statements := []ast.Stmt{
		goastutil.DefineStmt(goast.IdentQuery, &ast.SliceExpr{X: constant, High: goastutil.IntLit(point)}),
	}
	statements = append(statements, orderBy...)
	return append(statements, goastutil.AssignStmt(
		goastutil.CachedIdent(goast.IdentQuery),
		&ast.BinaryExpr{
			X:  goastutil.CachedIdent(goast.IdentQuery),
			Op: token.ADD,
			Y:  &ast.SliceExpr{X: constant, Low: goastutil.IntLit(point)},
		},
	))
}

// sortableQueryConcatAssign builds the statement `query = query + (prefix +
// string(params.<field>))`, used to append a validated ORDER BY column or direction onto
// the running query string.
//
// Takes prefix (string) which is the literal SQL fragment prepended to the field value.
// Takes field (ast.Expr) which reads the params struct field to convert and append.
//
// Returns ast.Stmt which is the append assignment.
func sortableQueryConcatAssign(prefix string, field ast.Expr) ast.Stmt {
	return goastutil.AssignStmt(
		goastutil.CachedIdent(goast.IdentQuery),
		&ast.BinaryExpr{
			X:  goastutil.CachedIdent(goast.IdentQuery),
			Op: token.ADD,
			Y: &ast.BinaryExpr{
				X:  goastutil.StrLit(prefix),
				Op: token.ADD,
				Y:  goastutil.CallExpr(goastutil.CachedIdent(goast.IdentString), field),
			},
		},
	)
}

// buildSortableOrderByAppend constructs the conditional ORDER BY append statements that
// operate on an existing "query" local variable: the column only when it is one the
// directive lists, and the direction only when it is ascending or descending.
//
// Takes query (*gogen.Query) which is the sortable query.
// Takes sortable (int) which is the index of its sortable parameter.
//
// Returns []ast.Stmt which contains the conditional ORDER BY append.
func buildSortableOrderByAppend(query *gogen.Query, sortable int) []ast.Stmt {
	columns := query.Analysed.Parameters[sortable].SortableColumns
	if len(columns) == 0 {
		return nil
	}
	parametersArgument := goastutil.CachedIdent(query.Naming.ParamsArgument)
	sortField := goastutil.SelectorExprFrom(parametersArgument, query.Naming.ParameterFields[sortable])
	directionField := goastutil.SelectorExprFrom(parametersArgument, query.Naming.DirectionField)

	columnLabels := make([]ast.Expr, len(columns))
	for index, column := range columns {
		columnLabels[index] = goastutil.StrLit(column)
	}
	directionSwitch := &ast.SwitchStmt{
		Tag: directionField,
		Body: goastutil.BlockStmt(&ast.CaseClause{
			List: []ast.Expr{goastutil.CachedIdent(identOrderAscending), goastutil.CachedIdent(identOrderDescending)},
			Body: []ast.Stmt{sortableQueryConcatAssign(directionSeparator, directionField)},
		}),
	}
	return []ast.Stmt{&ast.SwitchStmt{
		Tag: goastutil.CallExpr(goastutil.CachedIdent(goast.IdentString), sortField),
		Body: goastutil.BlockStmt(&ast.CaseClause{
			List: columnLabels,
			Body: []ast.Stmt{sortableQueryConcatAssign(orderByKeyword, sortField), directionSwitch},
		}),
	}}
}
