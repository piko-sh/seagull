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

package thirdparty

import (
	"strconv"
	"strings"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
)

// modelsFile writes models.go: a struct per catalogue table the plan names a model for,
// and the named enum and struct types the plan declares. A package with neither gets no
// file.
//
// Returns emit.GeneratedFile which is the file.
// Returns bool which is false when the package needs no file.
// Returns error when the file does not format.
func (current *generation) modelsFile() (emit.GeneratedFile, bool, error) {
	naming := &current.request.Naming
	if len(naming.Models) == 0 && len(naming.Types) == 0 {
		return emit.GeneratedFile{}, false, nil
	}
	file := newSource(naming)
	declaresJSON := false
	for index := range naming.Models {
		if current.writeModel(file, &naming.Models[index]) {
			declaresJSON = true
		}
	}
	if declaresJSON {
		file.printf("type %s []byte\n\n", emit.InlineJSONTypeName)
	}
	for index := range naming.Types {
		current.writeNamedType(file, &naming.Types[index])
	}
	generated, err := file.generated(modelsFile, current.request)
	return generated, true, err
}

// writeModel writes the struct of one model: a field for each column the plan names, each
// with the tags fieldTags gives it, and the table's and columns' comments as doc comments
// when the style asks for them.
//
// Takes file (*source) which is models.go.
// Takes model (*emit.ModelNaming) which names the model and its fields.
//
// Returns bool which is true when a field holds the inline JSON type.
func (current *generation) writeModel(file *source, model *emit.ModelNaming) bool {
	table := current.request.Catalogue.LookupTable(model.Schema, model.Relation)
	if table == nil {
		return false
	}
	documented := current.request.Style.DocComments
	if documented {
		writeComment(file, "", table.Comment)
	}
	file.printf("type %s struct {\n", model.Type)
	var held []int
	var names []string
	var tags []map[string]string
	for index := range table.Columns {
		if model.Fields[index] != "" {
			held = append(held, index)
			names = append(names, table.Columns[index].Name)
			tags = append(tags, table.Columns[index].StructTags)
		}
	}
	written := fieldTags(&current.request.Style, names, tags)
	declaresJSON := false
	for position, index := range held {
		goType := current.modelColumnType(&table.Columns[index])
		declaresJSON = declaresJSON || goType.Package == "" && strings.TrimLeft(goType.Name, pointerPrefix) == emit.InlineJSONTypeName
		if documented {
			writeComment(file, "", table.Columns[index].Comment)
		}
		writeField(file, model.Fields[index], file.typeOf(goType), written[position])
	}
	file.printf(closeBlock + "\n")
	return declaresJSON
}

// modelColumnType returns the Go type a model holds a table column in: its go_type
// override, or the type the mapping gives it, nullable as its directive or the engine
// says.
//
// Takes column (*dialect.Column) which is the column.
//
// Returns emit.GoType which is the type.
func (current *generation) modelColumnType(column *dialect.Column) emit.GoType {
	nullable := column.Nullable
	if column.NullableOverride != nil {
		nullable = *column.NullableOverride
	}
	if column.GoTypeOverride != nil {
		return nullableOverride(*column.GoTypeOverride, nullable)
	}
	return current.request.Types.Resolve(column.ValueType(), nullable)
}

// writeNamedType writes a named type the plan declares: an enum as a string type with a
// constant per value and a Valid method, and a structured type as a struct.
//
// Takes file (*source) which is models.go.
// Takes named (*emit.NamedType) which is the type.
func (current *generation) writeNamedType(file *source, named *emit.NamedType) {
	if named.SQLType.Category != dialect.TypeCategoryEnum {
		file.printf("type %s struct {\n", named.Name)
		names := make([]string, len(named.SQLType.StructFields))
		for index := range named.SQLType.StructFields {
			names[index] = named.SQLType.StructFields[index].Name
		}
		written := fieldTags(&current.request.Style, names, nil)
		for index := range named.SQLType.StructFields {
			field := &named.SQLType.StructFields[index]
			writeField(file, named.Fields[index], file.typeOf(current.request.Types.Resolve(field.SQLType, field.SQLType.Nullable)), written[index])
		}
		file.printf(closeBlock + "\n")
		return
	}
	file.printf("type %s string\n\nconst (\n", named.Name)
	for index, value := range named.SQLType.EnumValues {
		file.printf("%s %s = %s\n", named.Constants[index], named.Name, strconv.Quote(value))
	}
	file.printf(")\n\nfunc (value %s) Valid() bool {\n", named.Name)
	if len(named.Constants) > 0 {
		file.printf("switch value {\ncase %s:\nreturn true\n}\n", strings.Join(named.Constants, argumentSeparator))
	}
	file.printf("return false\n" + closeBlock + "\n")
}
