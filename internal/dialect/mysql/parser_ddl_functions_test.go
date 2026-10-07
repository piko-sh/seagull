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

package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestARoutineBodyIsKeptExactlyAsTheSourceWritesIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantBody     string
		wantLanguage string
	}{
		{
			name: "an IF block before a write",
			sql: "CREATE FUNCTION bump_if(p INT) RETURNS INT BEGIN IF p > 0 THEN SET p = 1; END IF; " +
				"UPDATE scores SET points = points + 1; RETURN p; END",
			wantBody:     "IF p > 0 THEN SET p = 1; END IF; UPDATE scores SET points = points + 1; RETURN p;",
			wantLanguage: "",
		},
		{
			name: "a CASE expression before a write",
			sql: "CREATE FUNCTION f(p INT) RETURNS INT BEGIN DECLARE v INT DEFAULT CASE WHEN p > 0 THEN 1 ELSE 0 END; " +
				"DELETE FROM t; RETURN v; END",
			wantBody:     "DECLARE v INT DEFAULT CASE WHEN p > 0 THEN 1 ELSE 0 END; DELETE FROM t; RETURN v;",
			wantLanguage: "",
		},
		{
			name: "loops and nested blocks",
			sql: "CREATE PROCEDURE p() BEGIN l1: LOOP BEGIN LEAVE l1; END; END LOOP l1; " +
				"WHILE 1 = 0 DO SELECT 1; END WHILE; REPEAT SELECT 2; UNTIL 1 END REPEAT; INSERT INTO t VALUES (1); END",
			wantBody: "l1: LOOP BEGIN LEAVE l1; END; END LOOP l1; WHILE 1 = 0 DO SELECT 1; END WHILE; " +
				"REPEAT SELECT 2; UNTIL 1 END REPEAT; INSERT INTO t VALUES (1);",
			wantLanguage: "",
		},
		{
			name:         "a RETURN expression keeps its quotes",
			sql:          "CREATE FUNCTION f(x TEXT) RETURNS TEXT DETERMINISTIC RETURN CONCAT('it''s ', `weird col`, x)",
			wantBody:     "CONCAT('it''s ', `weird col`, x)",
			wantLanguage: "sql",
		},
		{
			name:         "a procedure whose body is one statement",
			sql:          "CREATE PROCEDURE reset_scores() MODIFIES SQL DATA UPDATE scores SET points = 0",
			wantBody:     "UPDATE scores SET points = 0",
			wantLanguage: "",
		},
		{
			name:         "a body closed by a semicolon before its delimiter",
			sql:          "DELIMITER //\nCREATE PROCEDURE p() BEGIN\n  SELECT 'a;b';\nEND;//\nDELIMITER ;",
			wantBody:     "SELECT 'a;b';",
			wantLanguage: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.Equal(t, ast.MutationCreateFunction, mutation.Kind)
			require.NotNil(t, mutation.Function)
			assert.Equal(t, testCase.wantBody, mutation.Function.BodySQL)
			assert.Equal(t, testCase.wantLanguage, mutation.Function.Language)
		})
	}
}
