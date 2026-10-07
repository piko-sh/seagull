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

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// embedGroup describes a set of output columns that belong to one embed, along with
// whether the embedded table was introduced via an outer join.
type embedGroup struct {
	// naming names the group's Row field and struct type.
	naming *emit.EmbedNaming

	// TableName holds the name of the embedded table, which a group_by key table is matched
	// against.
	TableName string

	// Columns holds the indices into AnalysedQuery.OutputColumns of the group's columns.
	Columns []int

	// IsOuter holds whether the table was joined via an outer join.
	IsOuter bool
}

// GroupQueriesByFilename groups queries by their source SQL filename.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries to group.
//
// Returns map[string][]*sqlmodel.AnalysedQuery which maps filename to queries.
func GroupQueriesByFilename(queries []*sqlmodel.AnalysedQuery) map[string][]*sqlmodel.AnalysedQuery {
	grouped := make(map[string][]*sqlmodel.AnalysedQuery)
	for _, query := range queries {
		grouped[query.Filename] = append(grouped[query.Filename], query)
	}
	return grouped
}

// HasOutputColumns reports whether a query produces result rows.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query to check.
//
// Returns bool which is true when the query has output columns and uses a row-returning
// command.
func HasOutputColumns(query *sqlmodel.AnalysedQuery) bool {
	if len(query.OutputColumns) == 0 {
		return false
	}
	switch query.Command {
	case sqlmodel.QueryCommandOne, sqlmodel.QueryCommandMany, sqlmodel.QueryCommandStream, sqlmodel.QueryCommandBatch:
		return true
	default:
		return false
	}
}

// BuildConstant constructs the const declaration of a piece of SQL, quoted as
// goast.StringLiteral quotes it.
//
// Takes name (string) which names the constant.
// Takes sql (string) which is the SQL.
//
// Returns ast.Decl which is the const declaration.
func BuildConstant(name, sql string) ast.Decl {
	return &ast.GenDecl{
		Tok: token.CONST,
		Specs: []ast.Spec{
			&ast.ValueSpec{
				Names:  []*ast.Ident{goastutil.CachedIdent(name)},
				Values: []ast.Expr{goast.StringLiteral(sql)},
			},
		},
	}
}

// BuildParamsStruct constructs a query's Params struct: one field per parameter, named by
// the naming plan, of the type ParameterType gives it.
//
// Takes query (*gogen.Query) which is the query.
// Takes parameters ([]sqlmodel.QueryParameter) which are the query's parameters, or a
// copy of them with their nullability promoted.
//
// Returns ast.Decl which is the type declaration.
func BuildParamsStruct(query *gogen.Query, parameters []sqlmodel.QueryParameter) ast.Decl {
	fields := make([]*ast.Field, 0, len(parameters))
	for index := range parameters {
		fields = append(fields, goastutil.Field(query.Naming.ParameterFields[index], ParameterType(query, &parameters[index])))
	}
	return goastutil.GenDeclType(query.Naming.Params, goastutil.StructType(fields...))
}

// RequiresRowType reports whether the query command expects a Row type to scan into,
// regardless of whether output columns were successfully resolved.
//
// Used to decide whether to emit a stub Row struct when the analyser could not resolve
// any output columns. The stub keeps the generated code compilable, so a package that
// calls the affected methods still builds rather than failing as a whole. A batch method
// scans nothing, so a batch query without output columns gets no stub.
//
// Takes query (*sqlmodel.AnalysedQuery) which provides the query command.
//
// Returns bool which is true when the command needs a Row type.
func RequiresRowType(query *sqlmodel.AnalysedQuery) bool {
	switch query.Command {
	case sqlmodel.QueryCommandOne, sqlmodel.QueryCommandMany, sqlmodel.QueryCommandStream:
		return true
	default:
		return false
	}
}

// EmptyRowStruct emits a no-field Row stub.
//
// Used when the analyser could not resolve any output columns but the query command still
// requires a Row type for the generated Scan call site.
//
// Takes structName (string) which is the name for the generated stub struct.
//
// Returns ast.Decl which is the empty struct type declaration.
func EmptyRowStruct(structName string) ast.Decl {
	return goastutil.GenDeclType(structName, goastutil.StructType())
}

