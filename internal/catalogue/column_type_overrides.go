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

package catalogue

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// unqualifiedColumnParts is the number of parts in a "table.column" reference.
	unqualifiedColumnParts = 2

	// qualifiedColumnParts is the number of parts in a "schema.table.column" reference.
	qualifiedColumnParts = 3
)

// ColumnReference names one column of the catalogue, optionally schema-qualified.
type ColumnReference struct {
	// Schema is the schema name, or empty to search every schema.
	Schema string

	// Table is the table name.
	Table string

	// Column is the column name.
	Column string
}

// partitionKey names a table by the keys of its schema's name and its own, as the
// catalogue's identifier rules compare them.
type partitionKey struct {
	// schema is the key of the table's schema name.
	schema string

	// name is the key of the table's name.
	name string
}

// partitionIndex maps each partitioned table, by its key, to the tables that partition it
// directly, as their PartitionOf names it.
type partitionIndex map[partitionKey][]*sqlmodel.Table

// newPartitionIndex indexes the tables of a catalogue that partition another by the table
// their PartitionOf names.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables.
//
// Returns partitionIndex which maps each partitioned table's key to its partitions.
func newPartitionIndex(catalogue *sqlmodel.Catalogue) partitionIndex {
	index := make(partitionIndex)
	for _, schema := range catalogue.Schemas {
		if schema == nil {
			continue
		}
		for _, table := range schema.Tables {
			if table == nil || table.PartitionOf == nil {
				continue
			}
			key := partitionKey{schema: catalogue.Key(table.PartitionOf.Schema), name: catalogue.Key(table.PartitionOf.Name)}
			index[key] = append(index[key], table)
		}
	}
	return index
}

// ParseColumnReference splits a "table.column" or "schema.table.column" reference.
//
// Takes reference (string) which is the reference as configured.
//
// Returns ColumnReference which holds the parts.
// Returns error which wraps fault.ErrInvalidConfig when the reference does not have two
// or three non-empty dot-separated parts.
func ParseColumnReference(reference string) (ColumnReference, error) {
	parts := strings.Split(reference, ".")
	if slices.Contains(parts, "") || (len(parts) != unqualifiedColumnParts && len(parts) != qualifiedColumnParts) {
		return ColumnReference{}, fmt.Errorf("column %q must be table.column or schema.table.column: %w",
			reference, fault.ErrInvalidConfig)
	}
	if len(parts) == unqualifiedColumnParts {
		return ColumnReference{Schema: "", Table: parts[0], Column: parts[1]}, nil
	}
	return ColumnReference{Schema: parts[0], Table: parts[1], Column: parts[2]}, nil
}

// ApplyColumnTypeOverrides sets the Go type, and the elements' nullability, of every
// column named by a column type override, as a migration column directive does. A column
// that already carries a go_type or non_null_elements directive keeps it, because the
// directive ranks above configuration.
//
// Overrides without a Column are SQL-type overrides and are skipped; the type mapping
// table applies them instead. Any override, of a column or of a SQL type, whose Go type
// the generator cannot write is reported as a Q049 error and not applied.
//
// Takes catalogue (*sqlmodel.Catalogue) which is updated in place.
// Takes overrides ([]sqlmodel.TypeOverride) which are the configured overrides.
//
// Returns []diag.SourceError which holds a Q049 error for each override whose Go type is
// unusable and a Q037 error for each override whose column is not in the catalogue, with
// the closest known column as the suggestion.
func ApplyColumnTypeOverrides(catalogue *sqlmodel.Catalogue, overrides []sqlmodel.TypeOverride) []diag.SourceError {
	var diagnostics []diag.SourceError
	var knownColumns []string
	var partitions partitionIndex
	for _, override := range overrides {
		if unusable, ok := unusableTypeOverride(override); ok {
			diagnostics = append(diagnostics, unusable)
			continue
		}
		if override.Column == "" {
			continue
		}
		reference, err := ParseColumnReference(override.Column)
		column := findReferencedColumn(catalogue, reference)
		if err != nil || column == nil {
			if knownColumns == nil {
				knownColumns = collectKnownTableColumns(catalogue, writtenAsNamed)
			}
			diagnostics = append(diagnostics, diagbuild.NewErrorBuilder("").UnknownOverrideMigrationColumn(
				diagbuild.TextSpan{Line: 0, Column: 0, EndLine: 0, EndColumn: 0},
				"type override",
				override.Column,
				knownColumns,
			))
			continue
		}
		applyColumnTypeOverride(column, override)
		if partitions == nil {
			partitions = newPartitionIndex(catalogue)
		}
		for _, partitionColumn := range partitionColumns(catalogue, reference, partitions) {
			applyColumnTypeOverride(partitionColumn, override)
		}
	}
	return diagnostics
}

