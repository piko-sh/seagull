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
	"go/types"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ScanRows builds a row loop, sharing scan storage for known value types.
//
// Its body reads the scan result and may continue to the next row, return, or yield a
// value. Scratch storage is created only after Next finds the first row.
//
// Takes emission (*gogen.Emission) which holds the package plan, driver and imports.
// Takes query (*gogen.Query) which is the planned query being emitted.
// Takes body (func(Scan, *ast.CallExpr) []ast.Stmt) which builds the loop body from the
// scan plan and rows.Scan call.
//
// Returns ast.Stmt which scans rows with reusable storage when the mapped types allow it.
func ScanRows(emission *gogen.Emission, query *gogen.Query, body func(Scan, *ast.CallExpr) []ast.Stmt) ast.Stmt {
	scan := BuildScan(emission, query)
	var setup, reset []ast.Stmt
	reusable := reusableScan(emission, query, scan)
	if hasEmbeddedColumns(query.Analysed) {
		if planned, declarations, clearing, ok := reusableEmbedScan(emission, query); ok {
			scan, setup, reset, reusable = planned, declarations, clearing, true
		}
	}
	next := func() *ast.CallExpr {
		return goastutil.CallExpr(goastutil.SelectorExpr(goast.IdentRows, "Next"))
	}
	call := goastutil.CallExpr(goastutil.SelectorExpr(goast.IdentRows, "Scan"), scan.Arguments...)
	row := goastutil.VarDecl(goast.IdentRow, goastutil.CachedIdent(query.Naming.Row))
	if !reusable {
		allocations, written := BuildEmbedPreAllocStatements(query), body(scan, call)
		statements := make([]ast.Stmt, 0, 1+len(allocations)+len(scan.Declarations)+len(written))
		statements = append(statements, row)
		statements = append(statements, allocations...)
		statements = append(statements, scan.Declarations...)
		statements = append(statements, written...)
		return &ast.ForStmt{Cond: next(), Body: goastutil.BlockStmt(statements...)}
	}
	destinations := goastutil.DefineStmt("scanArgs", goastutil.CompositeLit(
		&ast.ArrayType{Elt: goastutil.CachedIdent("any")}, scan.Arguments...))
	call.Args = []ast.Expr{goastutil.CachedIdent("scanArgs")}
	call.Ellipsis = token.Pos(1)
	written := body(scan, call)
	statements := make([]ast.Stmt, 0, 1+len(reset)+len(written))
	statements = append(statements, goastutil.AssignStmt(goastutil.CachedIdent(goast.IdentRow),
		goastutil.CompositeLit(goastutil.CachedIdent(query.Naming.Row))))
	statements = append(statements, reset...)
	statements = append(statements, written...)
	loop := &ast.ForStmt{
		Init: goastutil.DefineStmt("more", goastutil.CachedIdent("true")),
		Cond: goastutil.CachedIdent("more"),
		Post: goastutil.AssignStmt(goastutil.CachedIdent("more"), next()),
		Body: goastutil.BlockStmt(statements...),
	}
	prepared := append([]ast.Stmt{row}, setup...)
	prepared = append(prepared, destinations, loop)
	return &ast.IfStmt{Cond: next(), Body: goastutil.BlockStmt(prepared...)}
}

// reusableEmbedScan keeps supported nullable embed fields in independent scratch storage.
// Returned embeds are allocated only for present rows, copying the fields rather than
// retaining an address into the storage used by the next scan.
//
// Takes emission (*gogen.Emission) which holds the package plan, driver and imports.
// Takes query (*gogen.Query) which is the planned query being emitted.
//
// Returns scan (Scan) which describes reusable destinations and embed completion.
// Returns setup ([]ast.Stmt) which declares scratch storage before the scan loop.
// Returns reset ([]ast.Stmt) which clears scratch fields before each row.
// Returns reusable (bool) which reports whether every embed can safely reuse storage.
func reusableEmbedScan(emission *gogen.Emission, query *gogen.Query) (scan Scan, setup, reset []ast.Stmt, reusable bool) {
	groups := embedGroups(query)
	byColumn := embedColumns(groups)
	if !reusableEmbedColumns(emission, query, byColumn) {
		return Scan{}, nil, nil, false
	}
	var fields []*ast.Field
	var finish []ast.Stmt
	for _, group := range groups {
		if !group.IsOuter {
			continue
		}
		sentinel, found := nilableSentinel(query, group.Columns)
		if !found || !strings.HasPrefix(ColumnGoType(query, sentinel).Name, "*") {
			return Scan{}, nil, nil, false
		}
		fields = append(fields, goastutil.Field(group.naming.Field, goastutil.CachedIdent(group.naming.Type)))
		holder := goastutil.SelectorExpr("embedRows", group.naming.Field)
		values := embedCopies(query, group, holder)
		finish = append(finish, &ast.IfStmt{
			Cond: &ast.BinaryExpr{X: goastutil.SelectorExprFrom(holder, query.Naming.ColumnFields[sentinel]), Op: token.NEQ, Y: goastutil.CachedIdent(goast.IdentNil)},
			Body: goastutil.BlockStmt(goastutil.AssignStmt(goastutil.SelectorExpr(goast.IdentRow, group.naming.Field),
				goastutil.AddressExpr(goastutil.CompositeLit(goastutil.CachedIdent(group.naming.Type), values...)))),
		})
	}
	scan = buildScan(emission, query, func(index int) ast.Expr {
		holder := ast.Expr(goastutil.CachedIdent(goast.IdentRow))
		if group := byColumn[index]; group != nil {
			if group.IsOuter {
				holder = goastutil.CachedIdent("embedRows")
			}
			holder = goastutil.SelectorExprFrom(holder, group.naming.Field)
		}
		return goastutil.SelectorExprFrom(holder, query.Naming.ColumnFields[index])
	})
	if len(scan.Declarations)+len(scan.Conversions)+len(scan.Checked) != 0 {
		return Scan{}, nil, nil, false
	}
	scan.Conversions, scan.embedsFinal = finish, true
	if len(fields) == 0 {
		return scan, nil, nil, true
	}
	scratch := &ast.StructType{Fields: goastutil.FieldList(fields...)}
	return scan, []ast.Stmt{goastutil.VarDecl("embedRows", scratch)},
		[]ast.Stmt{goastutil.AssignStmt(goastutil.CachedIdent("embedRows"), goastutil.CompositeLit(scratch))}, true
}

