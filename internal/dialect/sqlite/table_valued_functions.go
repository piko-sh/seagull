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

package sqlite

import (
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// moduleFTS5 is the module name of an FTS5 full-text virtual table.
	moduleFTS5 = "fts5"
)

var (
	// jsonTableColumns describes the output schema shared by SQLite's json_each and
	// json_tree table-valued functions.
	jsonTableColumns = []ast.ScopedColumn{
		{Name: "key", SQLType: tvfSQLType("text", sqlmodel.TypeCategoryText), Nullable: true},
		{Name: "value", SQLType: tvfSQLType("text", sqlmodel.TypeCategoryText), Nullable: true},
		{Name: "type", SQLType: tvfSQLType("text", sqlmodel.TypeCategoryText), Nullable: true},
		{Name: "atom", SQLType: tvfSQLType("text", sqlmodel.TypeCategoryText), Nullable: true},
		{Name: "id", SQLType: tvfSQLType("integer", sqlmodel.TypeCategoryInteger), Nullable: true},
		{Name: "parent", SQLType: tvfSQLType("integer", sqlmodel.TypeCategoryInteger), Nullable: true},
		{Name: "fullkey", SQLType: tvfSQLType("text", sqlmodel.TypeCategoryText), Nullable: true},
		{Name: "path", SQLType: tvfSQLType("text", sqlmodel.TypeCategoryText), Nullable: true},
	}

	// schemaTableShape describes the schema table SQLite keeps for every database: one row
	// per table, index, view and trigger, with the SQL that created it.
	schemaTableShape = []ast.ScopedColumn{
		textColumn("type", false),
		textColumn("name", false),
		textColumn("tbl_name", false),
		integerColumn("rootpage", false),
		textColumn("sql", true),
	}

	// schemaTableColumns maps the names SQLite gives its schema tables, for the main and
	// temporary databases and under their legacy names, to their columns. No migration
	// creates them, so the analyser reads them as built-in table-valued sources.
	schemaTableColumns = map[string][]ast.ScopedColumn{
		"sqlite_schema":      schemaTableShape,
		"sqlite_master":      schemaTableShape,
		"sqlite_temp_schema": schemaTableShape,
		"sqlite_temp_master": schemaTableShape,
	}

	// pragmaFunctionColumns maps SQLite's pragma table-valued functions, such as
	// pragma_table_info, to their visible columns.
	pragmaFunctionColumns = map[string][]ast.ScopedColumn{
		"pragma_table_info": {
			integerColumn("cid", false), textColumn("name", false), textColumn("type", false),
			integerColumn("notnull", false), textColumn("dflt_value", true), integerColumn("pk", false),
		},
		"pragma_table_xinfo": {
			integerColumn("cid", false), textColumn("name", false), textColumn("type", false),
			integerColumn("notnull", false), textColumn("dflt_value", true), integerColumn("pk", false),
			integerColumn("hidden", false),
		},
		"pragma_table_list": {
			textColumn("schema", false), textColumn("name", false), textColumn("type", false),
			integerColumn("ncol", false), integerColumn("wr", false), integerColumn("strict", false),
		},
		"pragma_index_list": {
			integerColumn("seq", false), textColumn("name", false), integerColumn("unique", false),
			textColumn("origin", false), integerColumn("partial", false),
		},
		"pragma_index_info": {
			integerColumn("seqno", false), integerColumn("cid", false), textColumn("name", true),
		},
		"pragma_index_xinfo": {
			integerColumn("seqno", false), integerColumn("cid", false), textColumn("name", true),
			integerColumn("desc", false), textColumn("coll", true), integerColumn("key", false),
		},
		"pragma_foreign_key_list": {
			integerColumn("id", false), integerColumn("seq", false), textColumn("table", false),
			textColumn("from", false), textColumn("to", true), textColumn("on_update", false),
			textColumn("on_delete", false), textColumn("match", false),
		},
		"pragma_foreign_key_check": {
			textColumn("table", false), integerColumn("rowid", true), textColumn("parent", false),
			integerColumn("fkid", false),
		},
		"pragma_database_list": {
			integerColumn("seq", false), textColumn("name", false), textColumn("file", false),
		},
		"pragma_collation_list":  {integerColumn("seq", false), textColumn("name", false)},
		"pragma_module_list":     {textColumn("name", false)},
		"pragma_pragma_list":     {textColumn("name", false)},
		"pragma_compile_options": {textColumn("compile_options", false)},
		"pragma_function_list": {
			textColumn("name", false), integerColumn("builtin", false), textColumn("type", false),
			textColumn("enc", false), integerColumn("narg", false), integerColumn("flags", false),
		},
		"pragma_integrity_check": {textColumn("integrity_check", false)},
		"pragma_quick_check":     {textColumn("quick_check", false)},
		"pragma_user_version":    {integerColumn("user_version", false)},
		"pragma_application_id":  {integerColumn("application_id", false)},
		"pragma_schema_version":  {integerColumn("schema_version", false)},
		"pragma_page_count":      {integerColumn("page_count", false)},
		"pragma_page_size":       {integerColumn("page_size", false)},
		"pragma_freelist_count":  {integerColumn("freelist_count", false)},
		"pragma_foreign_keys":    {integerColumn("foreign_keys", false)},
		"pragma_journal_mode":    {textColumn("journal_mode", false)},
		"pragma_encoding":        {textColumn("encoding", false)},
	}

	// tableValuedFunctionColumns maps SQLite table-valued function names, and the built-in
	// sources the analyser reads as table-valued, to their output column schemas.
	tableValuedFunctionColumns = mergedTableValuedFunctionColumns(map[string][]ast.ScopedColumn{
		"json_each":       jsonTableColumns,
		"json_tree":       jsonTableColumns,
		"generate_series": {{Name: "value", SQLType: tvfSQLType("integer", sqlmodel.TypeCategoryInteger), Nullable: false}},
	}, schemaTableColumns, pragmaFunctionColumns)
)

