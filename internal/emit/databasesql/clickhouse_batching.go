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

package databasesql

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// clickHouseBatchesFile is the generated file holding the ClickHouse batch helpers.
	clickHouseBatchesFile = "clickhouse_batches.go"

	// identStatement is the local holding the prepared INSERT a row is appended through.
	identStatement = "statement"

	// batchedIntegerBits is the widest integer clickhouse-go appends from a Go integer.
	batchedIntegerBits = 64

	// batchedUUIDPackage is the import path of the UUID type clickhouse-go appends.
	batchedUUIDPackage = "github.com/google/uuid"
)

var (
	// ownedWriterLocals lists the locals a batch method writing on connections it owns
	// declares where a planned name could meet them.
	ownedWriterLocals = []string{identOwns}

	// batchingHelpers lists the suffixes of the helpers clickhouse_batches.go declares.
	batchingHelpers = []string{"ClickHouseBatch", "ClickHouseBatches"}

	// batchedCategories lists the SQL type categories whose values clickhouse-go appends to
	// a batch in the same way as a multi-row INSERT binds them. The native emitter's
	// columnar batch appends the same Go values in the same way.
	batchedCategories = []sqlmodel.SQLTypeCategory{
		sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryBoolean, sqlmodel.TypeCategoryText,
		sqlmodel.TypeCategoryUUID, sqlmodel.TypeCategoryTemporal, sqlmodel.TypeCategoryArray, sqlmodel.TypeCategoryMap,
	}
)

// batchesThroughClickHouse reports whether a batch or copyfrom method writes its rows
// through clickhouse-go's batch insert when it owns its connection. The driver must be
// clickhouse-go, the statement a plain INSERT with a column list and one VALUES row of
// distinct parameters, and each parameter a value the driver appends unchanged.
//
// Takes query (*gogen.Query) which is the query.
// Takes row (*emitkit.Row) which is its VALUES row.
// Takes perRow (bool) which is true when the method sends each row on its own.
//
// Returns []int which holds the parameter of each column, or nil when the method does not
// batch.
func (s *sqlDriver) batchesThroughClickHouse(query *gogen.Query, row *emitkit.Row, perRow bool) []int {
	analysed := query.Analysed
	if s.profile.module != driverClickHouse || perRow || len(analysed.InsertColumns) == 0 || !plainInsertText(query.Rendered.SQL, row) {
		return nil
	}
	columns, plain := emitkit.RowColumns(analysed)
	if !plain || len(columns) != len(analysed.Parameters) || len(columns) != len(analysed.InsertColumns) {
		return nil
	}
	for _, index := range columns {
		if !batchedParameter(query, &analysed.Parameters[index]) {
			return nil
		}
	}
	return columns
}

