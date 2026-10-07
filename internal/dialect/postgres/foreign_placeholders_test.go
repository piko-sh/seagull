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

func TestAPlaceholderInAnotherToolsSyntaxIsWarnedOf(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		warns []string
	}{
		{name: "an at sign before a name", sql: "SELECT id FROM users WHERE name = @name",
			warns: []string{"@name is no parameter here: @ before a name is the absolute value operator, so the statement reads " +
				"the column name or fails rather than taking a parameter; write $1 or :name"}},
		{name: "an at sign right after an equals sign", sql: "DELETE FROM users WHERE id=@id",
			warns: []string{"@id is no parameter here: @ before a name is the absolute value operator, so the statement reads " +
				"the column id or fails rather than taking a parameter; write $1 or :id"}},
		{name: "question marks where values stand", sql: "INSERT INTO users (id, name) VALUES (?, ?)",
			warns: []string{questionPlaceholderMessage, questionPlaceholderMessage}},
		{name: "a question mark after a comparison and a keyword", sql: "SELECT id FROM users ORDER BY id LIMIT ? OFFSET ?",
			warns: []string{questionPlaceholderMessage, questionPlaceholderMessage}},
		{name: "the jsonb key test", sql: "SELECT id FROM users WHERE name::jsonb ? 'key' AND name::jsonb ? $1"},
		{name: "the absolute value of a number", sql: "SELECT @ -5 AS magnitude, @ id AS spaced FROM users"},
		{name: "jsonb containment", sql: "SELECT id FROM users WHERE name::jsonb @> '{}'"},
		{name: "a PostgreSQL parameter", sql: "SELECT id FROM users WHERE name = :name AND id = $2"},
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

func TestASyntaxErrorAtAPlaceholderInAnotherToolsSyntaxSaysHowToWriteIt(t *testing.T) {
	t.Parallel()

	err := analyseQueryError(t, "SELECT id FROM users WHERE id = ? AND name = ?")

	require.EqualError(t, err, `syntax error near "?": `+questionPlaceholderMessage)
}
