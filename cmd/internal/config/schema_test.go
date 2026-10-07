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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

func TestJSONSchemaFollowsValidation(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	require.NoError(t, json.Unmarshal(JSONSchema(), &schema))
	databases := objectAt(t, objectAt(t, objectAt(t, objectAt(t, schema, "properties"), "databases"), "items"), "properties")
	functions := objectAt(t, objectAt(t, objectAt(t, databases, "custom_functions"), "items"), "properties")
	limits := objectAt(t, objectAt(t, databases, "dialect_options"), "properties")
	goProperties := objectAt(t, objectAt(t, databases, "go"), "properties")

	tests := []struct {
		name     string
		property map[string]any
		key      string
		want     any
	}{
		{name: "data_access lists the accepted values", property: objectAt(t, functions, "data_access"), key: "enum", want: []any{"read_only", "modifies_data"}},
		{name: "nullable lists the accepted values", property: objectAt(t, functions, "nullable"), key: "enum", want: []any{"null_on_null", "never_null", "called_on_null"}},
		{name: "lock lists the accepted values", property: objectAt(t, objectAt(t, objectAt(t, databases, "migrate"), "properties"), "lock"), key: "enum", want: []any{"advisory", "table", "none"}},
		{name: "case lists the accepted values", property: objectAt(t, objectAt(t, objectAt(t, goProperties, "json_tags"), "properties"), "case"), key: "enum", want: []any{"snake", "camel", "pascal"}},
		{name: "dialect examples are the built-in dialects", property: objectAt(t, databases, "dialect"), key: "examples", want: anySlice(dialect_registry.Names())},
		{name: "max_parse_depth has the parser's ceiling", property: objectAt(t, limits, "max_parse_depth"), key: "maximum", want: float64(dialect_registry.MaxParseDepthCeiling)},
		{name: "max_tokens_per_statement has the parser's ceiling", property: objectAt(t, limits, "max_tokens_per_statement"), key: "maximum", want: float64(dialect_registry.MaxTokensPerStatementCeiling)},
		{name: "max_type_parse_depth has the deepest type", property: objectAt(t, limits, "max_type_parse_depth"), key: "maximum", want: float64(dialect.MaxTypeDepth)},
		{name: "a limit is never negative", property: objectAt(t, limits, "max_parse_depth"), key: "minimum", want: float64(0)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, testCase.property[testCase.key])
		})
	}
}

func anySlice(values []string) []any {
	converted := make([]any, 0, len(values))
	for _, value := range values {
		converted = append(converted, value)
	}
	return converted
}

func TestJSONSchemaDescribesEveryKey(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	require.NoError(t, json.Unmarshal(JSONSchema(), &schema))

	require.Equal(t, schemaDialect, schema["$schema"])
	properties := objectAt(t, schema, "properties")
	require.Contains(t, properties, "databases")
	require.Equal(t, "string", objectAt(t, properties, "$schema")["type"], "a file may name the schema it follows")
	databases := objectAt(t, objectAt(t, objectAt(t, properties, "databases"), "items"), "properties")
	require.Contains(t, databases, "dialect_options")
	require.Contains(t, objectAt(t, databases, "dialect")["examples"], "postgres")
	goProperties := objectAt(t, objectAt(t, databases, "go"), "properties")
	require.Contains(t, objectAt(t, goProperties, "driver")["examples"], "pgx")
	require.Equal(t, "boolean", objectAt(t, goProperties, "prepared")["type"])
	typeProperties := objectAt(t, objectAt(t, goProperties, "types"), "properties")
	require.Equal(t, "string", objectAt(t, typeProperties, "enum")["type"])
	require.Equal(t, "object", objectAt(t, typeProperties, "package_names")["type"], "a map is an object of strings")
	require.Equal(t, "string", objectAt(t, objectAt(t, typeProperties, "package_names"), "additionalProperties")["type"])
	require.Equal(t, "object", objectAt(t, goProperties, "struct_tags")["type"])
	require.Contains(t, objectAt(t, objectAt(t, objectAt(t, goProperties, "json_tags"), "properties"), "case")["enum"], "camel")
	require.Equal(t, "boolean", objectAt(t, objectAt(t, objectAt(t, goProperties, "interface"), "properties"), "enabled")["type"])
}

func objectAt(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()

	object, ok := value[key].(map[string]any)
	require.Truef(t, ok, "%s is not an object", key)
	return object
}
