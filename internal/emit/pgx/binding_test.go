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

package pgx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func binaryOverrideQuery(name string, command sqlmodel.QueryCommand) *sqlmodel.AnalysedQuery {
	identifier := &sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"}
	return &sqlmodel.AnalysedQuery{
		Name:     name,
		Command:  command,
		SQL:      "INSERT INTO tokens (id, raw, label) VALUES ($1, $2, $3)",
		Filename: "tokens.sql",
		Parameters: []sqlmodel.QueryParameter{
			{Number: 1, Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryBinary, EngineName: "bytea"}, GoTypeOverride: identifier},
			{Number: 2, Name: "raw", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryBinary, EngineName: "bytea"}},
			{Number: 3, Name: "label", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}, GoTypeOverride: identifier},
		},
	}
}

func TestABinaryOverrideBindsTheBytesItMarshalsItselfTo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		command   sqlmodel.QueryCommand
		wantBound string
		wantPlain []string
	}{
		{name: "a statement", command: sqlmodel.QueryCommandExec, wantBound: "sglDriverBinary{value: params.ID}",
			wantPlain: []string{"params.Raw,", "params.Label)"}},
		{name: "a batch", command: sqlmodel.QueryCommandBatch, wantBound: "sglDriverBinary{value: item.ID}",
			wantPlain: []string{"item.Raw,", "item.Label)"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, err := emitQueriesFor(t.Context(), New(), "db", []*sqlmodel.AnalysedQuery{binaryOverrideQuery("AddToken", testCase.command)}, defaultMappings())
			require.NoError(t, err)

			querySource := findGeneratedFile(t, files, "tokens.sql.go")
			helperSource := findGeneratedFile(t, files, "driver_values.go")
			require.Contains(t, querySource, testCase.wantBound)
			for _, plain := range testCase.wantPlain {
				require.Contains(t, querySource, plain)
			}
			require.Contains(t, helperSource, "func (wrapped sglDriverBinary) Value() (driver.Value, error)")
		})
	}
}

func TestAPackageWithoutBinaryOverridesHasNoDriverValues(t *testing.T) {
	t.Parallel()

	query := binaryOverrideQuery("AddToken", sqlmodel.QueryCommandExec)
	query.Parameters[0].GoTypeOverride = nil
	files, err := emitQueriesFor(t.Context(), New(), "db", []*sqlmodel.AnalysedQuery{query}, defaultMappings())
	require.NoError(t, err)

	for _, file := range files {
		require.NotEqual(t, "driver_values.go", file.Name)
	}
}

func findGeneratedFile(t *testing.T, files []sqlmodel.GeneratedFile, name string) string {
	t.Helper()
	for _, file := range files {
		if file.Name == name {
			return string(file.Content)
		}
	}
	require.Failf(t, "missing generated file", "expected %q among %d files", name, len(files))
	return ""
}
