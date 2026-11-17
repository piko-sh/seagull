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
	"slices"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// duckDBAppendingFile is the generated file holding the DuckDB appender helpers.
	duckDBAppendingFile = "duckdb_appending.go"

	// importDatabaseSQL is the import path of database/sql.
	importDatabaseSQL = "database/sql"

	// importDriver is the import path of database/sql/driver.
	importDriver = "database/sql/driver"

	// identDB is the local holding the method's writer as a *sql.DB.
	identDB = "db"

	// identAppender is the local holding the DuckDB appender a row is appended to.
	identAppender = "appender"

	// identChecker is the local holding the driver's check of an argument.
	identChecker = "checker"

	// identConn is the local holding the driver's connection.
	identConn = "conn"

	// identIndex is the local holding the index of the row appended.
	identIndex = "index"

	// identItem is the local pointing to the row appended.
	identItem = "item"

	// identOffset is the local holding the index of the first row of the chunk that failed.
	identOffset = "offset"

	// identOwns is the local reporting that the method's writer is a *sql.DB.
	identOwns = "owns"

	// identAppended is the local reporting that the appender wrote the rows.
	identAppended = "appended"

	// appendingChunkLabel follows a batch method's name in the label of a failed chunk's
	// error, before the index of the row the chunk starts at, as a statement's is labelled.
	appendingChunkLabel = ": the statement starting at row "
)

var (
	// appendingHelpers lists the suffixes of the helpers duckdb_appending.go declares.
	appendingHelpers = []string{"DuckDBAppend", "DuckDBAppendChunk", "DuckDBAppendRow", "DuckDBAppender", "DuckDBContextRows"}

	// appendedNumbers gives the width in bits of each Go number type a batch appends. It is
	// a list rather than a map because its keys are Go type names, which read like engine
	// type names to the neutrality check.
	appendedNumbers = []appendedNumber{
		{name: "int", bits: 64}, {name: "int8", bits: 8}, {name: "int16", bits: 16}, {name: "int32", bits: 32},
		{name: "int64", bits: 64}, {name: "uint8", bits: 8}, {name: "uint16", bits: 16}, {name: "uint32", bits: 32},
		{name: "float32", bits: 32}, {name: "float64", bits: 64},
	}
)

// appendedNumber is a Go number type a batch appends and its width in bits.
type appendedNumber struct {
	// name is the Go type's name.
	name string

	// bits is the type's width in bits.
	bits int
}

// appendTarget is the table a batch appends to and the columns its INSERT lists, as the
// catalogue spells them. The appender, unlike SQL, matches them by case.
type appendTarget struct {
	// schema is the table's schema, or empty for the search path's.
	schema string

	// table is the table's name.
	table string

	// columns are the listed columns' names, in the INSERT's order.
	columns []string
}

// appendsThroughDuckDB finds a batch or copyfrom method's DuckDB appender target.
//
// The method must own a duckdb-go connection and use a plain INSERT into a catalogue
// table with a column list and no generated columns. Its VALUES row must use each
// parameter once in column order with a type the appender stores as INSERT would.
//
// Takes query (*gogen.Query) which is the query.
// Takes row (*emitkit.Row) which is its VALUES row.
// Takes perRow (bool) which is true when the method sends each row on its own.
//
// Returns *appendTarget which names the table and columns as the catalogue spells them,
// or nil when the method does not append.
func (s *sqlDriver) appendsThroughDuckDB(query *gogen.Query, row *emitkit.Row, perRow bool) *appendTarget {
	analysed := query.Analysed
	if s.profile.module != driverDuckDB || perRow || query.Renderer == nil || len(analysed.InsertColumns) != len(row.Slots) ||
		len(row.Arguments) != len(analysed.Parameters) || !plainValuesRow(query.Rendered.SQL, row, query.Renderer.Form.Prefix) {
		return nil
	}
	target := s.appendTarget(analysed)
	if target == nil {
		return nil
	}
	for slot, argument := range row.Slots {
		if argument != slot || slices.Index(row.Arguments, row.Arguments[slot]) != slot ||
			!s.appendsParameter(query, &analysed.Parameters[row.Arguments[slot]]) {
			return nil
		}
	}
	return target
}

