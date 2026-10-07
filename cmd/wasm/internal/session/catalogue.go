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

package session

import (
	"maps"
	"slices"

	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/sdk/dialect"
)

// CatalogueView describes the schema the migrations build. Schemas and the objects in
// them are sorted by name; columns keep their declared order.
type CatalogueView struct {
	// DefaultSchema is the schema unqualified names resolve in, or empty.
	DefaultSchema string `json:"defaultSchema"`

	// Schemas holds every schema.
	Schemas []SchemaView `json:"schemas"`
}

// SchemaView describes one schema.
type SchemaView struct {
	// Name is the schema's name.
	Name string `json:"name"`

	// Tables holds the schema's tables.
	Tables []TableView `json:"tables"`

	// Views holds the schema's views.
	Views []RelationView `json:"views"`

	// Enums holds the schema's enum types.
	Enums []EnumView `json:"enums"`

	// Composites holds the schema's composite types.
	Composites []RelationView `json:"composites"`

	// Sequences holds the schema's sequences.
	Sequences []SequenceView `json:"sequences"`

	// FunctionCount counts the schema's function overloads, including configured custom
	// functions.
	FunctionCount int `json:"functionCount"`
}

// TableView describes one table.
type TableView struct {
	// Name is the table's name.
	Name string `json:"name"`

	// Origin is the request path of the migration that created the table, or empty.
	Origin string `json:"origin"`

	// Comment is the table's comment, as COMMENT ON TABLE or a table's COMMENT option gives
	// it, or empty.
	Comment string `json:"comment"`

	// PartitionOf names the partitioned table the table is a partition of, qualified with
	// its schema when that differs from the table's, or is empty for any other table. A
	// partition carries its parent's columns and has no generated model.
	PartitionOf string `json:"partitionOf"`

	// Columns holds the table's columns in declared order.
	Columns []CatalogueColumnView `json:"columns"`
}

// RelationView describes a view or composite type: a name and its columns.
type RelationView struct {
	// Name is the view's or type's name.
	Name string `json:"name"`

	// Comment is the view's comment, or empty, and always empty for a composite type.
	Comment string `json:"comment"`

	// Columns holds its columns or fields in declared order.
	Columns []CatalogueColumnView `json:"columns"`
}

// EnumView describes one enum type.
type EnumView struct {
	// Name is the type's name.
	Name string `json:"name"`

	// Values holds the enum's labels in declared order.
	Values []string `json:"values"`
}

// SequenceView describes one sequence.
type SequenceView struct {
	// Name is the sequence's name.
	Name string `json:"name"`

	// OwnedBy is the column that owns the sequence, as table.column, or empty.
	OwnedBy string `json:"ownedBy"`

	// Columns holds the columns a query reading the sequence as a relation sees, such as
	// PostgreSQL's last_value, log_cnt and is_called, or is empty when the engine does not
	// let a query read a sequence.
	Columns []CatalogueColumnView `json:"columns"`
}

// CatalogueColumnView describes one column of a table, view or composite type.
type CatalogueColumnView struct {
	// Name is the column's name.
	Name string `json:"name"`

	// Type is the column's SQL type.
	Type string `json:"type"`

	// Comment is the column's comment, as COMMENT ON COLUMN or a column's COMMENT gives it,
	// or empty.
	Comment string `json:"comment"`

	// Nullable reports whether the column may be NULL.
	Nullable bool `json:"nullable"`

	// PrimaryKey reports whether the column is part of the primary key.
	PrimaryKey bool `json:"primaryKey"`

	// HasDefault reports whether the column has a default.
	HasDefault bool `json:"hasDefault"`

	// Generated reports whether the database computes the column.
	Generated bool `json:"generated"`
}

// catalogueView describes a schema catalogue.
//
// Takes files (map[string]string) which maps request paths to contents.
// Takes resolved (*project) which maps migration file names back to request paths.
// Takes catalogue (*dialect.Catalogue) which is described, or nil.
//
// Returns *CatalogueView which describes it, or nil when catalogue is nil.
func catalogueView(files map[string]string, resolved *project, catalogue *dialect.Catalogue) *CatalogueView {
	if catalogue == nil {
		return nil
	}
	view := &CatalogueView{DefaultSchema: catalogue.DefaultSchema, Schemas: make([]SchemaView, 0, len(catalogue.Schemas))}
	for _, name := range slices.Sorted(maps.Keys(catalogue.Schemas)) {
		schema := catalogue.Schemas[name]
		if schema == nil {
			continue
		}
		view.Schemas = append(view.Schemas, schemaView(files, resolved, name, schema))
	}
	return view
}

