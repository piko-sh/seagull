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

package emitkit

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// rowChoice is the struct the plan reads a query's rows into when it is not a Row struct
// of the query's own: a table's model, or a row type several queries share.
type rowChoice struct {
	// name is the struct's name.
	name string

	// shared is true when another declaration declares the struct: the models file a model,
	// or the first query naming a shared row type.
	shared bool
}

// rowField is one field of a row struct as the plan compares two of them.
type rowField struct {
	// goType is the field's Go type, as an override or the type mapping gives it.
	goType sqlmodel.GoType

	// column is the column the field holds, as a message names it.
	column string

	// named holds the keys of the enum and struct types the field's type holds that the
	// package names, which decide its Go type once the mapping names them.
	named string

	// tag is the field's struct tag, without back quotes.
	tag string
}

// rowShape is the fields of a row struct, by field name.
type rowShape struct {
	// fields maps each field's name to the field.
	fields map[string]rowField

	// order lists the fields' names in declaration order.
	order []string
}

// modelEntry is a model the package declares and the table it stands for.
type modelEntry struct {
	// naming names the model and its fields.
	naming *emit.ModelNaming

	// table is the table.
	table *sqlmodel.Table
}

// rowPlanner chooses the queries whose rows are read into a struct other than a Row
// struct of their own, and checks that each such query's columns fit it.
type rowPlanner struct {
	// request is the request being planned.
	request *emit.Request

	// scope is the package scope a shared row type's name is claimed in.
	scope *nameScope

	// named reports which enum and struct types the package names, or is nil.
	named func(sqlType sqlmodel.SQLType) bool

	// models maps a table's schema and name to its model.
	models map[sqlmodel.TableName]modelEntry

	// choices maps each query reading its rows into such a struct to the struct.
	choices map[*sqlmodel.AnalysedQuery]rowChoice

	// problems accumulates the queries whose columns do not fit the struct they ask for.
	problems []emit.Problem
}

// chooseModel reads a query's rows into a table's model when the query's columns give the
// model's fields, and otherwise reports the query when it asked for the model.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes table (sqlmodel.TableName) which is the table.
// Takes asked (bool) which is true when the query's model: keyword asked for the model.
func (r *rowPlanner) chooseModel(query *sqlmodel.AnalysedQuery, table sqlmodel.TableName, asked bool) {
	entry, found := r.models[table]
	if !found {
		if asked {
			r.report(query, fmt.Sprintf("model: the package declares no model for %s", r.tableName(table)))
		}
		return
	}
	differences := shapeDifferences(r.modelShape(entry), r.queryShape(query), "the model "+entry.naming.Type)
	if len(differences) > 0 {
		if asked {
			r.report(query, fmt.Sprintf("model: the rows do not fit the model %s of %s: %s",
				entry.naming.Type, r.tableName(table), strings.Join(differences, "; ")))
		}
		return
	}
	r.choices[query] = rowChoice{name: entry.naming.Type, shared: true}
}

// shareRow assigns a named row type to queries with matching fields, with the first query
// declaring it. It reports every query using a name already declared or imported by the
// package.
//
// Takes name (string) which is the row type's name.
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries naming it, in planning
// order.
func (r *rowPlanner) shareRow(name string, queries []*sqlmodel.AnalysedQuery) {
	if !r.scope.take(name) {
		for _, query := range queries {
			r.report(query, fmt.Sprintf("row: %s is a name the generated package already declares or imports, such as a "+
				"table's model; choose another name, or read the rows into a table's model with model:", name))
		}
		return
	}
	declaring := queries[0]
	r.choices[declaring] = rowChoice{name: name, shared: false}
	want := r.queryShape(declaring)
	for _, query := range queries[1:] {
		differences := shapeDifferences(want, r.queryShape(query), name+" as "+exportedName(declaring.Name)+" declares it")
		if len(differences) > 0 {
			r.report(query, fmt.Sprintf("row: the rows do not fit %s, which %s declares: %s",
				name, exportedName(declaring.Name), strings.Join(differences, "; ")))
			continue
		}
		r.choices[query] = rowChoice{name: name, shared: true}
	}
}

