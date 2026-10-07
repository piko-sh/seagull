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

package emitkit_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func upsertBirds(id, name sqlmodel.SQLSegment) []sqlmodel.SQLSegment {
	return []sqlmodel.SQLSegment{
		text("INSERT INTO birds (id, name, rank) VALUES "),
		values(text("("), id, text(", lower("), name, text("), "), id, text(" + 1, '$9 ?')")),
		text(" ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name"),
	}
}

func writeRows(row emitkit.Row, count int) string {
	rows := make([]string, count)
	for index := range rows {
		var builder strings.Builder
		for part, text := range row.Parts {
			builder.WriteString(text)
			if part == len(row.Slots) {
				break
			}
			switch row.Kind {
			case dialect.PlaceholderNumbered:
				builder.WriteString(strconv.Itoa(index*len(row.Arguments) + row.Slots[part] + 1))
			case dialect.PlaceholderNamed:
				builder.WriteString(strconv.Itoa(index))
			}
		}
		rows[index] = builder.String()
	}
	return strings.Join(rows, ", ")
}

func TestRowTemplateRepeatsTheValuesRowInEachForm(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		renderer  emitkit.Renderer
		name      string
		rows      string
		parts     []string
		slots     []int
		arguments []int
		names     []string
		segments  []sqlmodel.SQLSegment
	}{
		{
			name:      "a numbered form numbers each row's arguments after the rows before it",
			renderer:  renderer(postgresPlaceholders, postgresPlaceholders.Execution),
			segments:  upsertBirds(parameter(0), parameter(1)),
			parts:     []string{"($", ", lower($", "), $", " + 1, '$9 ?')"},
			slots:     []int{0, 1, 0},
			arguments: []int{0, 1},
			rows:      "($1, lower($2), $1 + 1, '$9 ?'), ($3, lower($4), $3 + 1, '$9 ?')",
		},
		{
			name:      "a numbered form numbers by parameter, not by where the row writes it",
			renderer:  renderer(sqlitePlaceholders, sqlitePlaceholders.Execution),
			segments:  upsertBirds(parameter(1), parameter(0)),
			parts:     []string{"(?", ", lower(?", "), ?", " + 1, '$9 ?')"},
			slots:     []int{1, 0, 1},
			arguments: []int{0, 1},
			rows:      "(?2, lower(?1), ?2 + 1, '$9 ?'), (?4, lower(?3), ?4 + 1, '$9 ?')",
		},
		{
			name:      "the martian form numbers after its prefix",
			renderer:  renderer(martianPlaceholders, martianPlaceholders.Execution),
			segments:  upsertBirds(parameter(0), parameter(1)),
			parts:     []string{"(@p", ", lower(@p", "), @p", " + 1, '$9 ?')"},
			slots:     []int{0, 1, 0},
			arguments: []int{0, 1},
			rows:      "(@p1, lower(@p2), @p1 + 1, '$9 ?'), (@p3, lower(@p4), @p3 + 1, '$9 ?')",
		},
		{
			name:      "an anonymous form writes a fixed row binding every placeholder in turn",
			renderer:  renderer(mysqlPlaceholders, mysqlPlaceholders.Execution),
			segments:  upsertBirds(parameter(0), parameter(1)),
			parts:     []string{"(?, lower(?), ? + 1, '$9 ?')"},
			arguments: []int{0, 1, 0},
			rows:      "(?, lower(?), ? + 1, '$9 ?'), (?, lower(?), ? + 1, '$9 ?')",
		},
		{
			name:      "a named form ends each name with the row's index",
			renderer:  renderer(clickhousePlaceholders, clickhousePlaceholders.Execution),
			segments:  upsertBirds(typed(0, "UInt64"), typed(1, "String")),
			parts:     []string{"({p_id_", ":UInt64}, lower({p_name_", ":String}), {p_id_", ":UInt64} + 1, '$9 ?')"},
			slots:     []int{0, 1, 0},
			arguments: []int{0, 1},
			names:     []string{"p_id_", "p_name_"},
			rows:      "({p_id_0:UInt64}, lower({p_name_0:String}), {p_id_0:UInt64} + 1, '$9 ?'), ({p_id_1:UInt64}, lower({p_name_1:String}), {p_id_1:UInt64} + 1, '$9 ?')",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := batchQuery(sqlmodel.QueryCommandBatch, testCase.segments...)
			testCase.renderer.Query = query
			row, err := emitkit.RowTemplate(query, &testCase.renderer)
			require.NoError(t, err)

			require.Equal(t, testCase.parts, row.Parts)
			require.Equal(t, testCase.slots, row.Slots)
			require.Equal(t, testCase.arguments, row.Arguments)
			require.Equal(t, testCase.names, row.Names)
			require.Equal(t, testCase.renderer.Form.Kind, row.Kind)
			require.Equal(t, testCase.rows, writeRows(row, 2))

			rendered, err := emitkit.Render(query.Template, &testCase.renderer)
			require.NoError(t, err)
			if row.Kind != dialect.PlaceholderNamed {
				require.Equal(t, writeRows(row, 1), rendered.SQL[row.Values.Start:row.Values.End], "the first row is the row the constant holds")
			}
			require.Equal(t, "INSERT INTO birds (id, name, rank) VALUES ", rendered.SQL[:row.Values.Start])
			require.Equal(t, " ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name", rendered.SQL[row.Values.End:])
		})
	}
}

func TestRowTemplateRefusesARowItCannotRepeat(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		renderer emitkit.Renderer
		name     string
		want     string
		segments []sqlmodel.SQLSegment
	}{
		{
			name: "a parameter outside the row", renderer: renderer(postgresPlaceholders, postgresPlaceholders.Execution),
			segments: append(insertBirds(text("("), parameter(0), text(")")), text(" RETURNING "), parameter(1)),
			want:     "query InsertBirds: command: batch binds parameter name outside its VALUES row",
		},
		{
			name: "no VALUES row", renderer: renderer(postgresPlaceholders, postgresPlaceholders.Execution),
			segments: []sqlmodel.SQLSegment{text("UPDATE birds SET name = "), parameter(1)},
			want:     "query InsertBirds: command: batch repeats the statement's VALUES row",
		},
		{
			name: "a typed form without a type", renderer: renderer(clickhousePlaceholders, clickhousePlaceholders.Execution),
			segments: insertBirds(text("("), parameter(0), text(")")),
			want:     `parameter "id" is written in a typed form but its placeholder carries no type`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := batchQuery(sqlmodel.QueryCommandBatch, testCase.segments...)
			testCase.renderer.Query = query
			_, err := emitkit.RowTemplate(query, &testCase.renderer)
			require.ErrorContains(t, err, testCase.want)
		})
	}
}
