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
	"strconv"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Scan holds scan destinations, raw locals and conversions for one result row.
type Scan struct {
	// Declarations allocates temporary destinations needed before scanning.
	Declarations []ast.Stmt

	// Arguments holds pointers passed to rows.Scan in output order.
	Arguments []ast.Expr

	// Conversions assigns successfully scanned temporary values to the row.
	Conversions []ast.Stmt

	// Checked holds conversions that may return an error after scanning.
	Checked []ast.Expr

	// embedsFinal reports whether conversions already finish nullable embeds.
	embedsFinal bool
}

// CheckedConversions returns the statements running a scan's checked conversions, each as
// `if err := conversion; err != nil { failure }`, to follow the scan's own check.
//
// Takes failure (...ast.Stmt) which is what the method does when a conversion fails, as
// it does when the scan fails, such as `return nil, err`.
//
// Returns []ast.Stmt which holds the statements, or nil when there are no conversions.
func (scan Scan) CheckedConversions(failure ...ast.Stmt) []ast.Stmt {
	if len(scan.Checked) == 0 {
		return nil
	}
	statements := make([]ast.Stmt, 0, len(scan.Checked))
	for _, conversion := range scan.Checked {
		statements = append(statements, goastutil.IfStmt(
			goastutil.DefineStmt(goast.IdentErr, conversion),
			errorIsNotNil(),
			goastutil.BlockStmt(failure...),
		))
	}
	return statements
}

// rawField is one field of the raw local: the strings, or the driver's own Go values, a
// column is scanned into before they are converted into the column's target.
type rawField struct {
	// target is the field or local the column is read into.
	target ast.Expr

	// raw is the Go type the driver fills for a column it reads raw, or nil for a column
	// read through Go's own strings.
	raw *sqlmodel.GoType

	// name is the field's name.
	name string

	// holding says how the column's Go type holds the enum type it converts to.
	holding gogen.EnumHolding
}

// BuildMethodParams constructs the parameter field list of a query method: the context,
// then the query's one parameter as an argument of its own, or its Params struct, as the
// naming plan names them.
//
// Takes query (*gogen.Query) which is the query.
//
// Returns *ast.FieldList which is the parameter list.
func BuildMethodParams(query *gogen.Query) *ast.FieldList {
	arguments := BuildArgumentParams(query).List
	fields := make([]*ast.Field, 0, 1+len(arguments))
	fields = append(fields, goastutil.Field(goast.IdentCtx, goastutil.SelectorExpr("context", "Context")))
	return goastutil.FieldList(append(fields, arguments...)...)
}

// BuildArgumentParams constructs the parameter field list of the arguments a query takes,
// without a context: the query's one parameter as an argument of its own, or its Params
// struct, as the naming plan names them, or none.
//
// Takes query (*gogen.Query) which is the query.
//
// Returns *ast.FieldList which is the parameter list.
func BuildArgumentParams(query *gogen.Query) *ast.FieldList {
	switch {
	case len(query.Analysed.Parameters) == 0:
		return goastutil.FieldList()
	case query.Naming.Params == "":
		return goastutil.FieldList(goastutil.Field(query.Naming.ParameterFields[0], ParameterType(query, &query.Analysed.Parameters[0])))
	default:
		return goastutil.FieldList(goastutil.Field(query.Naming.ParamsArgument, goastutil.CachedIdent(query.Naming.Params)))
	}
}

// BuildScan builds scan destinations and conversions for a generated result row.
//
// A NOT NULL enum value scans through a conversion of its field's address, such as
// (*string)(&row.Mood); a nullable one, or a slice, scans into a field of the raw local
// and is converted into its field after the scan.
//
// Takes emission (*gogen.Emission) which names the scan adapter and says which enums the
// driver fills only as strings.
// Takes query (*gogen.Query) which holds the columns, their names and the wrapped
// columns.
//
// Returns Scan which reads the row.
func BuildScan(emission *gogen.Emission, query *gogen.Query) Scan {
	columns := query.Analysed.OutputColumns
	embedFields := make(map[string]string, len(query.Naming.Embeds))
	for _, embed := range query.Naming.Embeds {
		embedFields[embed.Group] = embed.Field
	}
	return buildScan(emission, query, func(index int) ast.Expr {
		holder := ast.Expr(goastutil.CachedIdent(goast.IdentRow))
		if columns[index].IsEmbedded {
			holder = goastutil.SelectorExprFrom(holder, embedFields[embedGroupName(&columns[index])])
		}
		return goastutil.SelectorExprFrom(holder, query.Naming.ColumnFields[index])
	})
}