// BuildOutputStructs generates the row struct and any per-embed structs for a query,
// named by the naming plan.
//
// When no embeds are present, produces a single row struct. When embeds exist, produces
// per-embed structs followed by a main row struct containing nested embed fields. For
// group_by queries, non-key embed fields become slices.
//
// Takes emission (*gogen.Emission) which holds the style shaping the fields' struct tags.
// Takes query (*gogen.Query) which defines the output columns and their names.
//
// Returns []ast.Decl which contains the struct declarations.
func BuildOutputStructs(emission *gogen.Emission, query *gogen.Query) []ast.Decl {
	style := &emission.Request.Style
	if !hasEmbeddedColumns(query.Analysed) {
		return []ast.Decl{buildColumnStruct(style, query, query.Naming.Row, flatColumns(query.Analysed))}
	}
	groups := embedGroups(query)
	declarations := make([]ast.Decl, 0, len(groups)+1)
	for index := range groups {
		declarations = append(declarations, buildColumnStruct(style, query, groups[index].naming.Type, groups[index].Columns))
	}
	return append(declarations, buildEmbedRowStruct(style, query, groups))
}

// ColumnGoType returns the Go type an output column is read into: its go_type override,
// or the mapped type.
//
// Takes query (*gogen.Query) which holds the column.
// Takes column (int) which is the index into AnalysedQuery.OutputColumns.
//
// Returns sqlmodel.GoType which is the type.
func ColumnGoType(query *gogen.Query, column int) sqlmodel.GoType {
	output := &query.Analysed.OutputColumns[column]
	if override := goast.NullableOverride(output.GoTypeOverride, output.SQLType, output.Nullable); override != nil {
		return *override
	}
	return goast.ResolveGoType(output.SQLType, output.Nullable, query.Types)
}

// hasEmbeddedColumns reports whether any output column is part of an embed.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query to check.
//
// Returns bool which is true when at least one output column is embedded.
func hasEmbeddedColumns(query *sqlmodel.AnalysedQuery) bool {
	for index := range query.OutputColumns {
		if query.OutputColumns[index].IsEmbedded {
			return true
		}
	}
	return false
}

// flatColumns returns the indices of a query's output columns that belong to no embed, in
// order.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []int which holds the indices.
func flatColumns(query *sqlmodel.AnalysedQuery) []int {
	var columns []int
	for index := range query.OutputColumns {
		if !query.OutputColumns[index].IsEmbedded {
			columns = append(columns, index)
		}
	}
	return columns
}

// embedGroups returns a query's embed groups in the order the naming plan names them,
// which is the order they first appear among the output columns.
//
// Takes query (*gogen.Query) which is the query.
//
// Returns []embedGroup which holds the groups.
func embedGroups(query *gogen.Query) []embedGroup {
	groups := make([]embedGroup, len(query.Naming.Embeds))
	positions := make(map[string]int, len(groups))
	for index := range query.Naming.Embeds {
		groups[index] = embedGroup{naming: &query.Naming.Embeds[index], TableName: "", Columns: nil, IsOuter: false}
		positions[query.Naming.Embeds[index].Group] = index
	}
	columns := query.Analysed.OutputColumns
	for index := range columns {
		if !columns[index].IsEmbedded {
			continue
		}
		position, ok := positions[embedGroupName(&columns[index])]
		if !ok {
			continue
		}
		group := &groups[position]
		if len(group.Columns) == 0 {
			group.TableName = columns[index].EmbedTable
			group.IsOuter = columns[index].EmbedIsOuter
		}
		group.Columns = append(group.Columns, index)
	}
	return groups
}

// embedGroupName returns the name of the embed group an embedded column belongs to: its
// EmbedName, or its EmbedTable when the model leaves the name empty.
//
// Takes column (*sqlmodel.OutputColumn) which is the embedded column.
//
// Returns string which is the embed group name.
func embedGroupName(column *sqlmodel.OutputColumn) string {
	if column.EmbedName != "" {
		return column.EmbedName
	}
	return column.EmbedTable
}