// schemaView describes one schema.
//
// Takes files (map[string]string) which maps request paths to contents.
// Takes resolved (*project) which maps migration file names back to request paths.
// Takes name (string) which is the schema's name.
// Takes schema (*dialect.Schema) which is described.
//
// Returns SchemaView which describes it.
func schemaView(files map[string]string, resolved *project, name string, schema *dialect.Schema) SchemaView {
	view := SchemaView{
		Name:          name,
		Tables:        make([]TableView, 0, len(schema.Tables)),
		Views:         make([]RelationView, 0, len(schema.Views)),
		Enums:         make([]EnumView, 0, len(schema.Enums)),
		Composites:    make([]RelationView, 0, len(schema.CompositeTypes)),
		Sequences:     make([]SequenceView, 0, len(schema.Sequences)),
		FunctionCount: 0,
	}
	for _, key := range slices.Sorted(maps.Keys(schema.Tables)) {
		table := schema.Tables[key]
		view.Tables = append(view.Tables, TableView{
			Name:        table.Name,
			Origin:      originPath(files, resolved, table.Origin),
			Comment:     table.Comment,
			PartitionOf: parentName(name, table.PartitionOf),
			Columns:     columnList(table.Columns, table.PrimaryKey),
		})
	}
	for _, key := range slices.Sorted(maps.Keys(schema.Views)) {
		relation := schema.Views[key]
		view.Views = append(view.Views, RelationView{Name: relation.Name, Comment: relation.Comment, Columns: columnList(relation.Columns, nil)})
	}
	for _, key := range slices.Sorted(maps.Keys(schema.Enums)) {
		enum := schema.Enums[key]
		view.Enums = append(view.Enums, EnumView{Name: enum.Name, Values: nonNil(slices.Clone(enum.Values))})
	}
	for _, key := range slices.Sorted(maps.Keys(schema.CompositeTypes)) {
		composite := schema.CompositeTypes[key]
		view.Composites = append(view.Composites, RelationView{Name: composite.Name, Comment: "", Columns: columnList(composite.Fields, nil)})
	}
	for _, key := range slices.Sorted(maps.Keys(schema.Sequences)) {
		sequence := schema.Sequences[key]
		owner := ""
		if sequence.OwnedByTable != "" && sequence.OwnedByColumn != "" {
			owner = sequence.OwnedByTable + "." + sequence.OwnedByColumn
		}
		view.Sequences = append(view.Sequences, SequenceView{Name: sequence.Name, OwnedBy: owner, Columns: columnList(sequence.Columns, nil)})
	}
	for _, overloads := range schema.Functions {
		view.FunctionCount += len(overloads)
	}
	return view
}

// columnList describes columns in their declared order.
//
// Takes columns ([]dialect.Column) which are described.
// Takes primaryKey ([]string) which names the primary key's columns, or nil.
//
// Returns []CatalogueColumnView which holds one entry per column.
func columnList(columns []dialect.Column, primaryKey []string) []CatalogueColumnView {
	views := make([]CatalogueColumnView, 0, len(columns))
	for index := range columns {
		column := &columns[index]
		views = append(views, CatalogueColumnView{
			Name:       column.Name,
			Type:       assemble.SQLTypeName(column.SQLType),
			Comment:    column.Comment,
			Nullable:   column.Nullable,
			PrimaryKey: slices.Contains(primaryKey, column.Name),
			HasDefault: column.HasDefault,
			Generated:  column.IsGenerated,
		})
	}
	return views
}

// parentName names the partitioned table a partition belongs to, qualified with its
// schema when that differs from the partition's.
//
// Takes schema (string) which is the partition's schema.
// Takes parent (*dialect.TableName) which names the parent, or is nil.
//
// Returns string which is the parent's name, or empty when parent is nil.
func parentName(schema string, parent *dialect.TableName) string {
	switch {
	case parent == nil:
		return ""
	case parent.Schema == "" || parent.Schema == schema:
		return parent.Name
	default:
		return parent.Schema + "." + parent.Name
	}
}

// originPath maps a catalogue object's origin back to the migration's request path.
//
// Takes files (map[string]string) which maps request paths to contents.
// Takes resolved (*project) which names the migration directory.
// Takes origin (dialect.MigrationOrigin) which names the migration file.
//
// Returns string which is the request path, or empty when the origin is unknown.
func originPath(files map[string]string, resolved *project, origin dialect.MigrationOrigin) string {
	if origin.Filename == "" {
		return ""
	}
	return resolved.migrationPath(files, origin.Filename)
}
