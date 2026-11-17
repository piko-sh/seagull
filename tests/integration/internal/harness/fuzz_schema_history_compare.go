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

package harness

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/testkit/fuzzschema"
	"seagull.sh/seagull/sdk/dialect"
)

const (
	// historyMaterialised is the property only the model and the database observe, as
	// Seagull's catalogue keeps materialised views with views.
	historyMaterialised = " materialised"

	// historyTrue spells a true property.
	historyTrue = "true"

	// historyKeySeparator separates keys in a property that lists several.
	historyKeySeparator = "|"

	// constraintPrimaryKey is the replayed name of a primary key constraint.
	constraintPrimaryKey = "primary-key"

	// constraintUnique is the replayed name of a unique constraint.
	constraintUnique = "unique"

	// constraintCheck is the replayed name of a check constraint.
	constraintCheck = "check"

	// constraintForeignKey is the replayed name of a foreign key constraint.
	constraintForeignKey = "foreign-key"
)

// historyFamilies maps each replayed type category to the model's coarse family. The
// temporal and array categories need more than the category and are handled apart.
var historyFamilies = map[dialect.SQLTypeCategory]string{
	dialect.TypeCategoryInteger: fuzzschema.FamilyInteger,
	dialect.TypeCategoryFloat:   fuzzschema.FamilyFloat,
	dialect.TypeCategoryDecimal: fuzzschema.FamilyDecimal,
	dialect.TypeCategoryBoolean: fuzzschema.FamilyBoolean,
	dialect.TypeCategoryText:    fuzzschema.FamilyText,
	dialect.TypeCategoryBinary:  fuzzschema.FamilyBinary,
	dialect.TypeCategoryJSON:    fuzzschema.FamilyJSON,
	dialect.TypeCategoryUUID:    fuzzschema.FamilyUUID,
	dialect.TypeCategoryEnum:    fuzzschema.FamilyEnum,
	dialect.TypeCategoryArray:   fuzzschema.FamilyArray,
}

// historyProperties flattens a schema into comparable named properties.
//
// Takes schema (fuzzschema.Schema) which is one observation.
// Takes checkNames (bool) which compares CHECK constraint names, or only their number
// where the engine names them itself.
//
// Returns map[string]string which maps each property to its value.
func historyProperties(schema fuzzschema.Schema, checkNames bool) map[string]string {
	properties := map[string]string{"sequences": strings.Join(sortedStrings(schema.Sequences), historySeparator)}
	var relations []string
	for index := range schema.Relations {
		relation := &schema.Relations[index]
		key := relation.Key()
		table := relation.Kind == fuzzschema.KindTable
		relations = append(relations, key+historyRelationSuffix(table))
		properties[key+" columns"] = strings.Join(historyColumnNames(relation.Columns), historySeparator)
		for _, column := range relation.Columns {
			properties[key+"."+column.Name+" family"] = column.Family
			if table {
				properties[key+"."+column.Name+" nullable"] = strconv.FormatBool(column.Nullable)
				properties[key+"."+column.Name+" default"] = strconv.FormatBool(column.Default)
				properties[key+"."+column.Name+" generated"] = strconv.FormatBool(column.Generated)
			}
		}
		if !table {
			properties[key+historyMaterialised] = strconv.FormatBool(relation.Kind == fuzzschema.KindMaterialisedView)
			continue
		}
		properties[key+" primary key"] = strings.Join(relation.PrimaryKey, historySeparator)
		properties[key+" uniques"] = historyKeys(relation.Uniques, relation.PrimaryKey)
		properties[key+" checks"] = strconv.Itoa(len(relation.Checks))
		if checkNames {
			properties[key+" checks"] = strings.Join(sortedStrings(relation.Checks), historySeparator)
		}
		properties[key+" foreign keys"] = historyForeignKeys(relation.ForeignKeys)
		properties[key+" partition of"] = relation.PartitionOf
	}
	properties["relations"] = strings.Join(sortedStrings(relations), ",")
	for name, values := range schema.Enums {
		properties["enum "+name] = strings.Join(values, historySeparator)
	}
	return properties
}

// historyRelationSuffix marks a relation as a table or a view.
//
// Takes table (bool) which is true for a table.
//
// Returns string which is the suffix.
func historyRelationSuffix(table bool) string {
	if table {
		return ":table"
	}
	return ":view"
}

// historyColumnNames lists column names in order.
//
// Takes columns ([]fuzzschema.Column) which are the columns.
//
// Returns []string which holds their names.
func historyColumnNames(columns []fuzzschema.Column) []string {
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name)
	}
	return names
}

