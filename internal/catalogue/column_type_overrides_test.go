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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func overrideCatalogue() *sqlmodel.Catalogue {
	return &sqlmodel.Catalogue{
		DefaultSchema: "public",
		SearchPath:    []string{"public"},
		Identifiers:   sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false},
		Schemas: map[string]*sqlmodel.Schema{
			"public": {Tables: map[string]*sqlmodel.Table{
				"users": {Name: "users", Columns: []sqlmodel.Column{{Name: "id"}, {Name: "settings"}}},
			}},
			"audit": {Tables: map[string]*sqlmodel.Table{
				"users": {Name: "users", Columns: []sqlmodel.Column{{Name: "id"}, {Name: "settings"}}},
			}},
			"empty": nil,
		},
	}
}

func TestParseColumnReference(t *testing.T) {
	t.Parallel()

	reference, err := ParseColumnReference("users.settings")
	require.NoError(t, err)
	assert.Equal(t, ColumnReference{Table: "users", Column: "settings"}, reference)

	reference, err = ParseColumnReference("audit.users.settings")
	require.NoError(t, err)
	assert.Equal(t, ColumnReference{Schema: "audit", Table: "users", Column: "settings"}, reference)

	for _, bad := range []string{"", "users", "a.b.c.d", ".settings", "users."} {
		_, err = ParseColumnReference(bad)
		require.ErrorIs(t, err, fault.ErrInvalidConfig, bad)
	}
}

func TestApplyColumnTypeOverrides(t *testing.T) {
	t.Parallel()

	catalogue := overrideCatalogue()
	directive := &sqlmodel.GoType{Package: "example.com/directive", Name: "Kept"}
	catalogue.Schemas["public"].Tables["users"].Columns[0].GoTypeOverride = directive

	diagnostics := ApplyColumnTypeOverrides(catalogue, []sqlmodel.TypeOverride{
		{SQLTypeName: "text", GoName: "string"},
		{Column: "AUDIT.Users.Settings", GoPackage: "example.com/audit", GoName: "Settings"},
		{Column: "public.users.id", GoPackage: "example.com/ignored", GoName: "Ignored"},
		{Column: "users.setings", GoPackage: "example.com/missing", GoName: "Missing"},
		{Column: "nowhere.users.settings", GoPackage: "example.com/missing", GoName: "Missing"},
		{Column: "malformed", GoPackage: "example.com/missing", GoName: "Missing"},
	})

	audit := catalogue.Schemas["audit"].Tables["users"].Columns[1]
	require.NotNil(t, audit.GoTypeOverride)
	assert.Equal(t, sqlmodel.GoType{Package: "example.com/audit", Name: "Settings"}, *audit.GoTypeOverride)
	assert.Nil(t, catalogue.Schemas["public"].Tables["users"].Columns[1].GoTypeOverride)
	assert.Same(t, directive, catalogue.Schemas["public"].Tables["users"].Columns[0].GoTypeOverride)

	require.Len(t, diagnostics, 3)
	for _, diagnostic := range diagnostics {
		assert.Equal(t, diag.CodeUnknownOverrideMigrationColumn, diagnostic.Code)
		assert.Equal(t, diag.SeverityError, diagnostic.Severity)
	}
	assert.Equal(t, "users.settings", diagnostics[0].Suggestion)
	assert.Empty(t, ApplyColumnTypeOverrides(nil, nil))
	assert.Len(t, ApplyColumnTypeOverrides(nil, []sqlmodel.TypeOverride{{Column: "users.id", GoName: "X"}}), 1)
}

func TestApplyColumnTypeOverridesReportsUnusableGoTypes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		override    sqlmodel.TypeOverride
		wantMessage string
	}{
		{
			name:        "spaced name on a column",
			override:    sqlmodel.TypeOverride{Column: "users.settings", GoPackage: "", GoName: "bad type"},
			wantMessage: `type override for column "users.settings": go type "bad type" is unusable: "bad type" is not a Go type name`,
		},
		{
			name:        "map on a column",
			override:    sqlmodel.TypeOverride{Column: "users.settings", GoPackage: "", GoName: "map[string]int"},
			wantMessage: `"map[string]int" is not a Go type name`,
		},
		{
			name:        "numeric name on a column",
			override:    sqlmodel.TypeOverride{Column: "users.settings", GoPackage: "example.com/menu", GoName: "123"},
			wantMessage: `go type "example.com/menu.123" is unusable`,
		},
		{
			name:        "spaced name on a SQL type",
			override:    sqlmodel.TypeOverride{SQLTypeName: "text", GoPackage: "", GoName: "not a type"},
			wantMessage: `type override for SQL type "text": go type "not a type" is unusable`,
		},
		{
			name:        "invalid import path on a SQL type",
			override:    sqlmodel.TypeOverride{SQLTypeName: "text", GoPackage: "example.com/my menu", GoName: "Text"},
			wantMessage: `"example.com/my menu" is not a valid import path`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := overrideCatalogue()
			diagnostics := ApplyColumnTypeOverrides(catalogue, []sqlmodel.TypeOverride{testCase.override})

			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeUnusableGoTypeOverride, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Contains(t, diagnostics[0].Message, testCase.wantMessage)
			require.Nil(t, catalogue.Schemas["public"].Tables["users"].Columns[1].GoTypeOverride)
		})
	}
}