// readTable finds the one table every column of a query reads, whose model the query may
// return when the style reuses models: the table, or the table a partition partitions, of
// every column's source.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns sqlmodel.TableName which is the table.
// Returns bool which is false when the query reads a column that is not a table's own, or
// the columns of more than one table.
func (r *rowPlanner) readTable(query *sqlmodel.AnalysedQuery) (sqlmodel.TableName, bool) {
	catalogue := r.request.Catalogue
	if catalogue == nil || len(query.OutputColumns) == 0 {
		return sqlmodel.TableName{}, false
	}
	var read *sqlmodel.Table
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		if column.SourceTable == "" || column.SourceColumn == "" {
			return sqlmodel.TableName{}, false
		}
		table := catalogue.LookupTable(column.SourceSchema, column.SourceTable)
		if table != nil && table.PartitionOf != nil {
			table = catalogue.LookupTable(table.PartitionOf.Schema, table.PartitionOf.Name)
		}
		if table == nil || (read != nil && table != read) {
			return sqlmodel.TableName{}, false
		}
		read = table
	}
	return sqlmodel.TableName{Schema: read.Schema, Name: read.Name}, true
}

// report records a query whose rows cannot be read into the struct it asks for.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes message (string) which describes the problem.
func (r *rowPlanner) report(query *sqlmodel.AnalysedQuery, message string) {
	r.problems = append(r.problems, emit.Problem{Query: query, Code: diag.CodeRowType, Message: message, Severity: diag.SeverityError})
}

// queryShape returns the fields of the Row struct a query of no embed would declare: one
// per output column, named as the plan names them, of the Go type and with the struct tag
// the emitters give it.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns rowShape which holds the fields.
func (r *rowPlanner) queryShape(query *sqlmodel.AnalysedQuery) rowShape {
	columns := query.OutputColumns
	names := make([]string, len(columns))
	columnTags := make([]map[string]string, len(columns))
	for index := range columns {
		names[index] = columns[index].Name
		columnTags[index] = columns[index].StructTags
	}
	tags := FieldTags(&r.request.Style, names, columnTags)
	fieldNames := flatColumnFields(columns, newNameScope())
	shape := rowShape{fields: make(map[string]rowField, len(columns)), order: fieldNames}
	for index := range columns {
		column := &columns[index]
		shape.fields[fieldNames[index]] = r.field(column.SQLType, column.Nullable, column.GoTypeOverride, column.Name, tags[index])
	}
	return shape
}

// modelShape returns the fields of a model: one per column the table does not hide, of
// the Go type and with the struct tag the models file gives it.
//
// Takes entry (modelEntry) which is the model and its table.
//
// Returns rowShape which holds the fields.
func (r *rowPlanner) modelShape(entry modelEntry) rowShape {
	columns := entry.table.Columns
	var held []int
	var names []string
	var columnTags []map[string]string
	for index := range columns {
		if index < len(entry.naming.Fields) && entry.naming.Fields[index] != "" {
			held = append(held, index)
			names = append(names, columns[index].Name)
			columnTags = append(columnTags, columns[index].StructTags)
		}
	}
	tags := FieldTags(&r.request.Style, names, columnTags)
	shape := rowShape{fields: make(map[string]rowField, len(held)), order: make([]string, 0, len(held))}
	for position, index := range held {
		column := &columns[index]
		nullable := column.Nullable
		if column.NullableOverride != nil {
			nullable = *column.NullableOverride
		}
		name := entry.naming.Fields[index]
		shape.order = append(shape.order, name)
		shape.fields[name] = r.field(column.ValueType(), nullable, column.GoTypeOverride, column.Name, tags[position])
	}
	return shape
}

