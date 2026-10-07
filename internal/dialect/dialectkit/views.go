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

package dialectkit

import (
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// UndeclaredColumns is what OverlayViewColumnNames does with the projected columns of a
// view's query that its declared column list is too short to name.
type UndeclaredColumns uint8

const (
	// UndeclaredColumnsKept keeps the projected columns past the end of a shorter declared
	// list under their own names, as PostgreSQL does.
	UndeclaredColumnsKept UndeclaredColumns = iota

	// UndeclaredColumnsDropped drops the projected columns past the end of a shorter
	// declared list, so the view has exactly the columns it declares.
	UndeclaredColumnsDropped
)

// ChangingRows returns an analyser that records the kind of row change its statement
// makes, so the analyser can judge a RETURNING clause against the dialect's
// Info.Returning.
//
// Takes change (ast.DataChange) which is the statement's kind.
// Takes analyse (func(P) (*ast.RawQueryAnalysis, error)) which analyses the statement
// with the dialect's parser.
//
// Returns func(P) (*ast.RawQueryAnalysis, error) which analyses the statement and records
// its kind on the analysis it returns.
func ChangingRows[P any](change ast.DataChange, analyse func(P) (*ast.RawQueryAnalysis, error)) func(P) (*ast.RawQueryAnalysis, error) {
	return func(parser P) (*ast.RawQueryAnalysis, error) {
		analysis, err := analyse(parser)
		if analysis != nil {
			analysis.DataChange = change
		}
		return analysis, err
	}
}

// KeepFirst returns the first count items with no spare capacity, so appending to the
// result never writes over the items after them, which another analysis may hold.
//
// Takes items ([]T) which are the items.
// Takes count (int) which is how many to keep; it is held to the number of items.
//
// Returns []T which holds the kept items, or nil when count is not positive.
func KeepFirst[T any](items []T, count int) []T {
	if count <= 0 {
		return nil
	}
	return slices.Clip(items[:min(count, len(items))])
}

// IsStarColumn reports whether a projected column is a star, which stands for columns the
// catalogue knows only once it resolves the star, and so names them by the view's
// declared column list itself.
//
// Takes column (ast.RawOutputColumn) which is the projected column.
//
// Returns bool which is true for a star.
func IsStarColumn(column ast.RawOutputColumn) bool {
	return column.IsStar
}

// ColumnsFromNames builds name-only columns for a view's declared column list, each
// nullable and of unknown type, for when the view's query cannot be typed.
//
// Takes names ([]string) which are the declared column names.
//
// Returns []sqlmodel.Column which holds one column per name, or nil when there are no
// names.
func ColumnsFromNames(names []string) []sqlmodel.Column {
	if len(names) == 0 {
		return nil
	}
	columns := make([]sqlmodel.Column, len(names))
	for index, name := range names {
		columns[index] = sqlmodel.NewColumn(name, sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true)
	}
	return columns
}

// OverlayViewColumnNames renames a view query's projected columns by the view's declared
// column list, in order, keeping each column's expression and source so it is still
// typed. A projected column the list names is no longer a star.
//
// A list longer than the projection adds a name-only column for each name past its end,
// rather than indexing out of bounds; what happens to projected columns past the end of a
// shorter list is the undeclared argument's choice.
//
// Takes analysis (*ast.RawQueryAnalysis) whose output columns are renamed in place; nil
// is left alone.
// Takes names ([]string) which is the declared column list.
// Takes undeclared (UndeclaredColumns) which says whether projected columns the list does
// not name are kept or dropped.
func OverlayViewColumnNames(analysis *ast.RawQueryAnalysis, names []string, undeclared UndeclaredColumns) {
	if analysis == nil {
		return
	}
	for columnIndex, name := range names {
		column := ast.RawOutputColumn{
			Name:       name,
			Expression: nil,
			TableAlias: "",
			ColumnName: "",
			IsStar:     false,
		}
		if columnIndex < len(analysis.OutputColumns) {
			column.Expression = analysis.OutputColumns[columnIndex].Expression
			column.ColumnName = analysis.OutputColumns[columnIndex].ColumnName
			column.TableAlias = analysis.OutputColumns[columnIndex].TableAlias
			analysis.OutputColumns[columnIndex] = column
			continue
		}
		analysis.OutputColumns = append(analysis.OutputColumns, column)
	}
	if undeclared == UndeclaredColumnsDropped && len(names) < len(analysis.OutputColumns) {
		analysis.OutputColumns = analysis.OutputColumns[:len(names)]
	}
}
