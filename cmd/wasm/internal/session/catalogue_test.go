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

package session_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/wasm/internal/session"
)

const readingsMigration = "CREATE TABLE measurements (\n" +
	"    id BIGINT NOT NULL,\n" +
	"    taken_on DATE NOT NULL,\n" +
	"    reading NUMERIC NOT NULL\n" +
	") PARTITION BY RANGE (taken_on);\n" +
	"CREATE TABLE measurements_2024 PARTITION OF measurements FOR VALUES FROM ('2024-01-01') TO ('2025-01-01');\n" +
	"COMMENT ON TABLE measurements IS 'Readings taken each day.';\n" +
	"COMMENT ON COLUMN measurements.reading IS 'The value read.';\n" +
	"CREATE SEQUENCE ticket_numbers;\n"

var viewCommentCases = []struct {
	want      map[string]string
	dialect   string
	migration string
}{
	{
		dialect: "postgres",
		migration: "CREATE TABLE readings (id BIGINT NOT NULL, reading NUMERIC NOT NULL);\n" +
			"CREATE VIEW recent_readings AS SELECT id, reading FROM readings;\n" +
			"CREATE MATERIALIZED VIEW reading_totals AS SELECT count(*) AS total FROM readings;\n" +
			"COMMENT ON VIEW recent_readings IS 'The latest readings.';\n" +
			"COMMENT ON COLUMN recent_readings.reading IS 'The value read.';\n" +
			"COMMENT ON MATERIALIZED VIEW reading_totals IS 'How many readings there are.';\n",
		want: map[string]string{"recent_readings": "The latest readings.", "recent_readings.reading": "The value read.",
			"reading_totals": "How many readings there are."},
	},
	{
		dialect: "duckdb",
		migration: "CREATE TABLE readings (id BIGINT NOT NULL, reading DECIMAL(10, 2) NOT NULL);\n" +
			"CREATE VIEW recent_readings AS SELECT id, reading FROM readings;\n" +
			"COMMENT ON VIEW recent_readings IS 'The latest readings.';\n" +
			"COMMENT ON COLUMN recent_readings.reading IS 'The value read.';\n",
		want: map[string]string{"recent_readings": "The latest readings.", "recent_readings.reading": "The value read."},
	},
}

func findTable(t *testing.T, schema session.SchemaView, name string) session.TableView {
	t.Helper()
	for _, table := range schema.Tables {
		if table.Name == name {
			return table
		}
	}
	require.Failf(t, "table not found", "no table named %q", name)
	return session.TableView{}
}

func TestCatalogueShowsPartitionsSequenceColumnsAndComments(t *testing.T) {
	t.Parallel()

	response := run(t, map[string]string{"db/migrations/0001_init.up.sql": readingsMigration}, session.Override{Dialect: "postgres"})

	require.True(t, response.OK, response.Error)
	require.Zero(t, response.Counts.Errors, response.Diagnostics)
	require.Len(t, response.Catalogue.Schemas, 1)
	schema := response.Catalogue.Schemas[0]

	parent := findTable(t, schema, "measurements")
	require.Equal(t, "Readings taken each day.", parent.Comment)
	require.Empty(t, parent.PartitionOf)
	require.Equal(t, []string{"", "", "The value read."}, []string{parent.Columns[0].Comment, parent.Columns[1].Comment, parent.Columns[2].Comment})

	partition := findTable(t, schema, "measurements_2024")
	require.Equal(t, "measurements", partition.PartitionOf)
	require.Equal(t, []string{"id", "taken_on", "reading"}, []string{partition.Columns[0].Name, partition.Columns[1].Name, partition.Columns[2].Name})

	require.Len(t, schema.Sequences, 1)
	sequence := schema.Sequences[0]
	require.Equal(t, "ticket_numbers", sequence.Name)
	names := make([]string, 0, len(sequence.Columns))
	for _, column := range sequence.Columns {
		names = append(names, column.Name+" "+column.Type)
	}
	require.Equal(t, []string{"last_value int8", "log_cnt int8", "is_called bool"}, names)
}

func TestCatalogueShowsTheCommentsOnViews(t *testing.T) {
	t.Parallel()

	for _, testCase := range viewCommentCases {
		t.Run(testCase.dialect, func(t *testing.T) {
			t.Parallel()

			response := run(t, map[string]string{"db/migrations/0001_init.up.sql": testCase.migration}, session.Override{Dialect: testCase.dialect})

			require.True(t, response.OK, response.Error)
			require.Zero(t, response.Counts.Errors, response.Diagnostics)
			require.Len(t, response.Catalogue.Schemas, 1)
			comments := map[string]string{}
			for _, view := range response.Catalogue.Schemas[0].Views {
				comments[view.Name] = view.Comment
				for _, column := range view.Columns {
					if column.Comment != "" {
						comments[view.Name+"."+column.Name] = column.Comment
					}
				}
			}
			require.Equal(t, testCase.want, comments)
		})
	}
}

func TestCatalogueShowsNoSequenceColumnsWhereTheEngineHasNone(t *testing.T) {
	t.Parallel()

	response := run(t, map[string]string{"db/migrations/0001_init.up.sql": "CREATE SEQUENCE ticket_numbers;\n"}, session.Override{Dialect: "duckdb"})

	require.True(t, response.OK, response.Error)
	require.Len(t, response.Catalogue.Schemas, 1)
	require.Len(t, response.Catalogue.Schemas[0].Sequences, 1)
	require.Empty(t, response.Catalogue.Schemas[0].Sequences[0].Columns)
	require.NotNil(t, response.Catalogue.Schemas[0].Sequences[0].Columns, "an empty list encodes as []")
}