// groupByKeyTable extracts the table name from the first group_by key (e.g., "orders.id"
// -> "orders").
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query to inspect.
//
// Returns string which is the key table name, or empty string if no group_by is set.
func groupByKeyTable(query *sqlmodel.AnalysedQuery) string {
	if len(query.GroupByKey) == 0 {
		return ""
	}
	parts := strings.SplitN(query.GroupByKey[0], ".", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

// isGroupByDetailEmbed reports whether a given embed group is a "detail" group in a
// group_by query (i.e., not the key table). Detail embeds become slices in the row
// struct.
//
// Takes group (*embedGroup) which is the embed group to check.
// Takes keyTable (string) which is the group_by key table name.
//
// Returns bool which is true when the group is a detail embed.
func isGroupByDetailEmbed(group *embedGroup, keyTable string) bool {
	return keyTable != "" && !strings.EqualFold(group.TableName, keyTable)
}

// buildColumnStruct constructs a struct with one field per output column, named by the
// naming plan and tagged as the style and the column's struct tags say.
//
// Takes style (*emit.Style) which shapes the fields' struct tags.
// Takes query (*gogen.Query) which holds the columns and their names.
// Takes structName (string) which names the struct.
// Takes columns ([]int) which are the indices of the struct's columns.
//
// Returns ast.Decl which is the type declaration.
func buildColumnStruct(style *emit.Style, query *gogen.Query, structName string, columns []int) ast.Decl {
	return goastutil.GenDeclType(structName, goastutil.StructType(columnFields(style, query, columns)...))
}

// columnFields returns the fields of a struct holding output columns.
//
// Takes style (*emit.Style) which shapes the fields' struct tags.
// Takes query (*gogen.Query) which holds the columns and their names.
// Takes columns ([]int) which are the indices of the struct's columns.
//
// Returns []*ast.Field which holds the fields.
func columnFields(style *emit.Style, query *gogen.Query, columns []int) []*ast.Field {
	sourceNames := make([]string, len(columns))
	columnTags := make([]map[string]string, len(columns))
	for position, column := range columns {
		sourceNames[position] = query.Analysed.OutputColumns[column].Name
		columnTags[position] = query.Analysed.OutputColumns[column].StructTags
	}
	tags := emitkit.FieldTags(style, sourceNames, columnTags)
	fields := make([]*ast.Field, 0, len(columns))
	for position, column := range columns {
		fields = append(fields, &ast.Field{
			Names: []*ast.Ident{goastutil.CachedIdent(query.Naming.ColumnFields[column])},
			Type:  query.Imports.AddType(ColumnGoType(query, column)),
			Tag:   goast.TagLiteral(tags[position]),
		})
	}
	return fields
}

// buildEmbedRowStruct constructs the main row struct with flat fields and nested embed
// fields.
//
// Inner-join embeds are value types; outer-join embeds are pointer types. In group_by
// queries, non-key embeds become slice fields.
//
// Takes style (*emit.Style) which shapes the flat fields' struct tags.
// Takes query (*gogen.Query) which holds the columns and their names.
// Takes groups ([]embedGroup) which are the query's embed groups.
//
// Returns ast.Decl which is the struct type declaration.
func buildEmbedRowStruct(style *emit.Style, query *gogen.Query, groups []embedGroup) ast.Decl {
	fields := columnFields(style, query, flatColumns(query.Analysed))
	keyTable := groupByKeyTable(query.Analysed)
	for index := range groups {
		embedType := goastutil.CachedIdent(groups[index].naming.Type)
		var fieldType ast.Expr = embedType
		switch {
		case isGroupByDetailEmbed(&groups[index], keyTable):
			fieldType = &ast.ArrayType{Elt: embedType}
		case groups[index].IsOuter:
			fieldType = goastutil.StarExpr(embedType)
		}
		fields = append(fields, goastutil.Field(groups[index].naming.Field, fieldType))
	}
	return goastutil.GenDeclType(query.Naming.Row, goastutil.StructType(fields...))
}