// appendTarget looks up the table an INSERT writes in the catalogue. It refuses a table
// the catalogue does not hold, a qualifier naming a database, and a column list naming a
// column the table lacks, a column twice, or a generated column.
//
// Takes analysed (*sqlmodel.AnalysedQuery) which is the INSERT.
//
// Returns *appendTarget which names the table and columns, or nil.
func (s *sqlDriver) appendTarget(analysed *sqlmodel.AnalysedQuery) *appendTarget {
	insert := analysed.InsertTarget
	if insert.Name == "" || strings.Contains(insert.Schema, ".") || len(analysed.InsertColumns) == 0 {
		return nil
	}
	table := s.catalogue.LookupTable(insert.Schema, insert.Name)
	if table == nil {
		return nil
	}
	target := &appendTarget{schema: "", table: table.Name, columns: make([]string, 0, len(analysed.InsertColumns))}
	if insert.Schema != "" {
		target.schema = table.Schema
	}
	for _, name := range analysed.InsertColumns {
		index := slices.IndexFunc(table.Columns, func(column sqlmodel.Column) bool {
			return s.catalogue.Key(column.Name) == s.catalogue.Key(name)
		})
		if index < 0 || table.Columns[index].IsGenerated || slices.Contains(target.columns, table.Columns[index].Name) {
			return nil
		}
		target.columns = append(target.columns, table.Columns[index].Name)
	}
	return target
}

// appendsParameter reports whether a parameter binds as it is, with a Go type whose
// values DuckDB's appender stores in the column as an INSERT would.
//
// Takes query (*gogen.Query) which is the query.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns bool which is true when the appender takes the parameter.
func (s *sqlDriver) appendsParameter(query *gogen.Query, parameter *sqlmodel.QueryParameter) bool {
	if parameter.IsSlice || parameter.GoTypeOverride != nil {
		return false
	}
	access := goastutil.CachedIdent(identItem)
	if bound := s.profile.enums.Bound(access, query.Analysed, parameter, query.Types, s.vocabulary); bound != ast.Expr(access) {
		return false
	}
	if bound := s.Bind(access, parameter, query.Imports); bound != ast.Expr(access) {
		return false
	}
	goType := goast.ResolveGoType(parameter.SQLType, parameter.Nullable, query.Types)
	if sqlmodel.IsNullValue(goType) {
		goType = goType.Composite.Arguments[0]
	}
	if goType.Composite != nil {
		return false
	}
	return appendedGoType(parameter.SQLType, goType.Package, strings.TrimPrefix(goType.Name, "*"))
}

// appendingStatement builds the first statement for a DuckDB appender method.
//
// A *sql.DB writer appends rows in chunks and returns. A connection without an appender
// continues by sending SQL statements.
//
// Takes query (*gogen.Query) which is the query.
// Takes row (*emitkit.Row) which is its VALUES row.
// Takes target (*appendTarget) which names the table and columns appended to.
// Takes chunk (int) which is how many rows one statement would carry.
//
// Returns ast.Stmt which is the statement.
func (s *sqlDriver) appendingStatement(query *gogen.Query, row *emitkit.Row, target *appendTarget, chunk int) ast.Stmt {
	call := goastutil.CallExpr(goastutil.CachedIdent(s.vocabulary.Helper("DuckDBAppend")),
		goastutil.CachedIdent(goast.IdentCtx), goastutil.CachedIdent(identDB),
		goastutil.CallExpr(goastutil.CachedIdent("len"), goastutil.CachedIdent(query.Naming.ParamsArgument)),
		goastutil.IntLit(chunk), s.appenderOpener(query, target), s.rowAppender(query, row))
	offset, failure := s.chunkFailure(query)
	return ownedWriterStatement(query,
		goastutil.DefineStmtMulti([]string{identAppended, offset, goast.IdentErr}, call),
		goastutil.IfStmt(nil, errNotNil(), goastutil.BlockStmt(goastutil.ReturnStmt(failure))),
		goastutil.IfStmt(nil, goastutil.CachedIdent(identAppended),
			goastutil.BlockStmt(goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentNil)))),
	)
}