// reusableScan accepts only destinations whose values cannot retain pointers into scan
// storage. Unknown mappings and conversion adapters keep fresh storage per row.
//
// Takes emission (*gogen.Emission) which holds the package plan, driver and imports.
// Takes query (*gogen.Query) which is the planned query being emitted.
// Takes scan (Scan) which is the proposed row scan plan.
//
// Returns bool which reports whether scan destinations can be reused across rows.
func reusableScan(emission *gogen.Emission, query *gogen.Query, scan Scan) bool {
	if len(scan.Declarations)+len(scan.Conversions)+len(scan.Checked) != 0 {
		return false
	}
	for index := range query.Analysed.OutputColumns {
		column := &query.Analysed.OutputColumns[index]
		_, wrapped := query.Rendered.Marks.Wrapped[index]
		if column.IsEmbedded || column.GoTypeOverride != nil || wrapped || emission.ReadsOverride(column) || emission.ConvertsColumn(column, wrapped) {
			return false
		}
		enum := emission.Driver.Enums.Scanned(column, query.Types)
		if enum.Shape == gogen.EnumValue && !enum.Pointer {
			continue
		}
		if !reusableValueType(ColumnGoType(query, index)) {
			return false
		}
	}
	return true
}

// reusableValueType lists scalar mappings whose Scan implementations retain no references
// to their receivers. Pointer and collection mappings are excluded.
//
// Takes value (sqlmodel.GoType) which is the mapped Go type to check.
//
// Returns bool which reports whether the known type can safely reuse its scan
// destination.
func reusableValueType(value sqlmodel.GoType) bool {
	if value.Composite != nil {
		return false
	}
	switch value.Package {
	case "":
		if object := types.Universe.Lookup(value.Name); object != nil {
			basic, ok := object.Type().(*types.Basic)
			return ok && basic.Info()&(types.IsBoolean|types.IsInteger|types.IsFloat|types.IsString) != 0
		}
	case "time":
		return value.Name == "Time" || value.Name == "Duration"
	case "database/sql":
		switch value.Name {
		case "NullBool", "NullByte", "NullFloat64", "NullInt16", "NullInt32", "NullInt64", "NullString", "NullTime":
			return true
		}
	}
	return false
}

// reusableEmbedColumns checks every mapping before sharing destinations.
//
// Takes emission (*gogen.Emission) which holds the package plan, driver and imports.
// Takes query (*gogen.Query) which is the planned query being emitted.
// Takes byColumn (map[int]*embedGroup) which maps output column indexes to their embed
// groups.
//
// Returns bool which reports whether every output column permits reusable storage.
func reusableEmbedColumns(emission *gogen.Emission, query *gogen.Query, byColumn map[int]*embedGroup) bool {
	for index := range query.Analysed.OutputColumns {
		column := &query.Analysed.OutputColumns[index]
		_, wrapped := query.Rendered.Marks.Wrapped[index]
		if column.GoTypeOverride != nil || wrapped || emission.ReadsOverride(column) || emission.ConvertsColumn(column, wrapped) {
			return false
		}
		value := ColumnGoType(query, index)
		if group := byColumn[index]; group != nil && group.IsOuter {
			value.Name = strings.TrimPrefix(value.Name, "*")
		}
		if !reusableValueType(value) {
			return false
		}
	}

	return true
}

// embedCopies builds field values that retain no address into the reusable outer struct.
//
// Takes query (*gogen.Query) which is the planned query being emitted.
// Takes group (embedGroup) which is the embed whose fields are copied.
// Takes holder (ast.Expr) which reads the scratch struct holding the scanned fields.
//
// Returns []ast.Expr which initialises independent embed fields from the scratch storage.
func embedCopies(query *gogen.Query, group embedGroup, holder ast.Expr) []ast.Expr {
	values := make([]ast.Expr, 0, len(group.Columns))
	for _, column := range group.Columns {
		name := query.Naming.ColumnFields[column]
		values = append(values, goastutil.KeyValueIdent(name, goastutil.SelectorExprFrom(holder, name)))
	}
	return values
}

// embedColumns indexes the planned embed group of each output column.
//
// Takes groups ([]embedGroup) which holds the query's embed groups.
//
// Returns map[int]*embedGroup which maps each embedded column index to its group.
func embedColumns(groups []embedGroup) map[int]*embedGroup {
	byColumn := make(map[int]*embedGroup)
	for index := range groups {
		for _, column := range groups[index].Columns {
			byColumn[column] = &groups[index]
		}
	}

	return byColumn
}