// field describes one field of a row struct.
//
// Takes sqlType (sqlmodel.SQLType) which is the type of the column the field holds.
// Takes nullable (bool) which is true when the column may be NULL.
// Takes override (*sqlmodel.GoType) which is the column's Go type override, or nil.
// Takes column (string) which is the column's name.
// Takes tag (string) which is the field's struct tag.
//
// Returns rowField which is the field.
func (r *rowPlanner) field(sqlType sqlmodel.SQLType, nullable bool, override *sqlmodel.GoType, column, tag string) rowField {
	if overridden := goast.NullableOverride(override, sqlType, nullable); overridden != nil {
		return rowField{goType: *overridden, column: column, named: "", tag: tag}
	}
	var named strings.Builder
	r.writeNamedKeys(&named, sqlType, 0)
	return rowField{goType: r.request.Types.Resolve(sqlType, nullable), column: column, named: named.String(), tag: tag}
}

// writeNamedKeys writes the key of every enum and struct type a type holds that the
// package names, at every depth.
//
// Takes builder (*strings.Builder) which receives the keys.
// Takes sqlType (sqlmodel.SQLType) which is the type.
// Takes depth (int) which bounds the walk.
func (r *rowPlanner) writeNamedKeys(builder *strings.Builder, sqlType sqlmodel.SQLType, depth int) {
	if r.named == nil || depth >= sqlmodel.MaxTypeDepth {
		return
	}
	if (sqlType.Category == sqlmodel.TypeCategoryEnum || sqlType.Category == sqlmodel.TypeCategoryStruct) && r.named(sqlType) {
		builder.WriteString(namedTypeKey(&sqlType, r.request.Catalogue))
		return
	}
	for _, part := range []*sqlmodel.SQLType{sqlType.ElementType, sqlType.KeyType} {
		if part != nil {
			r.writeNamedKeys(builder, *part, depth+1)
		}
	}
	for index := range sqlType.StructFields {
		r.writeNamedKeys(builder, sqlType.StructFields[index].SQLType, depth+1)
	}
}

// tableName writes a table's name for a message, after its schema's when the schema is
// off the search path.
//
// Takes table (sqlmodel.TableName) which is the table.
//
// Returns string which is the name.
func (r *rowPlanner) tableName(table sqlmodel.TableName) string {
	if table.Schema == "" || r.request.Catalogue == nil || r.request.Catalogue.OnSearchPath(table.Schema) {
		return table.Name
	}
	return table.Schema + "." + table.Name
}