// appenderOpener builds the function opening an appender on the INSERT's table and
// columns. The table's defaults fill the columns the INSERT does not list.
//
// Takes query (*gogen.Query) which is the query.
// Takes target (*appendTarget) which names the table and columns appended to.
//
// Returns ast.Expr which is the function.
func (s *sqlDriver) appenderOpener(query *gogen.Query, target *appendTarget) ast.Expr {
	imports := query.Imports
	columns := make([]ast.Expr, len(target.columns))
	for index, column := range target.columns {
		columns[index] = goastutil.StrLit(column)
	}
	open := goastutil.CallExpr(imports.AddType(sqlmodel.GoType{Composite: nil, Package: duckDBModule, Name: "NewAppenderWithColumns"}),
		goastutil.CachedIdent(identConn), goastutil.StrLit(""), goastutil.StrLit(target.schema), goastutil.StrLit(target.table),
		&ast.CompositeLit{Type: &ast.ArrayType{Elt: goastutil.CachedIdent("string")}, Elts: columns})
	return &ast.FuncLit{
		Type: &ast.FuncType{
			Params: goastutil.FieldList(goastutil.Field(identConn, imports.AddType(sqlmodel.GoType{Composite: nil, Package: importDriver, Name: "Conn"}))),
			Results: goastutil.FieldList(
				goastutil.Field("", goastutil.CachedIdent(s.vocabulary.Helper("DuckDBAppender"))),
				goastutil.Field("", goastutil.CachedIdent(goast.IdentError)),
			),
		},
		Body: goastutil.BlockStmt(goastutil.ReturnStmt(open)),
	}
}

// rowAppender builds the function appending the row at an index.
//
// Takes query (*gogen.Query) which is the query.
// Takes row (*emitkit.Row) which is its VALUES row.
//
// Returns ast.Expr which is the function.
func (s *sqlDriver) rowAppender(query *gogen.Query, row *emitkit.Row) ast.Expr {
	values := make([]ast.Expr, 0, len(row.Arguments)+2)
	values = append(values, goastutil.CachedIdent(identAppender), goastutil.CachedIdent(identChecker))
	for _, argument := range row.Arguments {
		values = append(values, goastutil.SelectorExprFrom(goastutil.CachedIdent(identItem), query.Naming.ParameterFields[argument]))
	}
	item := &ast.UnaryExpr{Op: token.AND, X: &ast.IndexExpr{
		X: goastutil.CachedIdent(query.Naming.ParamsArgument), Index: goastutil.CachedIdent(identIndex)}}
	return &ast.FuncLit{
		Type: &ast.FuncType{
			Params: goastutil.FieldList(
				goastutil.Field(identAppender, goastutil.CachedIdent(s.vocabulary.Helper("DuckDBAppender"))),
				goastutil.Field(identChecker, query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: importDriver, Name: "NamedValueChecker"})),
				goastutil.Field(identIndex, goastutil.CachedIdent("int")),
			),
			Results: goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent(goast.IdentError))),
		},
		Body: goastutil.BlockStmt(
			goastutil.DefineStmt(identItem, item),
			goastutil.ReturnStmt(goastutil.CallExpr(goastutil.CachedIdent(s.vocabulary.Helper("DuckDBAppendRow")), values...)),
		),
	}
}

// plainValuesRow reports whether a query's SQL is INSERT INTO a table and its columns,
// then VALUES and the row. Each place in the row must be a placeholder alone, and only
// space or a semicolon may follow the row.
//
// Takes sql (string) which is the query's rendered SQL.
// Takes row (*emitkit.Row) which is its VALUES row.
// Takes prefix (string) which starts each placeholder, which the row's parts end with.
//
// Returns bool which is true for such a statement.
func plainValuesRow(sql string, row *emitkit.Row, prefix string) bool {
	if !plainInsertText(sql, row) || len(row.Parts) != len(row.Slots)+1 {
		return false
	}
	for index, part := range row.Parts {
		want := ","
		switch index {
		case 0:
			want = "("
		case len(row.Parts) - 1:
			want = ")"
		}
		if index < len(row.Slots) {
			part = strings.TrimSuffix(part, prefix)
		}
		if strings.TrimSpace(part) != want {
			return false
		}
	}
	return true
}

// plainInsertText reports whether the text around a query's VALUES row is INSERT INTO a
// table and its columns, then VALUES, and nothing after the row but space or a semicolon.
//
// Takes sql (string) which is the query's rendered SQL.
// Takes row (*emitkit.Row) which is its VALUES row.
//
// Returns bool which is true for such a statement, and false when the row lies outside
// the SQL.
func plainInsertText(sql string, row *emitkit.Row) bool {
	if row.Values.Start < 0 || row.Values.Start > row.Values.End || row.Values.End > len(sql) {
		return false
	}
	statement := strings.ToLower(strings.Join(strings.Fields(sql[:row.Values.Start]), " "))
	return strings.HasPrefix(statement, "insert into ") && strings.HasSuffix(statement, " values") &&
		!strings.Contains(statement, " by name") && !strings.Contains(statement, " by position") &&
		!strings.Contains(statement, " settings ") && strings.Trim(sql[row.Values.End:], " \t\r\n;") == ""
}

