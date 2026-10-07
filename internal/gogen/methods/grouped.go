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

package methods

import (
	"go/ast"
	"go/token"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// identEntry holds the identifier name for the current group entry variable.
	identEntry = "entry"

	// identExists holds the identifier name for the map lookup existence flag.
	identExists = "exists"

	// identGroupIndex holds the identifier name for the map that indexes groups by key.
	identGroupIndex = "groupIndex"
)

// groupedContext holds precomputed values for grouped method body generation.
type groupedContext struct {
	// KeyTypeExpression holds the AST type expression for the group-by key column.
	KeyTypeExpression ast.Expr

	// Emission holds the generation the method belongs to.
	Emission *gogen.Emission

	// Query holds the query being emitted and its names.
	Query *gogen.Query

	// KeyTable holds the table name from the group_by directive.
	KeyTable string

	// EmbedGroups holds the embed groups derived from the output columns.
	EmbedGroups []embedGroup

	// KeyColumnIndex holds the index of the group-by key column in the output columns.
	KeyColumnIndex int
}

// HasGroupByKey reports whether the query declares group_by keys in its header.
//
// Takes query (*sqlmodel.AnalysedQuery) which holds the analysed query to inspect.
//
// Returns bool which is true when the query has a non-empty GroupByKey and embedded
// columns.
func HasGroupByKey(query *sqlmodel.AnalysedQuery) bool {
	return len(query.GroupByKey) > 0 && hasEmbeddedColumns(query)
}

// GroupedManyMethod constructs a many method that groups rows by a key column, producing
// a result where detail-embed fields are slices. A query whose key column is not among
// its output columns gets the plain many method.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes prelude (*Prelude) which the method starts with.
//
// Returns *ast.FuncDecl which holds the complete grouped method AST node.
func GroupedManyMethod(emission *gogen.Emission, query *gogen.Query, prelude *Prelude) *ast.FuncDecl {
	keyColumnIndex := findKeyColumnIndex(query.Analysed)
	if keyColumnIndex == -1 || len(query.Naming.ColumnLocals) == 0 {
		return buildManyMethod(emission, query, prelude)
	}

	groupContext := &groupedContext{
		Query:             query,
		KeyColumnIndex:    keyColumnIndex,
		KeyTypeExpression: query.Imports.AddType(ColumnGoType(query, keyColumnIndex)),
		KeyTable:          groupByKeyTable(query.Analysed),
		EmbedGroups:       embedGroups(query),
		Emission:          emission,
	}

	call := PrepareCall(emission, query, prelude.Source, ReturnGuard(goastutil.CachedIdent(goast.IdentNil)))
	statements := concat(prelude.Statements, call.Statements)
	statements = append(statements, buildGroupedMethodBody(groupContext, &call)...)

	return methodDecl(query, goastutil.FieldList(
		goastutil.Field("", &ast.ArrayType{Elt: goastutil.CachedIdent(query.Naming.Row)}),
		goastutil.Field("", goastutil.CachedIdent(goast.IdentError)),
	), statements)
}

// groupByKeyColumn extracts the column name portion from the first group_by key (e.g.,
// "orders.id" -> "id").
//
// Takes query (*sqlmodel.AnalysedQuery) which holds the analysed query containing the
// group_by directive.
//
// Returns string which holds the column name portion, or empty if no group_by key exists.
func groupByKeyColumn(query *sqlmodel.AnalysedQuery) string {
	if len(query.GroupByKey) == 0 {
		return ""
	}
	parts := strings.SplitN(query.GroupByKey[0], ".", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return parts[0]
}

// findKeyColumnIndex returns the index of the group_by key column in the output columns.
//
// Takes query (*sqlmodel.AnalysedQuery) which holds the analysed query to search.
//
// Returns int which holds the column index, or -1 if not found.
func findKeyColumnIndex(query *sqlmodel.AnalysedQuery) int {
	keyColumn := groupByKeyColumn(query)
	keyTable := groupByKeyTable(query)

	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		if !strings.EqualFold(column.Name, keyColumn) {
			continue
		}
		if keyTable == "" ||
			column.IsEmbedded && strings.EqualFold(column.EmbedTable, keyTable) ||
			column.SourceTable != "" && strings.EqualFold(column.SourceTable, keyTable) {
			return index
		}
	}
	for index := range query.OutputColumns {
		if strings.EqualFold(query.OutputColumns[index].Name, keyColumn) {
			return index
		}
	}
	return -1
}