// partitionColumns returns the column a reference names in every partition, at every
// level, of the table it names, since a partition holds its parent's columns. Each table
// is visited once, so partitions that name each other in a cycle end the walk.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables.
// Takes reference (ColumnReference) which names a table's column.
// Takes partitions (partitionIndex) which maps each table to its partitions.
//
// Returns []*sqlmodel.Column which aliases the partitions' columns.
func partitionColumns(catalogue *sqlmodel.Catalogue, reference ColumnReference, partitions partitionIndex) []*sqlmodel.Column {
	identifiers := catalogue.Identifiers
	parent := catalogue.LookupTable(identifiers.Fold(reference.Schema, false), identifiers.Fold(reference.Table, false))
	if parent == nil {
		return nil
	}
	columnName := identifiers.Fold(reference.Column, false)
	var columns []*sqlmodel.Column
	visited := map[*sqlmodel.Table]struct{}{parent: {}}
	for pending := []*sqlmodel.Table{parent}; len(pending) > 0; pending = pending[1:] {
		current := pending[0]
		for _, partition := range partitions[partitionKey{schema: catalogue.Key(current.Schema), name: catalogue.Key(current.Name)}] {
			if _, seen := visited[partition]; seen {
				continue
			}
			visited[partition] = struct{}{}
			pending = append(pending, partition)
			for index := range partition.Columns {
				if identifiers.Equal(partition.Columns[index].Name, columnName) {
					columns = append(columns, &partition.Columns[index])
				}
			}
		}
	}
	return columns
}

// applyColumnTypeOverride sets what a column type override declares on its column: its Go
// type, unless the column already carries a go_type directive, which ranks above
// configuration, and its elements' nullability, unless a directive declared it.
//
// Takes column (*sqlmodel.Column) which receives the override.
// Takes override (sqlmodel.TypeOverride) which is the configured override.
func applyColumnTypeOverride(column *sqlmodel.Column, override sqlmodel.TypeOverride) {
	if override.GoName != "" && column.GoTypeOverride == nil {
		column.GoTypeOverride = new(goast.WrittenGoType(override.GoPackage, override.GoName))
	}
	if override.NonNullElements != nil && column.NonNullElements == nil {
		column.NonNullElements = new(*override.NonNullElements)
	}
	if len(override.StructTags) > 0 {
		column.StructTags = mergeStructTags(column.StructTags, override.StructTags, false)
	}
}

// mergeStructTags returns a column's struct tags with more of them added: over those
// already there, key by key, when the added tags take precedence, as a migration
// directive's do, or only where a key is not there yet, as a configured override's do.
//
// Takes existing (map[string]string) which holds the column's tags, or is nil. It is not
// changed.
// Takes added (map[string]string) which holds the tags to add.
// Takes precedent (bool) which is true when the added tags replace existing ones.
//
// Returns map[string]string which holds the merged tags.
func mergeStructTags(existing, added map[string]string, precedent bool) map[string]string {
	merged := maps.Clone(existing)
	if merged == nil {
		merged = make(map[string]string, len(added))
	}
	for key, value := range added {
		if _, present := merged[key]; present && !precedent {
			continue
		}
		merged[key] = value
	}
	return merged
}

// unusableTypeOverride reports a type override whose Go type the generator cannot write.
// An override that names no Go type and declares only its elements' nullability or its
// struct tags keeps the mapped Go type and is usable.
//
// Takes override (sqlmodel.TypeOverride) which is the configured override.
//
// Returns diag.SourceError which is the Q049 error naming the override and the problem.
// Returns bool which is true when the Go type is unusable.
func unusableTypeOverride(override sqlmodel.TypeOverride) (diag.SourceError, bool) {
	if override.GoName == "" && override.GoPackage == "" && (override.NonNullElements != nil || len(override.StructTags) > 0) {
		return diag.SourceError{}, false
	}
	goType := sqlmodel.GoType{Composite: nil, Package: override.GoPackage, Name: override.GoName}
	problem := directive.CheckGoTypeOverride(goType)
	if problem == "" {
		return diag.SourceError{}, false
	}
	target := fmt.Sprintf("SQL type %q", override.SQLTypeName)
	if override.Column != "" {
		target = fmt.Sprintf("column %q", override.Column)
	}
	written := goType.Name
	if goType.Package != "" {
		written = goType.Package + "." + goType.Name
	}
	return diagbuild.NewErrorBuilder("").At(
		diagbuild.TextSpan{Line: 0, Column: 0, EndLine: 0, EndColumn: 0},
		diag.CodeUnusableGoTypeOverride,
		fmt.Sprintf("type override for %s: go type %q is unusable: %s", target, written, problem),
	), true
}

// findReferencedColumn finds the column a reference names: in the schema it names, or,
// without a schema, in the first schema on the search path that holds the table.
//
// Takes catalogue (*sqlmodel.Catalogue) which is searched.
// Takes reference (ColumnReference) which names the column.
//
// Returns *sqlmodel.Column which aliases the catalogue's column, or nil when there is
// none.
func findReferencedColumn(catalogue *sqlmodel.Catalogue, reference ColumnReference) *sqlmodel.Column {
	if catalogue == nil || reference.Table == "" {
		return nil
	}
	identifiers := catalogue.Identifiers
	return catalogue.FindColumn(
		identifiers.Fold(reference.Schema, false),
		identifiers.Fold(reference.Table, false),
		identifiers.Fold(reference.Column, false),
	)
}

// writtenAsNamed writes a name as the catalogue holds it, as a configured column
// override's suggestion shows it.
//
// Takes name (string) which is the name.
//
// Returns string which is the name unchanged.
func writtenAsNamed(name string) string {
	return name
}