// batchingStatement builds the statement a method batching through clickhouse-go starts
// with. When its writer is a *sql.DB, the method writes every row in one transaction for
// each chunk a multi-row INSERT would send, and returns.
//
// Takes query (*gogen.Query) which is the query.
// Takes columns ([]int) which holds the parameter of each column.
// Takes chunk (int) which is how many rows one statement would carry.
//
// Returns ast.Stmt which is the statement.
func (s *sqlDriver) batchingStatement(query *gogen.Query, columns []int, chunk int) ast.Stmt {
	imports := query.Imports
	statementType := &ast.StarExpr{X: imports.AddType(sqlmodel.GoType{Composite: nil, Package: importDatabaseSQL, Name: "Stmt"})}
	values := make([]ast.Expr, 0, len(columns)+1)
	values = append(values, goastutil.CachedIdent(goast.IdentCtx))
	for _, index := range columns {
		values = append(values, goastutil.SelectorExprFrom(goastutil.CachedIdent(identItem), query.Naming.ParameterFields[index]))
	}
	item := &ast.UnaryExpr{Op: token.AND, X: &ast.IndexExpr{
		X: goastutil.CachedIdent(query.Naming.ParamsArgument), Index: goastutil.CachedIdent(identIndex)}}
	appendRow := &ast.FuncLit{
		Type: &ast.FuncType{
			Params: goastutil.FieldList(
				goastutil.Field(identStatement, statementType),
				goastutil.Field(identIndex, goastutil.CachedIdent("int")),
			),
			Results: goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent(goast.IdentError))),
		},
		Body: goastutil.BlockStmt(
			goastutil.DefineStmt(identItem, item),
			goastutil.DefineStmtMulti([]string{goast.IdentBlank, goast.IdentErr},
				goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(identStatement), "ExecContext"), values...)),
			goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentErr)),
		),
	}
	call := goastutil.CallExpr(goastutil.CachedIdent(s.vocabulary.Helper("ClickHouseBatches")),
		goastutil.CachedIdent(goast.IdentCtx), goastutil.CachedIdent(identDB), goastutil.CachedIdent(query.Naming.SQLConstant),
		goastutil.CallExpr(goastutil.CachedIdent("len"), goastutil.CachedIdent(query.Naming.ParamsArgument)),
		goastutil.IntLit(chunk), appendRow)
	offset, failure := s.chunkFailure(query)
	return ownedWriterStatement(query,
		goastutil.IfStmt(
			goastutil.DefineStmtMulti([]string{offset, goast.IdentErr}, call),
			errNotNil(),
			goastutil.BlockStmt(goastutil.ReturnStmt(failure)),
		),
		goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentNil)),
	)
}

// chunkFailure finds the row local and error expression for a failed chunk.
//
// The local is blank when errors have no labels. Otherwise the error is labelled with the
// method's name and the failed chunk's first row.
//
// Takes query (*gogen.Query) which is the query.
//
// Returns string which is the local's name.
// Returns ast.Expr which is the error returned.
func (s *sqlDriver) chunkFailure(query *gogen.Query) (string, ast.Expr) {
	var failure ast.Expr = goastutil.CachedIdent(goast.IdentErr)
	if !s.vocabulary.WrapsErrors() {
		return goast.IdentBlank, failure
	}
	label := &ast.BinaryExpr{
		X:  goastutil.StrLit(s.vocabulary.Message(query.Naming.Method + appendingChunkLabel)),
		Op: token.ADD,
		Y:  goastutil.CallExpr(query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: "strconv", Name: "Itoa"}), goastutil.CachedIdent(identOffset)),
	}
	return identOffset, s.vocabulary.WrapError(label, failure)
}

// batchedParameter reports whether clickhouse-go appends a parameter's values as the
// generated code binds them, with no slice expansion, override or conversion.
//
// Takes query (*gogen.Query) which is the query.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns bool which is true when the driver appends the parameter unchanged.
func batchedParameter(query *gogen.Query, parameter *sqlmodel.QueryParameter) bool {
	if parameter.IsSlice || gogen.OverrideChosen(query.Types, parameter.GoTypeOverride, parameter.SQLType) || chvalues.Appends(&parameter.SQLType) {
		return false
	}
	return batchedType(&parameter.SQLType, 0) && batchedGoType(goast.ResolveGoType(parameter.SQLType, parameter.Nullable, query.Types), 0)
}

// batchedType reports whether clickhouse-go appends values of a SQL type, and of every
// type inside it, unchanged. Integers must be 64 bits at most, temporal types must be a
// date or a timestamp, and map keys must be text or such an integer.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
// Takes depth (int) which bounds the walk.
//
// Returns bool which is true when the driver appends the type's values as they are.
func batchedType(sqlType *sqlmodel.SQLType, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryInteger:
		return sqlType.Width > 0 && sqlType.Width <= batchedIntegerBits
	case sqlmodel.TypeCategoryTemporal:
		return sqlType.Temporal == sqlmodel.TemporalKindDate || sqlType.Temporal == sqlmodel.TemporalKindTimestamp
	case sqlmodel.TypeCategoryArray:
		return batchedType(sqlType.ElementType, depth+1)
	case sqlmodel.TypeCategoryMap:
		return batchedMapKey(sqlType.KeyType) && batchedType(sqlType.ElementType, depth+1)
	default:
		return slices.Contains(batchedCategories, sqlType.Category)
	}
}

