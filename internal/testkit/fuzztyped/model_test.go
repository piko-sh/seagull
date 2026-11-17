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

package fuzztyped

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzbuilder"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

var engines = []string{enginePostgres, engineTimescale, engineCockroach, engineMySQL, engineMariaDB, engineSQLite, engineDuckDB, engineClickHouse}

func TestDecodersKeepTheirVersions(t *testing.T) {
	t.Parallel()
	for _, engine := range engines {
		data := []byte{Version, 0, 0, 5, 1, 2, 3, 4, 5, 6, 7, 8}
		_, ok := fuzzcase.DecodeScenario(engine, data)
		assert.False(t, ok, "%s: scenario decoder claimed version 15", engine)
		_, ok = fuzzcase.DecodeRuntime(engine, data)
		assert.False(t, ok, "%s: runtime decoder claimed version 15", engine)
		_, ok = fuzzbuilder.Decode(engine, data)
		assert.False(t, ok, "%s: builder decoder claimed version 15", engine)
		_, ok = Decode(engine, []byte{fuzzbuilder.Version})
		assert.False(t, ok, "%s: typed decoder claimed version 16", engine)
	}
}

func TestSeedsReachEveryFamilyAndQuery(t *testing.T) {
	t.Parallel()
	for _, engine := range engines {
		methods := make(map[string]bool)
		for _, seed := range Seeds(engine) {
			model, ok := Decode(engine, seed)
			require.True(t, ok, "%s: seed %v does not decode", engine, seed)
			require.NotEmpty(t, model.String())
			for _, call := range model.Calls {
				methods[call.Method] = true
			}
		}
		assert.Len(t, methods, QueryCount(engine), "%s: seeds call too few methods", engine)
	}
}

func TestEngineRulesRefuseWhatEnginesRefuse(t *testing.T) {
	t.Parallel()
	_, ok := storedFloat(engineMySQL, Float(math.NaN()))
	assert.False(t, ok, "MySQL stored NaN")
	value, _ := storedFloat(engineSQLite, Float(math.NaN()))
	assert.Equal(t, KindNull, value.Kind, "SQLite NaN is not NULL")
	value, _ = storedWall(enginePostgres, wall("1969-12-31T23:59:59.9999995"))
	assert.Equal(t, "1969-12-31T23:59:59.999999", value.Text, "pre-1970 floor")
	_, ok = storedInstant(engineMySQL, instant("1969-12-31T23:59:59Z"))
	assert.False(t, ok, "MySQL TIMESTAMP stored 1969")
	value, _ = storedInstant(engineClickHouse, instant("2024-01-01T00:30:00.123456789+14:00"))
	assert.Equal(t, "2023-12-31T10:30:00.123456789Z", value.Text)
	_, ok = storedArray(enginePostgres, List(List(Integer(1)), List(Integer(2), Integer(3))))
	assert.False(t, ok, "PostgreSQL stored a jagged array")
	assert.True(t, equalFloat(enginePostgres, Float(math.NaN()), Float(math.NaN())))
	assert.False(t, equalFloat(engineClickHouse, Float(math.NaN()), Float(math.NaN())))
	assert.True(t, equalFloat(engineDuckDB, Float(math.Copysign(0, -1)), Float(0)), "negative zero does not equal zero")
}

func TestValueEqualityIsExactAndKindChecked(t *testing.T) {
	t.Parallel()
	assert.False(t, Float(0).Equal(Float(math.Copysign(0, -1))), "zero equals negative zero")
	assert.False(t, Integer(1).Equal(Text("1")), "kinds compare equal")
	assert.False(t, List().Equal(Null()), "kinds compare equal")
	assert.True(t, Scalar(KindDecimal, "1.50").Equal(Scalar(KindDecimal, "1.5")), "decimals with trailing zeros differ")
}

func TestValueDescriptionsCutLongTextAtACharacter(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", textDescriptionBound+1)
	described := Text(long).String()
	assert.Contains(t, described, strings.Repeat("é", textDescriptionBound))
	assert.Contains(t, described, "41 characters")
	assert.Equal(t, `"é"`, Text("é").String())
}

func TestExpressionModel(t *testing.T) {
	t.Parallel()
	assert.True(t, like(engineSQLite, "AbC", "a%"))
	assert.False(t, like(enginePostgres, "AbC", "a%"))
	assert.True(t, like(engineMySQL, "é", "_"))
	assert.Equal(t, "Éa", changeCase(engineClickHouse, "Éa", unicode.ToLower))
	assert.Equal(t, "éa", changeCase(enginePostgres, "ÉA", unicode.ToLower))
	planted := Case{Engine: enginePostgres, Family: expressionFamily, Data: nil, Inserts: nil, Mode: ModeDatabaseSQL,
		Calls: []Call{{Method: "ExprRead", Arguments: nil, Names: nil, Shapes: nil, Want: [][]Value{{Integer(1), Boolean(true)}}, Exec: false}}}
	assert.NotEmpty(t, planted.Problems([]Observation{{Error: "", Rows: [][]Value{{Integer(1), Integer(1)}}}}), "an integer passed as a PostgreSQL boolean")
	planted.Engine = engineMySQL
	assert.Empty(t, planted.Problems([]Observation{{Error: "", Rows: [][]Value{{Integer(1), Integer(1)}}}}), "a MySQL integer truth value failed")
}

func TestExpressionFunctionsComeFromTheSpecs(t *testing.T) {
	t.Parallel()
	specs := map[string]string{enginePostgres: "postgres", engineTimescale: "postgres", engineCockroach: "postgres", engineMySQL: "mysql",
		engineMariaDB: "mysql", engineSQLite: "sqlite", engineDuckDB: "duckdb", engineClickHouse: "clickhouse"}
	for engine, directory := range specs {
		content, err := os.ReadFile(filepath.Join("..", "..", "dialect", directory, "builtin_functions.spec"))
		require.NoError(t, err)
		queries := Queries(engine)
		for _, function := range []string{"lower", "upper", "replace", "abs", "char_length", "length", "lengthUTF8", "nullif", "concat"} {
			if !strings.Contains(queries, function+"(") || strings.Contains(queries, "_"+function+"(") {
				continue
			}
			spelled := map[string]string{"replace": "replaceAll"}[function]
			if engine != engineClickHouse || spelled == "" {
				spelled = function
			}
			assert.Regexp(t, regexp.MustCompile(`(?im)^`+spelled+`\(`), string(content), "%s calls %s, which its spec lacks", engine, function)
		}
	}
}