// planRows chooses shared row structs from model: or row: directives, or from matching
// table models when reuse is enabled. It reports field mismatches and leaves incompatible
// queries with their own Row structs.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries in planning order.
// Takes models ([]emit.ModelNaming) which are the package's models.
//
// Returns map[*sqlmodel.AnalysedQuery]rowChoice which maps each query reading its rows
// into such a struct to the struct.
func (p *packagePlanner) planRows(queries []*sqlmodel.AnalysedQuery, models []emit.ModelNaming) map[*sqlmodel.AnalysedQuery]rowChoice {
	planner := &rowPlanner{
		request:  p.request,
		scope:    p.scope,
		named:    p.style.NamedTypes,
		models:   modelEntries(p.request.Catalogue, models),
		choices:  make(map[*sqlmodel.AnalysedQuery]rowChoice),
		problems: nil,
	}
	shared := make(map[string][]*sqlmodel.AnalysedQuery)
	for _, query := range queries {
		if !readsRows(query) {
			continue
		}
		switch {
		case query.RowModel != (sqlmodel.TableName{}):
			planner.chooseModel(query, query.RowModel, true)
		case query.RowType != "":
			name, err := exportedQueryName(query.RowType)
			if err != nil {
				planner.report(query, fmt.Sprintf("row: the row type %s", strings.TrimPrefix(err.Error(), "query name ")))
				continue
			}
			shared[name] = append(shared[name], query)
		case p.request.Style.ReuseModels:
			if table, found := planner.readTable(query); found {
				planner.chooseModel(query, table, false)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(shared)) {
		planner.shareRow(name, shared[name])
	}
	p.problems = append(p.problems, planner.problems...)
	return planner.choices
}

// shapeDifferences lists missing fields, extra columns and mismatched Go types or struct
// tags between a query's Row struct and its target struct. Field order does not matter.
//
// Takes want (rowShape) which is the struct's fields.
// Takes got (rowShape) which is the fields the query's columns give.
// Takes target (string) which names the struct in the messages, such as the model Roles.
//
// Returns []string which describes each difference, or is empty when there is none.
func shapeDifferences(want, got rowShape, target string) []string {
	var differences []string
	for _, name := range want.order {
		wanted := want.fields[name]
		given, found := got.fields[name]
		switch {
		case !found:
			differences = append(differences, fmt.Sprintf("no column gives the field %s of %s", name, target))
		case !reflect.DeepEqual(wanted.goType, given.goType) || wanted.named != given.named:
			differences = append(differences, fmt.Sprintf("the column %s is %s, and the field %s of %s is %s",
				given.column, describeGoType(given.goType), name, target, describeGoType(wanted.goType)))
		case wanted.tag != given.tag:
			differences = append(differences, fmt.Sprintf("the column %s is tagged %#q, and the field %s of %s %#q",
				given.column, given.tag, name, target, wanted.tag))
		}
	}
	for _, name := range got.order {
		if _, found := want.fields[name]; !found {
			differences = append(differences, fmt.Sprintf("the column %s has no field %s in %s", got.fields[name].column, name, target))
		}
	}
	return differences
}

// describeGoType writes a Go type as Go source names it, each package by the name its
// path implies, for a message.
//
// Takes goType (sqlmodel.GoType) which is the type.
//
// Returns string which is the type's source.
func describeGoType(goType sqlmodel.GoType) string {
	return NewFileImports(&emit.Naming{Files: nil, Imports: nil, Models: nil, Types: nil, Queries: nil, Interface: ""}).Type(goType)
}

// modelEntries maps each model of a package by its table's schema and name.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables, or nil.
// Takes models ([]emit.ModelNaming) which are the models.
//
// Returns map[sqlmodel.TableName]modelEntry which maps each table to its model.
func modelEntries(catalogue *sqlmodel.Catalogue, models []emit.ModelNaming) map[sqlmodel.TableName]modelEntry {
	entries := make(map[sqlmodel.TableName]modelEntry, len(models))
	tables := catalogueTables(catalogue)
	for index := range models {
		model := &models[index]
		position := slices.IndexFunc(tables, func(table *sqlmodel.Table) bool {
			return table.Schema == model.Schema && table.Name == model.Relation
		})
		if position >= 0 {
			entries[sqlmodel.TableName{Schema: model.Schema, Name: model.Relation}] = modelEntry{naming: model, table: tables[position]}
		}
	}
	return entries
}

// readsRows reports whether a query's rows may be read into a struct other than one of
// its own: a query returning rows one at a time, all at once or as a stream, or a runtime
// builder's, of no embed.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns bool which is true for such a query.
func readsRows(query *sqlmodel.AnalysedQuery) bool {
	if len(embedGroups(query)) > 0 || len(query.OutputColumns) == 0 {
		return false
	}
	switch query.Command {
	case sqlmodel.QueryCommandOne, sqlmodel.QueryCommandMany, sqlmodel.QueryCommandStream:
		return true
	default:
		return query.DynamicRuntime
	}
}

// exportedName returns the method name a query's name gives, or the name itself when it
// gives none, for a message.
//
// Takes name (string) which is the query's name.
//
// Returns string which is the name.
func exportedName(name string) string {
	if exported, err := exportedQueryName(name); err == nil {
		return exported
	}
	return name
}

// flatColumnFields names the Row field of each output column of no embed, in order,
// unique within the struct the scope holds the names of.
//
// Takes columns ([]sqlmodel.OutputColumn) which are the query's output columns.
// Takes row (*nameScope) which holds the struct's names; it is updated.
//
// Returns []string which holds, parallel to columns, each field's name, or empty for an
// embedded column.
func flatColumnFields(columns []sqlmodel.OutputColumn, row *nameScope) []string {
	fields := make([]string, len(columns))
	for index := range columns {
		if !columns[index].IsEmbedded {
			fields[index] = row.claim(columnField(&columns[index]))
		}
	}
	return fields
}
