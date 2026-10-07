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

package sqlfile

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
)

var foreignDownSectionCases = []struct {
	name    string
	content string
	want    ForeignDownSection
	found   bool
}{
	{name: "goose", content: "-- +goose Up\nCREATE TABLE p (id int);\n-- +goose Down\nDROP TABLE p;\n", want: ForeignDownSection{Marker: "-- +goose Down", Tool: "goose", Line: 3}, found: true},
	{name: "goose in lower case after a statement block", content: "-- +goose StatementEnd\n\n  --   +goose down  \n", want: ForeignDownSection{Marker: "--   +goose down", Tool: "goose", Line: 3}, found: true},
	{name: "sql-migrate", content: "-- +migrate Up\nCREATE TABLE p (id int);\n-- +migrate Down\r\nDROP TABLE p;\r\n", want: ForeignDownSection{Marker: "-- +migrate Down", Tool: "sql-migrate", Line: 3}, found: true},
	{name: "dbmate", content: "-- migrate:up\nCREATE TABLE p (id int);\n\n-- migrate:down\nDROP TABLE p;\n", want: ForeignDownSection{Marker: "-- migrate:down", Tool: "dbmate", Line: 4}, found: true},
	{name: "tern", content: "create table t1(id serial);\n\n---- create above / drop below ----\n\ndrop table t1;\n", want: ForeignDownSection{Marker: "---- create above / drop below ----", Tool: "tern", Line: 3}, found: true},
	{name: "an up marker alone", content: "-- +goose Up\nCREATE TABLE p (id int);\n-- migrate:up\n", want: ForeignDownSection{}, found: false},
	{name: "a marker after a statement on its line", content: "DROP TABLE p; -- +goose Down\n", want: ForeignDownSection{}, found: false},
	{name: "a longer word", content: "-- +goose Downgrade notes\n-- migrate:downstream\n", want: ForeignDownSection{}, found: false},
	{name: "an ordinary migration", content: "CREATE TABLE p (id int);\n", want: ForeignDownSection{}, found: false},
}

func TestFindForeignDownSectionFindsTheMarkersOfOtherTools(t *testing.T) {
	t.Parallel()

	for _, testCase := range foreignDownSectionCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			section, found := FindForeignDownSection([]byte(testCase.content))

			require.Equal(t, testCase.found, found)
			require.Equal(t, testCase.want, section)
		})
	}
}

func TestForeignDownSectionErrorPointsAtTheMarkerAndTheDownFile(t *testing.T) {
	t.Parallel()

	diagnostic := ForeignDownSectionError("001_init.up.sql", ForeignDownSection{Marker: "-- migrate:down", Tool: "dbmate", Line: 4})

	require.Equal(t, diag.CodeIgnoredMigrationFile, diagnostic.Code)
	require.Equal(t, diag.SeverityError, diagnostic.Severity)
	require.Equal(t, "001_init.up.sql", diagnostic.Filename)
	require.Equal(t, 4, diagnostic.Line)
	require.Equal(t, 1, diagnostic.Column)
	require.Equal(t, "001_init.up.sql holds a dbmate down section from line 4 (-- migrate:down); the migration runner "+
		"applies an up migration whole, so the statements after it would undo the migration as soon as it ran", diagnostic.Message)
	require.Equal(t, "move the statements from line 4 on into 001_init.down.sql", diagnostic.Suggestion)
}
