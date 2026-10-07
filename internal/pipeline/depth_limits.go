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

package pipeline

import (
	"fmt"
	"maps"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

// depthLimitDiagnostics warns about types too deeply nested for Go type mapping, and
// about the Go type overrides of columns and parameters that nest too deeply for the
// generated code to follow.
//
// Parts of a SQL type beyond the limit become unknown types and may map to any; parts of
// a Go type beyond it keep their import path.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables the models come from.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the analysed queries.
// Takes overrides ([]sqlmodel.TypeOverride) which are the configured type overrides.
//
// Returns []diag.SourceError which holds one warning per type, or nil.
func depthLimitDiagnostics(
	catalogue *sqlmodel.Catalogue,
	queries []*sqlmodel.AnalysedQuery,
	overrides []sqlmodel.TypeOverride,
) []diag.SourceError {
	var diagnostics []diag.SourceError
	for _, override := range overrides {
		if override.Column == "" && gotypes.OverrideTooDeep(override) {
			diagnostics = append(diagnostics, diagbuild.Unlocated(diag.CodeDepthLimit, diag.SeverityWarning,
				fmt.Sprintf("type override %q names more array dimensions than the type mapping follows", override.SQLTypeName)))
		}
	}
	diagnostics = append(diagnostics, tableDepthDiagnostics(catalogue)...)
	for _, query := range queries {
		diagnostics = append(diagnostics, queryDepthDiagnostics(query)...)
	}
	return diagnostics
}

// tableDepthDiagnostics reports each table column whose type nests too deeply to map, and
// each table or view column whose Go type override nests too deeply to follow.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables; nil holds none, and a nil
// schema, table or view entry is passed over.
//
// Returns []diag.SourceError which holds one warning per column, at the migration that
// created its table or view.
func tableDepthDiagnostics(catalogue *sqlmodel.Catalogue) []diag.SourceError {
	if catalogue == nil {
		return nil
	}
	var diagnostics []diag.SourceError
	for _, schemaName := range slices.Sorted(maps.Keys(catalogue.Schemas)) {
		diagnostics = append(diagnostics, schemaDepthDiagnostics(catalogue.Schemas[schemaName])...)
	}
	return diagnostics
}

// schemaDepthDiagnostics reports the columns of one schema's tables and views as
// tableDepthDiagnostics describes, in name order.
//
// Takes schema (*sqlmodel.Schema) which holds the tables and views; nil, and a nil table
// or view entry, is passed over.
//
// Returns []diag.SourceError which holds one warning per column.
func schemaDepthDiagnostics(schema *sqlmodel.Schema) []diag.SourceError {
	if schema == nil {
		return nil
	}
	var diagnostics []diag.SourceError
	for _, tableName := range slices.Sorted(maps.Keys(schema.Tables)) {
		table := schema.Tables[tableName]
		if table == nil {
			continue
		}
		for index := range table.Columns {
			if gotypes.TypeTooDeep(new(table.Columns[index].ValueType())) {
				diagnostics = append(diagnostics, diagbuild.Block(table.Origin.Filename, 1, diag.CodeDepthLimit, diag.SeverityWarning,
					fmt.Sprintf("column %s.%s has a type nested more deeply than the type mapping follows", table.Name, table.Columns[index].Name)))
			}
		}
		diagnostics = append(diagnostics, overrideDepthDiagnostics(table.Origin.Filename, table.Name, table.Columns)...)
	}
	for _, viewName := range slices.Sorted(maps.Keys(schema.Views)) {
		if view := schema.Views[viewName]; view != nil {
			diagnostics = append(diagnostics, overrideDepthDiagnostics(view.Origin.Filename, view.Name, view.Columns)...)
		}
	}
	return diagnostics
}

// overrideDepthDiagnostics reports each column of a table or view whose Go type override
// nests too deeply for the generated code to follow.
//
// Takes filename (string) which is the migration that created the table or view.
// Takes relation (string) which is the table's or view's name.
// Takes columns ([]sqlmodel.Column) which are its columns.
//
// Returns []diag.SourceError which holds one warning per column, at the migration.
func overrideDepthDiagnostics(filename, relation string, columns []sqlmodel.Column) []diag.SourceError {
	var diagnostics []diag.SourceError
	for index := range columns {
		if goTypeTooDeep(columns[index].GoTypeOverride) {
			diagnostics = append(diagnostics, diagbuild.Block(filename, 1, diag.CodeDepthLimit, diag.SeverityWarning,
				fmt.Sprintf("column %s.%s has a Go type override nested more deeply than the generated code follows", relation, columns[index].Name)))
		}
	}
	return diagnostics
}

// queryDepthDiagnostics reports each output column and parameter of a query whose Go type
// override nests too deeply to follow, or whose type nests too deeply to map.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []diag.SourceError which holds up to two warnings per column or parameter, at
// the query.
func queryDepthDiagnostics(query *sqlmodel.AnalysedQuery) []diag.SourceError {
	var diagnostics []diag.SourceError
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		diagnostics = appendValueDepthDiagnostics(diagnostics, query, "output column", column.Name, &column.SQLType, column.GoTypeOverride)
	}
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		diagnostics = appendValueDepthDiagnostics(diagnostics, query, "parameter", parameter.Name, &parameter.SQLType, parameter.GoTypeOverride)
	}
	return diagnostics
}

// appendValueDepthDiagnostics reports one output column or parameter of a query whose Go
// type override nests too deeply to follow, or whose type nests too deeply to map.
//
// Takes diagnostics ([]diag.SourceError) which the warnings are appended to.
// Takes query (*sqlmodel.AnalysedQuery) which is the query the value belongs to.
// Takes kind (string) which names the kind of value, such as "parameter".
// Takes name (string) which is the value's name.
// Takes sqlType (*sqlmodel.SQLType) which is the value's type.
// Takes override (*sqlmodel.GoType) which is the value's Go type override, or nil.
//
// Returns []diag.SourceError which holds diagnostics and a warning for each, at the
// query.
func appendValueDepthDiagnostics(
	diagnostics []diag.SourceError,
	query *sqlmodel.AnalysedQuery,
	kind, name string,
	sqlType *sqlmodel.SQLType,
	override *sqlmodel.GoType,
) []diag.SourceError {
	if goTypeTooDeep(override) {
		diagnostics = append(diagnostics, diagbuild.Block(query.Filename, query.Line, diag.CodeDepthLimit, diag.SeverityWarning,
			fmt.Sprintf("%s %q of query %q has a Go type override nested more deeply than the generated code follows", kind, name, query.Name)))
	}
	if gotypes.TypeTooDeep(sqlType) {
		diagnostics = append(diagnostics, diagbuild.Block(query.Filename, query.Line, diag.CodeDepthLimit, diag.SeverityWarning,
			fmt.Sprintf("%s %q of query %q has a type nested more deeply than the type mapping follows", kind, name, query.Name)))
	}
	return diagnostics
}