func TestApplyColumnTypeOverridesFollowsTheSearchPath(t *testing.T) {
	t.Parallel()

	catalogue := overrideCatalogue()
	diagnostics := ApplyColumnTypeOverrides(catalogue, []sqlmodel.TypeOverride{
		{Column: "users.settings", GoPackage: "example.com/public", GoName: "Settings"},
	})

	require.Empty(t, diagnostics)
	require.NotNil(t, catalogue.Schemas["public"].Tables["users"].Columns[1].GoTypeOverride)
	assert.Nil(t, catalogue.Schemas["audit"].Tables["users"].Columns[1].GoTypeOverride,
		"an unqualified override names the table the search path finds, not the first schema by name")
}

func TestAColumnTypeOverrideReachesThePartitionsOfItsTable(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t, partitionedMeasurements+`
CREATE TABLE measurements_2027 PARTITION OF measurements FOR VALUES FROM ('2027-01-01') TO ('2028-01-01')
	PARTITION BY RANGE (id);
CREATE TABLE measurements_2027_low PARTITION OF measurements_2027 FOR VALUES FROM (0) TO (100);
CREATE TABLE unrelated (reading numeric);
`)
	require.Empty(t, diagnostics)

	overrideDiagnostics := ApplyColumnTypeOverrides(catalogue, []sqlmodel.TypeOverride{
		{Column: "measurements.reading", GoPackage: "example.com/readings", GoName: "Reading"},
	})

	require.Empty(t, overrideDiagnostics)
	want := sqlmodel.GoType{Package: "example.com/readings", Name: "Reading"}
	for _, name := range []string{"measurements", "measurements_2026", "measurements_2027", "measurements_2027_low"} {
		column := catalogue.FindColumn("", name, "reading")
		require.NotNil(t, column.GoTypeOverride, name)
		require.Equal(t, want, *column.GoTypeOverride, name)
	}
	require.Nil(t, catalogue.FindColumn("", "unrelated", "reading").GoTypeOverride)
}

func TestAColumnTypeOverrideEndsOnPartitionsThatNameEachOther(t *testing.T) {
	t.Parallel()

	partitioned := func(name, parent string) *sqlmodel.Table {
		return &sqlmodel.Table{Name: name, Schema: "public", Columns: []sqlmodel.Column{{Name: "reading"}},
			PartitionOf: &sqlmodel.TableName{Schema: "public", Name: parent}}
	}
	testCases := []struct {
		name   string
		tables []*sqlmodel.Table
		want   []string
	}{
		{name: "a table partitioning itself", tables: []*sqlmodel.Table{partitioned("loop", "loop")}, want: []string{"loop"}},
		{
			name:   "two tables partitioning each other",
			tables: []*sqlmodel.Table{partitioned("loop", "other"), partitioned("other", "loop")},
			want:   []string{"loop", "other"},
		},
		{
			name: "a diamond of partitions",
			tables: []*sqlmodel.Table{
				{Name: "loop", Schema: "public", Columns: []sqlmodel.Column{{Name: "reading"}}},
				partitioned("left", "loop"), partitioned("right", "loop"), partitioned("bottom", "left"),
			},
			want: []string{"bottom", "left", "loop", "right"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tables := make(map[string]*sqlmodel.Table, len(testCase.tables))
			for _, table := range testCase.tables {
				tables[table.Name] = table
			}
			catalogue := &sqlmodel.Catalogue{
				DefaultSchema: "public",
				SearchPath:    []string{"public"},
				Identifiers:   sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false},
				Schemas:       map[string]*sqlmodel.Schema{"public": {Name: "public", Tables: tables}},
			}

			var diagnostics []diag.SourceError
			scaling.Within(t, 100*time.Millisecond, func(context.Context) {
				diagnostics = ApplyColumnTypeOverrides(catalogue, []sqlmodel.TypeOverride{
					{Column: "loop.reading", GoPackage: "example.com/readings", GoName: "Reading"},
				})
			})

			require.Empty(t, diagnostics)
			var overridden []string
			for name, table := range tables {
				if table.Columns[0].GoTypeOverride != nil {
					overridden = append(overridden, name)
				}
			}
			assert.ElementsMatch(t, testCase.want, overridden)
		})
	}
}