// batchedMapKey reports whether clickhouse-go appends a map keyed by a SQL type.
//
// Takes key (*sqlmodel.SQLType) which is the map's key type, or nil.
//
// Returns bool which is true for text or an integer of 64 bits at most.
func batchedMapKey(key *sqlmodel.SQLType) bool {
	if key == nil {
		return false
	}
	return key.Category == sqlmodel.TypeCategoryText || key.Category == sqlmodel.TypeCategoryInteger && key.Width <= batchedIntegerBits
}

// batchedGoType reports whether clickhouse-go appends a type as generated code binds it.
//
// Accepted types include predeclared scalars, time.Time, UUIDs, and pointer, slice, Null,
// or map forms of them. Package-defined types such as enums are rejected.
//
// Takes goType (sqlmodel.GoType) which is the parameter's Go type.
// Takes depth (int) which bounds the walk.
//
// Returns bool which is true when the driver appends the type's values as they are.
func batchedGoType(goType sqlmodel.GoType, depth int) bool {
	if depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	if sqlmodel.IsNullValue(goType) {
		return batchedGoType(goType.Composite.Arguments[0], depth+1)
	}
	if composite := goType.Composite; composite != nil {
		return composite.Kind == sqlmodel.GoCompositeMap && strings.Trim(goType.Name, "*[]") == "" &&
			composite.Key != nil && composite.Value != nil && batchedGoType(*composite.Key, depth+1) && batchedGoType(*composite.Value, depth+1)
	}
	name := strings.TrimLeft(goType.Name, "*[]")
	switch goType.Package {
	case "":
		_, predeclared := types.Universe.Lookup(name).(*types.TypeName)
		return predeclared && name != "error" && name != "any"
	case "time":
		return name == "Time"
	default:
		return goType.Package == batchedUUIDPackage && name == "UUID"
	}
}

// ownedWriterStatement builds the statement that runs a body only when the method's
// writer is a *sql.DB, so the method owns the connections the body writes on.
//
// Takes query (*gogen.Query) which is the query.
// Takes body (...ast.Stmt) which runs when the method owns its writer.
//
// Returns ast.Stmt which is the statement.
func ownedWriterStatement(query *gogen.Query, body ...ast.Stmt) ast.Stmt {
	db := query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: importDatabaseSQL, Name: "DB"})
	writer := goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentQueriesReceiver), sqlConnection.Writer)
	return goastutil.IfStmt(
		goastutil.DefineStmtMulti([]string{identDB, identOwns}, &ast.TypeAssertExpr{X: writer, Type: &ast.StarExpr{X: db}}),
		goastutil.CachedIdent(identOwns),
		goastutil.BlockStmt(body...),
	)
}

// errNotNil builds the test err != nil.
//
// Returns ast.Expr which is the test.
func errNotNil() ast.Expr {
	return &ast.BinaryExpr{X: goastutil.CachedIdent(goast.IdentErr), Op: token.NEQ, Y: goastutil.CachedIdent(goast.IdentNil)}
}

// renderClickHouseBatches renders clickhouse_batches.go, the helpers a method batching
// through clickhouse-go calls.
//
// Takes vocabulary (gogen.Vocabulary) which names the generated package's identifiers.
// Takes packageName (string) which is the generated package's name.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func renderClickHouseBatches(vocabulary gogen.Vocabulary, packageName string) (sqlmodel.GeneratedFile, error) {
	return renderSource(clickHouseBatchesFile, vocabulary, &helpers.Data{Flags: nil, Values: nil, Renames: nil, Package: packageName})
}