// buildGroupedMethodBody constructs the body of a grouped many method after its prelude:
// the query, the scan loop and the result assembly.
//
// Takes groupContext (*groupedContext) which holds precomputed values for the grouped
// method.
// Takes call (*Call) which runs the query.
//
// Returns []ast.Stmt which holds the method body statements.
func buildGroupedMethodBody(groupContext *groupedContext, call *Call) []ast.Stmt {
	statements := buildGroupedQueryExecution(groupContext, call)
	statements = append(statements,
		&ast.ForStmt{
			Cond: goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentRows), "Next")),
			Body: goastutil.BlockStmt(buildGroupedScanLoop(groupContext)...),
		},
		BuildRowsErrCheck(),
	)
	return append(statements, goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentResults), goastutil.CachedIdent(goast.IdentNil)))
}

// buildGroupedQueryExecution constructs the query execution, error check, defer close,
// and group index/order variable declarations.
//
// Takes groupContext (*groupedContext) which holds precomputed values for the grouped
// method.
// Takes call (*Call) which runs the query.
//
// Returns []ast.Stmt which holds the execution and initialisation statements.
func buildGroupedQueryExecution(groupContext *groupedContext, call *Call) []ast.Stmt {
	emission := groupContext.Emission
	return []ast.Stmt{
		goastutil.DefineStmtMulti(
			[]string{goast.IdentRows, goast.IdentErr},
			ConnectionCall(emission, groupContext.Query, emission.Driver.Connection.Query, call),
		),
		BuildErrCheck(goastutil.CachedIdent(goast.IdentNil)),
		closeRows(),
		goastutil.DefineStmt(identGroupIndex,
			goastutil.CallExpr(
				goastutil.CachedIdent("make"),
				&ast.MapType{
					Key:   groupContext.KeyTypeExpression,
					Value: goastutil.CachedIdent("int"),
				},
			),
		),
		goastutil.DefineStmt(goast.IdentResults,
			goastutil.CompositeLit(&ast.ArrayType{Elt: goastutil.CachedIdent(groupContext.Query.Naming.Row)})),
	}
}

// buildGroupedScanLoop constructs the for-loop body for a grouped query: a local per
// column, the scan into them, and the group check.
//
// Takes groupContext (*groupedContext) which holds precomputed values for the grouped
// method.
//
// Returns []ast.Stmt which holds the loop body statements.
func buildGroupedScanLoop(groupContext *groupedContext) []ast.Stmt {
	query := groupContext.Query
	columns := query.Analysed.OutputColumns
	scan := buildScan(groupContext.Emission, query, func(index int) ast.Expr {
		return goastutil.CachedIdent(query.Naming.ColumnLocals[index])
	})
	statements := make([]ast.Stmt, 0, len(columns)+len(scan.Declarations)+len(scan.Conversions)+len(scan.Checked)+2)
	for index := range columns {
		statements = append(statements, goastutil.VarDecl(query.Naming.ColumnLocals[index], query.Imports.AddType(ColumnGoType(query, index))))
	}
	statements = append(statements, scan.Declarations...)
	statements = append(statements,
		goastutil.IfStmt(
			goastutil.DefineStmt(goast.IdentErr,
				goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentRows), "Scan"), scan.Arguments...),
			),
			errorIsNotNil(),
			goastutil.BlockStmt(goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentNil), goastutil.CachedIdent(goast.IdentErr))),
		),
	)
	statements = append(statements, scan.Conversions...)
	statements = append(statements, scan.CheckedConversions(goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentNil), goastutil.CachedIdent(goast.IdentErr)))...)
	return append(statements, buildGroupExistsCheck(groupContext))
}

