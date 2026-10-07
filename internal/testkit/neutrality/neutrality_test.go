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

package neutrality

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const moduleLine = "module seagull.sh/seagull"

func repositoryRoot(t *testing.T) string {
	t.Helper()

	directory, err := filepath.Abs(".")
	require.NoError(t, err)
	for {
		content, readErr := os.ReadFile(filepath.Join(directory, "go.mod"))
		if readErr == nil && strings.Contains(string(content), moduleLine+"\n") {
			return directory
		}
		parent := filepath.Dir(directory)
		require.NotEqual(t, directory, parent, "the seagull.sh/seagull module root was not found")
		directory = parent
	}
}

func TestCoreHoldsNoEngineFacts(t *testing.T) {
	offences, err := scanCore(repositoryRoot(t))
	require.NoError(t, err)

	if len(offences) > 0 {
		found := make([]string, 0, len(offences))
		for _, item := range offences {
			found = append(found, fmt.Sprintf("%s:%d: %s %q", item.file, item.line, item.rule, item.fact))
		}
		assert.Failf(t, "the core names an engine fact", "the core names an engine fact. Seagull has no reference engine: engine type names, default "+
			"schemas, placeholder syntax, operators and engine families reach the core only through the dialect "+
			"contract (dialect.Info, the type catalogue or a dialect hook), never as literals or family switches in "+
			"%s. Move the fact into the dialect and read it from the contract. Offences:\n  %s",
			strings.Join(corePackageRoots, ", "), strings.Join(found, "\n  "))
	}
}

func TestClassifyLiteral(t *testing.T) {
	plain := literalPosition{}
	mapKey := literalPosition{isMapKey: true}
	decisive := literalPosition{isDecisive: true}
	testCases := []struct {
		name     string
		literal  string
		expected string
		position literalPosition
	}{
		{name: "postgres integer spelling", literal: "int4", position: plain, expected: ruleTypeName},
		{name: "upper-case postgres spelling", literal: "INT2", position: plain, expected: ruleTypeName},
		{name: "padded spelling", literal: " jsonb ", position: plain, expected: ruleTypeName},
		{name: "mysql spelling", literal: "mediumtext", position: plain, expected: ruleTypeName},
		{name: "duckdb spelling", literal: "HUGEINT", position: plain, expected: ruleTypeName},
		{name: "clickhouse wrapper", literal: "LowCardinality", position: plain, expected: ruleTypeName},
		{name: "clickhouse nullable", literal: "Nullable", position: plain, expected: ruleTypeName},
		{name: "clickhouse unsigned", literal: "UInt64", position: plain, expected: ruleTypeName},
		{name: "clickhouse wide integer", literal: "Int128", position: plain, expected: ruleTypeName},
		{name: "lower-cased clickhouse map key", literal: "lowcardinality", position: mapKey, expected: ruleTypeName},
		{name: "go unsigned type name", literal: "uint64", position: plain, expected: ""},
		{name: "reflect kind selector", literal: "Uint8", position: plain, expected: ""},
		{name: "go signed type name", literal: "int64", position: plain, expected: ""},
		{name: "go int8 as a value", literal: "int8", position: plain, expected: ""},
		{name: "int8 as a constant or comparison", literal: "int8", position: decisive, expected: ruleTypeName},
		{name: "int8 as a map key", literal: "int8", position: mapKey, expected: ruleTypeName},
		{name: "go nullable field", literal: "Int64", position: plain, expected: ""},
		{name: "lower-case nullable word", literal: "nullable", position: plain, expected: ""},
		{name: "go identifier name", literal: "UInt64", position: literalPosition{isGoName: true}, expected: ""},
		{name: "struct tag", literal: "public", position: literalPosition{isStructTag: true}, expected: ""},
		{name: "default schema", literal: "public", position: plain, expected: ruleSchema},
		{name: "family", literal: "postgres", position: plain, expected: ruleFamily},
		{name: "family in capitals", literal: "ClickHouse", position: plain, expected: ruleFamily},
		{name: "concatenation operator", literal: "||", position: decisive, expected: ruleOperator},
		{name: "json operator", literal: "->>", position: decisive, expected: ruleOperator},
		{name: "pattern operator", literal: "GLOB", position: decisive, expected: ruleOperator},
		{name: "pseudo-column", literal: "_ROWID_", position: decisive, expected: rulePseudoColumn},
		{name: "numbered placeholder", literal: "$1", position: plain, expected: rulePlaceholder},
		{name: "bare dollar", literal: "$", position: plain, expected: rulePlaceholder},
		{name: "question mark", literal: "?", position: plain, expected: rulePlaceholder},
		{name: "at placeholder", literal: "@p1", position: plain, expected: rulePlaceholder},
		{name: "typed curly placeholder", literal: "{id:Int32}", position: plain, expected: rulePlaceholder},
		{name: "lone brace", literal: "{", position: plain, expected: ""},
		{name: "standard like", literal: "LIKE", position: decisive, expected: ""},
		{name: "neutral word", literal: "integer", position: plain, expected: ""},
		{name: "empty", literal: "", position: plain, expected: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, classifyLiteral(testCase.literal, testCase.position))
		})
	}
}

func TestScanFileFindsFactsInContext(t *testing.T) {
	source := `package sample

import (
	"regexp"

	"seagull.sh/seagull/internal/dialect"
	pg "seagull.sh/seagull/internal/dialect/postgres"
)

const canonical = "int8"

type row struct {
	Name string ` + "`json:\"public\"`" + `
}

var names = map[string]string{"jsonb": "json", "uint64": "UInt64"}

func pick(info dialect.Info, family string) string {
	if family == pg.Family || info.Family == "" {
		return Ident("UInt8")
	}
	switch family {
	case "int8":
		return "int8"
	}
	tracker.AddImport("regexp")
	return "$1"
}
`
	directory := t.TempDir()
	path := filepath.Join(directory, "sample.go")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
	offences, err := scanFile(directory, path)
	require.NoError(t, err)

	summary := make([]string, 0, len(offences))
	for _, item := range offences {
		summary = append(summary, item.rule+" "+item.fact)
	}
	require.ElementsMatch(t, []string{
		ruleTypeName + " int8",
		ruleTypeName + " jsonb",
		ruleTypeName + " uint64",
		ruleTypeName + " UInt64",
		ruleFamily + " pg.Family",
		ruleFamily + " info.Family",
		ruleTypeName + " int8",
		rulePlaceholder + " $1",
	}, summary)
}

func TestCorePackageRootsExist(t *testing.T) {
	root := repositoryRoot(t)
	for _, packageRoot := range corePackageRoots {
		directory := filepath.Join(root, packageRoot)
		info, err := os.Stat(directory)
		require.NoError(t, err, "core package root %s is missing; update corePackageRoots", packageRoot)
		require.True(t, info.IsDir())
		goFiles, err := filepath.Glob(filepath.Join(directory, "*.go"))
		require.NoError(t, err)
		require.NotEmpty(t, goFiles, "core package root %s holds no Go files", packageRoot)
		_, err = parser.ParseFile(token.NewFileSet(), goFiles[0], nil, parser.PackageClauseOnly)
		require.NoError(t, err)
	}
}
