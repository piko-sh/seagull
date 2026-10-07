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

package config

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

const (
	// schemaDialect is the JSON Schema draft the generated schema declares.
	schemaDialect = "https://json-schema.org/draft/2020-12/schema"

	// schemaIdentifier is the canonical URL of the schema.
	schemaIdentifier = "https://seagull.sh/sgl.schema.json"

	// schemaKeyType is the JSON Schema keyword naming a value's type.
	schemaKeyType = "type"
)

var (
	// enumerations lists the closed sets of values for particular keys: the values
	// validation accepts, without the empty value that stands for the default.
	enumerations = map[string][]string{
		"lock":        withoutEmpty(locks),
		"nullable":    withoutEmpty(nullabilities),
		"data_access": withoutEmpty(dataAccesses),
		"case":        withoutEmpty(jsonCases),
	}

	// examples lists the stock values of keys a custom sgl binary can extend, such as a
	// dialect or emitter registered through the cli package.
	examples = map[string][]string{
		"driver":  {"database/sql", "pgx", "clickhouse"},
		"dialect": dialect_registry.Names(),
	}

	// maximums lists the largest value of particular integer keys: the ceilings the dialects
	// accept for their parser limits.
	maximums = map[string]int{
		"max_parse_depth":          dialect_registry.MaxParseDepthCeiling,
		"max_tokens_per_statement": dialect_registry.MaxTokensPerStatementCeiling,
		"max_type_parse_depth":     dialect.MaxTypeDepth,
	}
)

// JSONSchema returns the JSON Schema describing sgl.yaml.
//
// Returns []byte which is the indented schema document, ending in a newline.
func JSONSchema() []byte {
	schema := schemaFor(reflect.TypeFor[Config]())
	schema["$schema"] = schemaDialect
	schema["$id"] = schemaIdentifier
	schema["title"] = "sgl.yaml"
	encoded, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil
	}
	return append(encoded, '\n')
}

// schemaFor builds the schema for one Go type.
//
// Takes reflectType (reflect.Type) which is described.
//
// Returns map[string]any which is the schema object.
func schemaFor(reflectType reflect.Type) map[string]any {
	switch reflectType.Kind() {
	case reflect.Pointer:
		return schemaFor(reflectType.Elem())
	case reflect.Struct:
		properties := make(map[string]any, reflectType.NumField())
		for field := range reflectType.Fields() {
			name := fieldName(field)
			properties[name] = annotated(name, schemaFor(field.Type))
		}
		return map[string]any{schemaKeyType: "object", "properties": properties, "additionalProperties": false}
	case reflect.Slice:
		return map[string]any{schemaKeyType: "array", "items": schemaFor(reflectType.Elem())}
	case reflect.Map:
		return map[string]any{schemaKeyType: "object", "additionalProperties": schemaFor(reflectType.Elem())}
	case reflect.Bool:
		return map[string]any{schemaKeyType: "boolean"}
	case reflect.Int, reflect.Int64:
		return map[string]any{schemaKeyType: "integer"}
	default:
		return map[string]any{schemaKeyType: "string"}
	}
}

// annotated adds what the schema knows about a key's values to its property: the values
// it accepts, examples of them, or its range.
//
// Takes name (string) which is the key.
// Takes property (map[string]any) which is the key's schema; it is changed in place.
//
// Returns map[string]any which is property.
func annotated(name string, property map[string]any) map[string]any {
	if values, ok := enumerations[name]; ok {
		property["enum"] = values
	}
	if values, ok := examples[name]; ok {
		property["examples"] = values
	}
	if maximum, ok := maximums[name]; ok {
		property["minimum"] = 0
		property["maximum"] = maximum
	}
	return property
}

// fieldName returns the YAML key of a struct field.
//
// Takes field (reflect.StructField) which is inspected.
//
// Returns string which is the key.
func fieldName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
	return name
}

// knownKeys lists the keys the configuration accepts, for suggestions: those of one
// struct type, or every key.
//
// Takes typeName (string) which names a struct type of the configuration as a decoding
// error prints it, such as config.Go, or is empty for every key. A name that matches no
// type also gives every key.
//
// Returns []string which holds each key once, sorted.
func knownKeys(typeName string) []string {
	keys := make(map[string]bool)
	collectKeys(reflect.TypeFor[Config](), typeName, keys)
	if len(keys) == 0 && typeName != "" {
		return knownKeys("")
	}
	return slices.Sorted(maps.Keys(keys))
}

// collectKeys adds the YAML keys of reflectType and its nested structs to keys, or only
// those of the struct type named typeName when it is set.
//
// Takes reflectType (reflect.Type) which is walked.
// Takes typeName (string) which names the one struct type to collect, or is empty.
// Takes keys (map[string]bool) which receives the keys.
func collectKeys(reflectType reflect.Type, typeName string, keys map[string]bool) {
	switch reflectType.Kind() {
	case reflect.Pointer, reflect.Slice:
		collectKeys(reflectType.Elem(), typeName, keys)
	case reflect.Struct:
		collect := typeName == "" || reflectType.String() == typeName
		for field := range reflectType.Fields() {
			if collect {
				keys[fieldName(field)] = true
			}
			collectKeys(field.Type, typeName, keys)
		}
	default:
	}
}

// withoutEmpty copies a list of accepted values without the empty value.
//
// Takes values ([]string) which may hold the empty value.
//
// Returns []string which holds the other values in order.
func withoutEmpty(values []string) []string {
	return slices.DeleteFunc(slices.Clone(values), func(value string) bool { return value == "" })
}