// BuildEmbedPreAllocStatements generates allocation statements for outer-join embed
// pointers before scanning (e.g. row.User = &GetOrderUser{}).
//
// Takes query (*gogen.Query) which defines the output columns and their names.
//
// Returns []ast.Stmt which contains the allocation statements, or nil when there are no
// outer-join embeds.
func BuildEmbedPreAllocStatements(query *gogen.Query) []ast.Stmt {
	if !hasEmbeddedColumns(query.Analysed) {
		return nil
	}
	var statements []ast.Stmt
	for _, group := range embedGroups(query) {
		if !group.IsOuter {
			continue
		}
		statements = append(statements,
			goastutil.AssignStmt(
				goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentRow), group.naming.Field),
				goastutil.AddressExpr(goastutil.CompositeLit(goastutil.CachedIdent(group.naming.Type))),
			),
		)
	}
	return statements
}

// BuildEmbedNilCheckStatements generates nil checks for outer-join embeds after scanning.
//
// When the sentinel column of the embed scanned as nil, the embed pointer is set to nil
// (e.g. if row.User.ID == nil then row.User = nil).
//
// The sentinel must have a nilable Go type for the generated `field == nil` comparison to
// compile. Selection uses columnFieldIsNilable, which assumes nullable columns use
// nilable Go types and checks the spelling of overrides for non-null columns. A nullable:
// false override can therefore cause a column to be skipped as a sentinel. A group with
// no nilable column emits no nil check (the pre-allocated embed pointer stays non-nil).
//
// Takes query (*gogen.Query) which defines the output columns and their names.
//
// Returns []ast.Stmt which contains the nil-check statements, or nil when there are no
// outer-join embeds.
func BuildEmbedNilCheckStatements(query *gogen.Query) []ast.Stmt {
	if !hasEmbeddedColumns(query.Analysed) {
		return nil
	}
	var statements []ast.Stmt
	for _, group := range embedGroups(query) {
		if !group.IsOuter || len(group.Columns) == 0 {
			continue
		}
		sentinel, found := nilableSentinel(query, group.Columns)
		if !found {
			continue
		}
		embed := goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentRow), group.naming.Field)
		statements = append(statements,
			&ast.IfStmt{
				Cond: &ast.BinaryExpr{
					X:  goastutil.SelectorExprFrom(embed, query.Naming.ColumnFields[sentinel]),
					Op: token.EQL,
					Y:  goastutil.CachedIdent(goast.IdentNil),
				},
				Body: goastutil.BlockStmt(
					goastutil.AssignStmt(
						goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentRow), group.naming.Field),
						goastutil.CachedIdent(goast.IdentNil),
					),
				),
			},
		)
	}
	return statements
}

// BuildErrCheck constructs an if err != nil { return ..., err } statement.
//
// Takes zeroValues (...ast.Expr) which are the zero values to return alongside the error.
//
// Returns *ast.IfStmt which is the error check statement.
func BuildErrCheck(zeroValues ...ast.Expr) *ast.IfStmt {
	returnValues := make([]ast.Expr, 0, len(zeroValues)+1)
	returnValues = append(returnValues, zeroValues...)
	returnValues = append(returnValues, goastutil.CachedIdent(goast.IdentErr))

	return &ast.IfStmt{
		Cond: &ast.BinaryExpr{
			X:  goastutil.CachedIdent(goast.IdentErr),
			Op: token.NEQ,
			Y:  goastutil.CachedIdent(goast.IdentNil),
		},
		Body: goastutil.BlockStmt(
			goastutil.ReturnStmt(returnValues...),
		),
	}
}