// historyKeys spells unique keys other than the primary key, sorted and without repeats.
//
// Takes keys ([][]string) which hold each key's columns.
// Takes primary ([]string) which is the primary key.
//
// Returns string which is comparable across observations.
func historyKeys(keys [][]string, primary []string) string {
	var spelled []string
	for _, key := range keys {
		if len(key) != 0 && !slices.Equal(key, primary) {
			spelled = append(spelled, strings.Join(key, historySeparator))
		}
	}
	return strings.Join(slices.Compact(sortedStrings(spelled)), historyKeySeparator)
}

// historyForeignKeys spells foreign keys, sorted.
//
// Takes keys ([]fuzzschema.ForeignKey) which are the foreign keys.
//
// Returns string which is comparable across observations.
func historyForeignKeys(keys []fuzzschema.ForeignKey) string {
	spelled := make([]string, 0, len(keys))
	for _, key := range keys {
		spelled = append(spelled, fmt.Sprintf("%s->%s(%s)", strings.Join(key.Columns, historySeparator), key.Table, strings.Join(key.References, historySeparator)))
	}
	return strings.Join(sortedStrings(spelled), historyKeySeparator)
}

// sortedStrings returns a sorted copy.
//
// Takes values ([]string) which are the values.
//
// Returns []string which is sorted.
func sortedStrings(values []string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return result
}

// compareHistory compares the three observations property by property.
//
// A property where the model and the database agree and Seagull differs is a Seagull
// finding. One where the model and the database differ is a model or harness fault,
// whatever Seagull says.
//
// Takes model (map[string]string) which is the model's prediction.
// Takes live (map[string]string) which is the database's catalogue.
// Takes replayed (map[string]string) which is Seagull's catalogue.
//
// Returns []string which describes each disagreement.
func compareHistory(model, live, replayed map[string]string) []string {
	keys := slices.Collect(maps.Keys(model))
	keys = append(keys, slices.Collect(maps.Keys(live))...)
	keys = append(keys, slices.Collect(maps.Keys(replayed))...)
	var problems []string
	for _, key := range slices.Compact(sortedStrings(keys)) {
		expected, observed := model[key], live[key]
		if expected != observed {
			problems = append(problems, fmt.Sprintf("\n[model] %s: model %q, database %q, Seagull %q", key, expected, observed, replayed[key]))
			continue
		}
		if strings.HasSuffix(key, historyMaterialised) {
			continue
		}
		if seagullValue := replayed[key]; seagullValue != observed {
			problems = append(problems, fmt.Sprintf("\n[seagull] %s: model and database %q, Seagull %q", key, observed, seagullValue))
		}
	}
	return problems
}

// seagullHistorySchema projects Seagull's replayed catalogue onto the model's terms.
//
// Takes catalogue (*dialect.Catalogue) which is the replayed catalogue.
// Takes namespace (string) which is the second schema's name in the files.
//
// Returns fuzzschema.Schema which holds the history's objects.
func seagullHistorySchema(catalogue *dialect.Catalogue, namespace string) fuzzschema.Schema {
	schema := fuzzschema.Schema{Enums: map[string][]string{}, Relations: nil, Sequences: nil}
	for name, contents := range catalogue.Schemas {
		scope := historyScope(name, catalogue.DefaultSchema, namespace)
		for _, table := range contents.Tables {
			if strings.HasPrefix(table.Name, historyPrefix) {
				schema.Relations = append(schema.Relations, seagullHistoryTable(table, catalogue.DefaultSchema, namespace, scope))
			}
		}
		for _, view := range contents.Views {
			if strings.HasPrefix(view.Name, historyPrefix) {
				schema.Relations = append(schema.Relations, fuzzschema.Relation{Namespace: scope, Name: view.Name, Kind: fuzzschema.KindView,
					PartitionOf: "", Columns: seagullHistoryColumns(view.Columns), PrimaryKey: nil, Uniques: nil, Checks: nil, ForeignKeys: nil})
			}
		}
		seagullHistoryObjects(&schema, contents, scope)
	}
	return schema
}

// seagullHistoryObjects projects a replayed schema's sequences and enum types.
//
// Takes schema (*fuzzschema.Schema) which receives them.
// Takes contents (*dialect.Schema) which is the replayed schema.
// Takes scope (string) which is its model namespace.
func seagullHistoryObjects(schema *fuzzschema.Schema, contents *dialect.Schema, scope string) {
	for _, sequence := range contents.Sequences {
		if strings.HasPrefix(sequence.Name, historyPrefix) {
			schema.Sequences = append(schema.Sequences, historyQualify(scope, sequence.Name))
		}
	}
	for _, enum := range contents.Enums {
		if strings.HasPrefix(enum.Name, historyPrefix) {
			schema.Enums[enum.Name] = slices.Clone(enum.Values)
		}
	}
}