// appendedGoType reports whether DuckDB's appender stores a value of a Go type in a
// column of a SQL type as an INSERT would. The appender narrows a number without the
// range check an INSERT's cast makes, so a Go number must fit the column's type.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
// Takes goPackage (string) which is the Go type's package, empty for a built-in type.
// Takes name (string) which is the Go type's name, without a pointer.
//
// Returns bool which is true when the appender stores the value.
func appendedGoType(sqlType sqlmodel.SQLType, goPackage, name string) bool {
	if goPackage == "time" {
		return name == "Time" && sqlType.Category == sqlmodel.TypeCategoryTemporal &&
			(sqlType.Temporal == sqlmodel.TemporalKindDate || sqlType.Temporal == sqlmodel.TemporalKindTimestamp)
	}
	if goPackage != "" {
		return false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat:
		return numberFits(sqlType, name)
	case sqlmodel.TypeCategoryBoolean:
		return name == "bool"
	case sqlmodel.TypeCategoryText:
		return name == "string"
	case sqlmodel.TypeCategoryBinary:
		return name == "[]byte"
	case sqlmodel.TypeCategoryArray:
		element, sliced := strings.CutPrefix(name, "[]")
		return sliced && sqlType.ElementType != nil && appendedElement(*sqlType.ElementType, element)
	default:
		return false
	}
}

// appendedElement reports whether DuckDB's appender stores a slice element of a Go type
// in an array element of a SQL type as an INSERT would.
//
// Takes sqlType (sqlmodel.SQLType) which is the array's element type.
// Takes name (string) which is the Go element type's name.
//
// Returns bool which is true for text, booleans and numbers that fit, held by value.
func appendedElement(sqlType sqlmodel.SQLType, name string) bool {
	switch {
	case strings.HasPrefix(name, "*"):
		return false
	case sqlType.Category == sqlmodel.TypeCategoryText:
		return name == "string"
	case sqlType.Category == sqlmodel.TypeCategoryBoolean:
		return name == "bool"
	default:
		return numberFits(sqlType, name)
	}
}

// numberFits reports whether every value of a Go number type fits a SQL type.
//
// A signed integer needs at least its own width. An unsigned integer needs an unsigned
// type at least as wide or a wider signed type. A float needs at least its own width.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
// Takes name (string) which is the Go type's name.
//
// Returns bool which is true when the Go type fits.
func numberFits(sqlType sqlmodel.SQLType, name string) bool {
	found := slices.IndexFunc(appendedNumbers, func(number appendedNumber) bool { return number.name == name })
	if found < 0 || sqlType.Width == 0 {
		return false
	}
	bits := appendedNumbers[found].bits
	unsigned := strings.HasPrefix(name, "uint")
	switch {
	case sqlType.Category == sqlmodel.TypeCategoryFloat:
		return strings.HasPrefix(name, "float") && bits <= sqlType.Width
	case sqlType.Category != sqlmodel.TypeCategoryInteger || strings.HasPrefix(name, "float"):
		return false
	case sqlType.Unsigned:
		return unsigned && bits <= sqlType.Width
	case unsigned:
		return bits < sqlType.Width
	default:
		return bits <= sqlType.Width
	}
}

// renderDuckDBAppending renders duckdb_appending.go, the helpers a method appending
// through DuckDB's appender calls.
//
// Takes vocabulary (gogen.Vocabulary) which names the generated package's identifiers.
// Takes packageName (string) which is the generated package's name.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source cannot be rendered.
func renderDuckDBAppending(vocabulary gogen.Vocabulary, packageName string) (sqlmodel.GeneratedFile, error) {
	return renderSource(duckDBAppendingFile, vocabulary, &helpers.Data{Flags: nil, Values: nil, Renames: nil, Package: packageName})
}

// chunkRows returns how many rows one multi-row statement of a batch carries, as
// multirow.Method works it out.
//
// Takes limit (int) which is the most arguments one statement binds.
// Takes arguments (int) which is how many arguments a row binds.
//
// Returns int which is the number of rows, at least one.
func chunkRows(limit, arguments int) int {
	return max(limit/max(arguments, 1), 1)
}