// mergedTableValuedFunctionColumns combines the table-valued function schemas into one
// lookup.
//
// Takes groups (...map[string][]ast.ScopedColumn) which are the schemas to combine.
//
// Returns map[string][]ast.ScopedColumn which holds every entry of every group.
func mergedTableValuedFunctionColumns(groups ...map[string][]ast.ScopedColumn) map[string][]ast.ScopedColumn {
	merged := make(map[string][]ast.ScopedColumn)
	for _, group := range groups {
		maps.Copy(merged, group)
	}
	return merged
}

// isBuiltinRelation reports whether a FROM source names one of SQLite's built-in
// relations that may be written without call parentheses: a schema table such as
// sqlite_schema, or an argument-less pragma function such as pragma_table_list.
//
// Takes name (string) which is the source's name.
//
// Returns bool which is true for a built-in relation.
func isBuiltinRelation(name string) bool {
	lowered := strings.ToLower(name)
	if _, known := schemaTableColumns[lowered]; known {
		return true
	}
	_, known := pragmaFunctionColumns[lowered]
	return known
}

// textColumn builds a text column of a built-in table-valued source.
//
// Takes name (string) which is the column name.
// Takes nullable (bool) which is true when the column may be NULL.
//
// Returns ast.ScopedColumn which is the column.
func textColumn(name string, nullable bool) ast.ScopedColumn {
	return ast.ScopedColumn{Name: name, SQLType: tvfSQLType("text", sqlmodel.TypeCategoryText), Nullable: nullable}
}

// integerColumn builds an integer column of a built-in table-valued source.
//
// Takes name (string) which is the column name.
// Takes nullable (bool) which is true when the column may be NULL.
//
// Returns ast.ScopedColumn which is the column.
func integerColumn(name string, nullable bool) ast.ScopedColumn {
	return ast.ScopedColumn{Name: name, SQLType: tvfSQLType("integer", sqlmodel.TypeCategoryInteger), Nullable: nullable}
}

// fullTextTableNames collects the lower-cased names of the catalogue's FTS5 tables.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the schema a query runs against, or nil.
//
// Returns map[string]bool which holds one entry per FTS5 table, or nil when there are
// none.
func fullTextTableNames(catalogue *sqlmodel.Catalogue) map[string]bool {
	if catalogue == nil {
		return nil
	}
	var names map[string]bool
	for _, schema := range catalogue.Schemas {
		if schema == nil {
			continue
		}
		for name, table := range schema.Tables {
			if table == nil || !strings.EqualFold(table.VirtualModuleName, moduleFTS5) {
				continue
			}
			if names == nil {
				names = make(map[string]bool)
			}
			names[strings.ToLower(name)] = true
		}
	}
	return names
}

// fullTextTableColumns returns the columns the table-valued form of an FTS5 table,
// `docs('query')`, produces: the table's declared columns, without the hidden ones.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the schema to look the table up in.
// Takes functionName (string) which is the table-valued function name.
//
// Returns []ast.ScopedColumn which holds the columns, or nil when functionName is not an
// FTS5 table.
func fullTextTableColumns(catalogue *sqlmodel.Catalogue, functionName string) []ast.ScopedColumn {
	table := findFullTextTable(catalogue, functionName)
	if table == nil {
		return nil
	}
	columns := make([]ast.ScopedColumn, 0, len(table.Columns))
	for index := range table.Columns {
		if table.Columns[index].Hidden {
			continue
		}
		columns = append(columns, ast.ScopedColumn{
			Name:     table.Columns[index].Name,
			SQLType:  table.Columns[index].SQLType,
			Nullable: table.Columns[index].Nullable,
		})
	}
	return columns
}

// findFullTextTable finds the FTS5 table of the given name, searching the schemas in name
// order so the result is stable.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the schema to look the table up in, or
// nil.
// Takes name (string) which is the table name, matched ignoring case.
//
// Returns *sqlmodel.Table which is the FTS5 table, or nil when there is none.
func findFullTextTable(catalogue *sqlmodel.Catalogue, name string) *sqlmodel.Table {
	if catalogue == nil {
		return nil
	}
	for _, schemaName := range slices.Sorted(maps.Keys(catalogue.Schemas)) {
		schema := catalogue.Schemas[schemaName]
		if schema == nil {
			continue
		}
		for tableName, table := range schema.Tables {
			if table != nil && strings.EqualFold(tableName, name) && strings.EqualFold(table.VirtualModuleName, moduleFTS5) {
				return table
			}
		}
	}
	return nil
}

// tvfSQLType constructs a SQLType for a table-valued function column.
//
// Takes engineName (string) which is the SQLite affinity name.
// Takes category (sqlmodel.SQLTypeCategory) which is the categorised type.
//
// Returns sqlmodel.SQLType which is the normalised type of the name when it falls in the
// category, carrying its structural attributes, and otherwise the bare category and name.
func tvfSQLType(engineName string, category sqlmodel.SQLTypeCategory) sqlmodel.SQLType {
	if normalised := normaliseTypeName(engineName); normalised.Category == category {
		return normalised
	}
	return sqlmodel.NewSQLType(category, engineName)
}
