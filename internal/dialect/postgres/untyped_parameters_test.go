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

package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAParameterPostgreSQLMeetsFirstWhereNothingTypesItIsWarnedOf(t *testing.T) {
	t.Parallel()

	message := func(placeholder, reason string) string {
		return "the server cannot determine the type of " + placeholder + ", which it meets first " + reason + ", so every " +
			"call fails; cast it where it first appears to the type it holds, such as " + placeholder + "::text or " +
			placeholder + "::bigint"
	}
	testCases := []struct {
		name  string
		sql   string
		warns []string
	}{
		{name: "an optional filter tested before it is compared", sql: "SELECT id FROM users WHERE (:name IS NULL OR name = :name)",
			warns: []string{message(":name", "as the operand of IS NULL")}},
		{name: "a pattern built by concat", sql: "SELECT id FROM users WHERE name LIKE concat('%', $1, '%')",
			warns: []string{message("$1", "as an argument of concat, which takes any type")}},
		{name: "a sum of two parameters", sql: "SELECT id FROM users WHERE id = $1 + $2",
			warns: []string{message("$1", "as an operand of + between two parameters"), message("$2", "as an operand of + between two parameters")}},
		{name: "a value tested in its own row", sql: "INSERT INTO posts (id, user_id, title) VALUES ($1, $2, $3), ($4, $5, $3 IS NULL)",
			warns: []string{message("$3", "as the operand of IS NULL")}},
		{name: "a filter tested before the value it assigns", sql: "UPDATE users SET name = $1 WHERE $1 IS NULL",
			warns: []string{message("$1", "as the operand of IS NULL")}},
		{name: "a negated parameter", sql: "SELECT -$1 AS negated",
			warns: []string{message("$1", "as the operand of a sign")}},
		{name: "a filter compared before it is tested", sql: "SELECT id FROM users WHERE name = :name OR :name IS NULL"},
		{name: "a cast filter", sql: "SELECT id FROM users WHERE :name::text IS NULL OR name = :name"},
		{name: "a join condition types the select list's test", sql: "SELECT $1 IS NULL AS unnamed FROM users u JOIN posts p ON p.title = $1"},
		{name: "a returned test of an inserted value", sql: "INSERT INTO users (id, name) VALUES ($1, $2) RETURNING $2 IS NULL AS unnamed"},
		{name: "a returned comparison types an assigned argument", sql: "UPDATE users SET name = concat(name, $1) WHERE id = 1 RETURNING name = $1 AS renamed"},
		{name: "a returned test of an updated value", sql: "UPDATE users SET name = $1 WHERE id = 1 RETURNING $1 IS NULL AS unnamed",
			warns: []string{message("$1", "as the operand of IS NULL")}},
		{name: "a separator of concat_ws", sql: "SELECT concat_ws($1, name, 'x') AS joined FROM users"},
		{name: "a test before a subquery holding it", sql: "SELECT id FROM users WHERE $1 IS NULL OR id IN (SELECT user_id FROM posts WHERE title = $1)"},
		{name: "a concatenation of two parameters", sql: "SELECT $1 || $2 AS joined"},
		{name: "an offset multiplying the page size by the page", sql: "SELECT id FROM users ORDER BY id LIMIT :size OFFSET :size * :page",
			warns: []string{message(":size", "as an operand of * between two parameters"), message(":page", "as an operand of * between two parameters")}},
		{name: "an offset from a cast page", sql: "SELECT id FROM users ORDER BY id LIMIT :size OFFSET (:page::int - 1) * :size"},
		{name: "a bare limit and offset", sql: "SELECT id FROM users ORDER BY id LIMIT $1 OFFSET $2"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			var messages []string
			for _, warning := range analysis.Warnings {
				require.True(t, warning.Span.Known())
				messages = append(messages, warning.Message)
			}
			require.Equal(t, testCase.warns, messages)
		})
	}
}