// buildScan constructs how a row is read into targets.
//
// A column the query's SQL wraps in the dialect's to-JSON function scans through the JSON
// scan adapter, a column the driver reads converted through the driver's converting scan
// adapter, since its SQL is not wrapped, and a column whose Go type an override chose
// through the driver's override scan adapter where the driver has one.
//
// Takes emission (*gogen.Emission) which names the scan adapter and says which enums the
// driver fills only as strings.
// Takes query (*gogen.Query) which holds the columns and their names.
// Takes target (func(index int) ast.Expr) which reads the field or local a column is read
// into.
//
// Returns Scan which reads the row.
func buildScan(emission *gogen.Emission, query *gogen.Query, target func(index int) ast.Expr) Scan {
	columns := query.Analysed.OutputColumns
	scan := Scan{Declarations: nil, Arguments: make([]ast.Expr, 0, len(columns)), Conversions: nil, Checked: nil, embedsFinal: false}
	var raws []rawField
	taken := make(map[string]bool)
	for index := range columns {
		destination := target(index)
		kind, wrapped := query.Rendered.Marks.Wrapped[index]
		holding := emission.Driver.Enums.Scanned(&columns[index], query.Types)
		switch {
		case emission.ReadsRaw(&columns[index]):
			field := rawField{target: destination, raw: new(emission.RawType(columns[index].SQLType)), name: unclaimed(query.Naming.ColumnFields[index], taken), holding: holding}
			raws = append(raws, field)
			scan.Arguments = append(scan.Arguments, goastutil.AddressExpr(rawSelector(field.name)))
		case emission.ConvertsColumn(&columns[index], wrapped):
			scan.Arguments = append(scan.Arguments, emission.ConvertingScan(goastutil.AddressExpr(destination)))
		case wrapped && kind == emitkit.ColumnWrapJSON:
			scan.Arguments = append(scan.Arguments, emission.Vocabulary.ScanIntoCall(goastutil.AddressExpr(destination), query.Imports))
		case holding.Shape == gogen.EnumValue && !holding.Pointer:
			address := holding
			address.Pointer = true
			scan.Arguments = append(scan.Arguments, address.Strings(goastutil.AddressExpr(destination), emission.Vocabulary))
		case holding.Shape != 0:
			field := rawField{target: destination, raw: nil, name: unclaimed(query.Naming.ColumnFields[index], taken), holding: holding}
			raws = append(raws, field)
			scan.Arguments = append(scan.Arguments, goastutil.AddressExpr(rawSelector(field.name)))
		case emission.ReadsOverride(&columns[index]):
			scan.Arguments = append(scan.Arguments, emission.OverrideScan(goastutil.AddressExpr(destination), &columns[index]))
		default:
			scan.Arguments = append(scan.Arguments, goastutil.AddressExpr(destination))
		}
	}
	if len(raws) == 0 {
		return scan
	}
	fields := make([]*ast.Field, len(raws))
	for index, field := range raws {
		if field.raw != nil {
			fields[index] = goastutil.Field(field.name, query.Imports.AddType(*field.raw))
			scan.Checked = append(scan.Checked, emission.ConvertRaw(goastutil.AddressExpr(field.target), rawSelector(field.name)))
			continue
		}
		fields[index] = goastutil.Field(field.name, field.holding.StringType(query.Imports))
		scan.Conversions = append(scan.Conversions, goastutil.AssignStmt(field.target, field.holding.FromStrings(rawSelector(field.name), emission.Vocabulary)))
	}
	scan.Declarations = []ast.Stmt{goastutil.VarDecl(goast.IdentRaw, &ast.StructType{Fields: goastutil.FieldList(fields...)})}
	return scan
}

// rawSelector returns the expression reading a field of the raw local.
//
// Takes field (string) which is the field's name.
//
// Returns ast.Expr which reads it.
func rawSelector(field string) ast.Expr {
	return goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentRaw), field)
}

// unclaimed returns a name no other field of the raw local has: the name, or the name
// with the first number from 2 that makes it unique, and claims it.
//
// Takes name (string) which is the name wanted.
// Takes taken (map[string]bool) which holds the names claimed so far.
//
// Returns string which is the name claimed.
func unclaimed(name string, taken map[string]bool) string {
	claimed := name
	for number := 2; taken[claimed]; number++ {
		claimed = name + strconv.Itoa(number)
	}
	taken[claimed] = true
	return claimed
}

// nilableSentinel returns the first of a group's columns whose field can be compared
// against nil.
//
// Takes query (*gogen.Query) which holds the columns.
// Takes columns ([]int) which are the indices of the group's columns, in order.
//
// Returns int which is the index of the sentinel column.
// Returns bool which is true when a nilable column was found.
func nilableSentinel(query *gogen.Query, columns []int) (int, bool) {
	for _, column := range columns {
		if columnFieldIsNilable(&query.Analysed.OutputColumns[column]) {
			return column, true
		}
	}
	return 0, false
}

// columnFieldIsNilable identifies columns to use in an embed's nil check.
//
// It assumes nullable columns use nilable Go types and otherwise checks the spelling of
// an explicit Go type override. It does not inspect the query's type mapping table.
//
// Takes column (*sqlmodel.OutputColumn) which is the column to inspect.
//
// Returns bool which is true when the column's field can be compared against nil.
func columnFieldIsNilable(column *sqlmodel.OutputColumn) bool {
	if column.Nullable {
		return true
	}
	if column.GoTypeOverride != nil {
		return goTypeNameIsNilable(column.GoTypeOverride.Name)
	}
	return false
}

// goTypeNameIsNilable reports whether a Go type name denotes a nil-comparable type.
//
// Takes name (string) which is the Go type name (for example "*string" or "[]byte").
//
// Returns bool which is true when a value of the type may be compared against nil.
func goTypeNameIsNilable(name string) bool {
	switch {
	case name == "any":
		return true
	case strings.HasPrefix(name, "*"):
		return true
	case strings.HasPrefix(name, "[]"):
		return true
	case strings.HasPrefix(name, "map["):
		return true
	case strings.HasPrefix(name, "chan "):
		return true
	case strings.HasPrefix(name, "func("):
		return true
	case strings.HasPrefix(name, "interface{"):
		return true
	default:
		return false
	}
}
