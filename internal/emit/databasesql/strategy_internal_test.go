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

package databasesql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestSQLStrategyWrapParameterAccessReturnsAccessVerbatimWhenNotClickHouse(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "GetByName",
			Command:  sqlmodel.QueryCommandOne,
			SQL:      "SELECT id FROM users WHERE name = $1",
			Filename: "users.sql",
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)

	querySource := findGeneratedFile(t, files, "users.sql.go")

	assert.NotContains(t, querySource, "clickhouse.Named")
	assert.NotContains(t, querySource, "sglClickHouseValue")
	for _, file := range files {
		assert.NotEqual(t, "clickhouse_values.go", file.Name, "no helper expected for the positional emitter")
	}
}