// buildGroupExistsCheck constructs the if-exists check that either appends detail rows to
// an existing group or creates a new group entry.
//
// Takes groupContext (*groupedContext) which holds precomputed values for the grouped
// method.
//
// Returns *ast.IfStmt which holds the if-exists branching statement.
func buildGroupExistsCheck(groupContext *groupedContext) *ast.IfStmt {
	query := groupContext.Query
	key := goastutil.CachedIdent(query.Naming.ColumnLocals[groupContext.KeyColumnIndex])
	detailAppends := buildDetailAppendStatements(groupContext)

	entryFields := buildKeyEmbedEntryFields(groupContext)
	entryFields = append(entryFields, columnValues(query, flatColumns(query.Analysed))...)

	elseStatements := make([]ast.Stmt, 0, 3+len(detailAppends))
	elseStatements = append(elseStatements,
		goastutil.DefineStmt(identEntry,
			goastutil.CallExpr(goastutil.CachedIdent("len"), goastutil.CachedIdent(goast.IdentResults)),
		),
		goastutil.AssignStmt(
			&ast.IndexExpr{X: goastutil.CachedIdent(identGroupIndex), Index: key},
			goastutil.CachedIdent(identEntry),
		),
		appendTo(goast.IdentResults, goastutil.CompositeLit(goastutil.CachedIdent(query.Naming.Row), entryFields...)),
	)
	elseStatements = append(elseStatements, detailAppends...)

	return &ast.IfStmt{
		Init: goastutil.DefineStmtMulti(
			[]string{identEntry, identExists},
			&ast.IndexExpr{X: goastutil.CachedIdent(identGroupIndex), Index: key},
		),
		Cond: goastutil.CachedIdent(identExists),
		Body: goastutil.BlockStmt(detailAppends...),
		Else: goastutil.BlockStmt(elseStatements...),
	}
}

// columnValues returns the Field: local pairs that fill a struct holding some columns
// from their scanned locals.
//
// Takes query (*gogen.Query) which holds the columns' names.
// Takes columns ([]int) which are the indices of the columns.
//
// Returns []ast.Expr which holds the key-value expressions.
func columnValues(query *gogen.Query, columns []int) []ast.Expr {
	values := make([]ast.Expr, 0, len(columns))
	for _, column := range columns {
		values = append(values, &ast.KeyValueExpr{
			Key:   goastutil.CachedIdent(query.Naming.ColumnFields[column]),
			Value: goastutil.CachedIdent(query.Naming.ColumnLocals[column]),
		})
	}
	return values
}

// buildKeyEmbedEntryFields builds the composite literal fields for key (non-detail) embed
// groups.
//
// Takes groupContext (*groupedContext) which holds precomputed values for the grouped
// method.
//
// Returns []ast.Expr which holds the key-value expressions for the composite literal.
func buildKeyEmbedEntryFields(groupContext *groupedContext) []ast.Expr {
	var entryFields []ast.Expr
	for index := range groupContext.EmbedGroups {
		group := &groupContext.EmbedGroups[index]
		if isGroupByDetailEmbed(group, groupContext.KeyTable) {
			continue
		}
		entryFields = append(entryFields, &ast.KeyValueExpr{
			Key:   goastutil.CachedIdent(group.naming.Field),
			Value: goastutil.CompositeLit(goastutil.CachedIdent(group.naming.Type), columnValues(groupContext.Query, group.Columns)...),
		})
	}
	return entryFields
}

// buildDetailAppendStatements constructs the append statements for non-key (detail) embed
// groups in the grouped loop body.
//
// Takes groupContext (*groupedContext) which holds precomputed values for the grouped
// method.
//
// Returns []ast.Stmt which holds the append statements for detail embed groups.
func buildDetailAppendStatements(groupContext *groupedContext) []ast.Stmt {
	var statements []ast.Stmt
	for index := range groupContext.EmbedGroups {
		group := &groupContext.EmbedGroups[index]
		if !isGroupByDetailEmbed(group, groupContext.KeyTable) || len(group.Columns) == 0 {
			continue
		}
		field := goastutil.SelectorExprFrom(&ast.IndexExpr{X: goastutil.CachedIdent(goast.IdentResults), Index: goastutil.CachedIdent(identEntry)}, group.naming.Field)
		appendStatement := goastutil.AssignStmt(field,
			goastutil.CallExpr(goastutil.CachedIdent("append"), field,
				goastutil.CompositeLit(goastutil.CachedIdent(group.naming.Type), columnValues(groupContext.Query, group.Columns)...),
			),
		)
		if !group.IsOuter {
			statements = append(statements, appendStatement)
			continue
		}
		statements = append(statements, &ast.IfStmt{
			Cond: &ast.BinaryExpr{
				X:  goastutil.CachedIdent(groupContext.Query.Naming.ColumnLocals[group.Columns[0]]),
				Op: token.NEQ,
				Y:  goastutil.CachedIdent(goast.IdentNil),
			},
			Body: goastutil.BlockStmt(appendStatement),
		})
	}
	return statements
}