// historyScope maps a schema name to the model's namespace.
//
// Takes name (string) which is the observed schema name.
// Takes defaultSchema (string) which is the default schema's name.
// Takes namespace (string) which is the second schema's name.
//
// Returns string which is empty for the default schema, the model's namespace for the
// second, and the marked name of any other, so that it surfaces as a difference.
func historyScope(name, defaultSchema, namespace string) string {
	switch name {
	case defaultSchema, "":
		return ""
	case namespace:
		return fuzzschema.Namespace
	default:
		return "unexpected-" + name
	}
}

// historyQualify qualifies a name outside the default schema.
//
// Takes scope (string) which is the model's namespace or empty.
// Takes name (string) which is the object's name.
//
// Returns string which is the qualified name.
func historyQualify(scope, name string) string {
	if scope == "" {
		return name
	}
	return scope + "." + name
}

// seagullHistoryTable projects one replayed table.
//
// Takes table (*dialect.Table) which is the table.
// Takes defaultSchema (string) which is the default schema's name.
// Takes namespace (string) which is the second schema's name.
// Takes scope (string) which is the table's model namespace.
//
// Returns fuzzschema.Relation which holds the table's observable structure.
func seagullHistoryTable(table *dialect.Table, defaultSchema, namespace, scope string) fuzzschema.Relation {
	relation := fuzzschema.Relation{Namespace: scope, Name: table.Name, Kind: fuzzschema.KindTable, PartitionOf: "",
		Columns: seagullHistoryColumns(table.Columns), PrimaryKey: slices.Clone(table.PrimaryKey), Uniques: nil, Checks: nil, ForeignKeys: nil}
	if table.PartitionOf != nil {
		relation.PartitionOf = historyQualify(historyScope(table.PartitionOf.Schema, defaultSchema, namespace), table.PartitionOf.Name)
	}
	for index := range table.Constraints {
		constraint := &table.Constraints[index]
		switch constraint.Kind.String() {
		case constraintPrimaryKey:
			if len(relation.PrimaryKey) == 0 {
				relation.PrimaryKey = slices.Clone(constraint.Columns)
			}
		case constraintUnique:
			relation.Uniques = append(relation.Uniques, slices.Clone(constraint.Columns))
		case constraintCheck:
			relation.Checks = append(relation.Checks, constraint.Name)
		case constraintForeignKey:
			referenced := constraint.ForeignTable
			if schemaName, name, qualified := strings.Cut(referenced, "."); qualified {
				referenced = historyQualify(historyScope(schemaName, defaultSchema, namespace), name)
			}
			relation.ForeignKeys = append(relation.ForeignKeys, fuzzschema.ForeignKey{Table: referenced, Columns: slices.Clone(constraint.Columns),
				References: slices.Clone(constraint.ForeignColumns)})
		default:
		}
	}
	for position := range table.Indexes {
		if tableIndex := &table.Indexes[position]; tableIndex.IsUnique && !tableIndex.IsPrimary {
			relation.Uniques = append(relation.Uniques, slices.Clone(tableIndex.Columns))
		}
	}
	return relation
}

// seagullHistoryColumns projects replayed columns, leaving out the hidden ones a query
// cannot select, unless the engine generates them.
//
// Takes columns ([]dialect.Column) which are the columns.
//
// Returns []fuzzschema.Column which holds their observable attributes.
func seagullHistoryColumns(columns []dialect.Column) []fuzzschema.Column {
	result := make([]fuzzschema.Column, 0, len(columns))
	for index := range columns {
		column := &columns[index]
		if column.Hidden && !column.IsGenerated {
			continue
		}
		result = append(result, fuzzschema.Column{Name: column.Name, Family: seagullHistoryFamily(column.SQLType, column.ArrayDimensions),
			Nullable: column.Nullable, Default: column.HasDefault, Generated: column.IsGenerated})
	}
	return result
}

// seagullHistoryFamily maps a replayed type to the model's coarse family.
//
// Takes sqlType (dialect.SQLType) which is the column's type.
// Takes dimensions (int) which counts its array dimensions.
//
// Returns string which is the family, or the engine name of an unmapped type.
func seagullHistoryFamily(sqlType dialect.SQLType, dimensions int) string {
	if dimensions > 0 {
		return fuzzschema.FamilyArray
	}
	if family, ok := historyFamilies[sqlType.Category]; ok {
		return family
	}
	if sqlType.Category == dialect.TypeCategoryTemporal {
		switch {
		case sqlType.Temporal == dialect.TemporalKindDate:
			return fuzzschema.FamilyDate
		case sqlType.Temporal == dialect.TemporalKindTimestamp && sqlType.TimeZone:
			return fuzzschema.FamilyTimestampTZ
		case sqlType.Temporal == dialect.TemporalKindTimestamp:
			return fuzzschema.FamilyTimestamp
		}
	}
	return "other:" + sqlType.EngineName
}
