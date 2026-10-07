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
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/wasm/internal/session"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestExplain(t *testing.T) {
	t.Parallel()

	explained, err := session.Explain("q1")
	require.NoError(t, err)
	require.Equal(t, "Q001", explained.Code)
	require.Equal(t, "Unknown column", explained.Title)
	require.Equal(t, "warning", explained.Severity)
	require.Contains(t, explained.Explanation, "```sql")

	encoded, err := json.Marshal(explained)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"severity":"warning"`)

	_, err = session.Explain("Q999")
	require.ErrorContains(t, err, `unknown diagnostic code "Q999"`)

	require.Len(t, session.Codes(), len(seagull.Codes()))
}

func TestFormat(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		request session.FormatRequest
		want    session.FormatResponse
	}{
		{
			name:    "alias rewritten to the canonical prefix",
			request: session.FormatRequest{Source: "--  seagull.query( GetUser ,one )\nSELECT 1;\n", Dialect: "", Canonical: "", Prefixes: nil},
			want:    session.FormatResponse{Source: "-- sgl.query(GetUser, one)\nSELECT 1;\n", Error: ""},
		},
		{
			name:    "canonical prefix chosen",
			request: session.FormatRequest{Source: "-- sgl.query(GetUser, one)\n", Dialect: "", Canonical: "seagull", Prefixes: nil},
			want:    session.FormatResponse{Source: "-- seagull.query(GetUser, one)\n", Error: ""},
		},
		{
			name:    "invalid prefix",
			request: session.FormatRequest{Source: "SELECT 1;\n", Dialect: "", Canonical: "Bad Prefix", Prefixes: nil},
			want:    session.FormatResponse{Source: "", Error: `invalid prefix: directive prefix "Bad Prefix" must match [a-z][a-z0-9_]{0,31}`},
		},
		{
			name:    "neutral rules read a comment without a space after the dashes",
			request: session.FormatRequest{Source: "--seagull.query(GetUser, one)\nSELECT 1;\n", Dialect: "", Canonical: "", Prefixes: nil},
			want:    session.FormatResponse{Source: "-- sgl.query(GetUser, one)\nSELECT 1;\n", Error: ""},
		},
		{
			name:    "mysql needs a space after the dashes, so the line is not a directive",
			request: session.FormatRequest{Source: "--seagull.query(GetUser, one)\nSELECT 1;\n", Dialect: "mysql", Canonical: "", Prefixes: nil},
			want:    session.FormatResponse{Source: "--seagull.query(GetUser, one)\nSELECT 1;\n", Error: ""},
		},
		{
			name:    "a named dialect formats its own comments",
			request: session.FormatRequest{Source: "--  seagull.query( GetUser ,one )\nSELECT 1;\n", Dialect: "mysql", Canonical: "", Prefixes: nil},
			want:    session.FormatResponse{Source: "-- sgl.query(GetUser, one)\nSELECT 1;\n", Error: ""},
		},
		{
			name:    "unknown dialect",
			request: session.FormatRequest{Source: "SELECT 1;\n", Dialect: "oracle", Canonical: "", Prefixes: nil},
			want: session.FormatResponse{Source: "", Error: `unknown dialect: invalid configuration "oracle": expected one of ` +
				"clickhouse, cockroachdb, duckdb, mariadb, mysql, postgres, sqlite, timescaledb"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, session.Format(testCase.request))
		})
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()

	version := session.Version()

	require.NotEmpty(t, version.Version)
	require.NotEmpty(t, version.GoVersion)
}

func TestFormatRefusesARequestLargerThanThePlaygroundTakes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		request session.FormatRequest
		message string
	}{
		{
			name:    "a source larger than a file may be",
			request: session.FormatRequest{Source: strings.Repeat("-", session.MaxFileBytes+1), Dialect: "", Canonical: "", Prefixes: nil},
			message: "the source is 262145 bytes, more than the playground takes: files of at most 262144 bytes",
		},
		{
			name:    "too many prefixes",
			request: session.FormatRequest{Source: "SELECT 1;\n", Dialect: "", Canonical: "", Prefixes: make([]string, 17)},
			message: "17 directive prefixes are more than the playground takes: at most 16",
		},
		{
			name:    "a prefix too long",
			request: session.FormatRequest{Source: "SELECT 1;\n", Dialect: "", Canonical: "", Prefixes: []string{strings.Repeat("p", 65)}},
			message: "a dialect or prefix of 65 characters is more than the playground takes: at most 64",
		},
		{
			name:    "a dialect name too long",
			request: session.FormatRequest{Source: "SELECT 1;\n", Dialect: strings.Repeat("d", 1000), Canonical: "", Prefixes: nil},
			message: "a dialect or prefix of 1000 characters is more than the playground takes: at most 64",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scaling.Within(t, 50*time.Millisecond, func(context.Context) {
				require.Equal(t, session.FormatResponse{Source: "", Error: testCase.message}, session.Format(testCase.request))
			})
		})
	}
}

func TestFormatTakesASourceAtTheLimit(t *testing.T) {
	t.Parallel()

	source := "-- seagull.query(GetUser, one)\n" + strings.Repeat("-", session.MaxFileBytes-len("-- seagull.query(GetUser, one)\n"))

	response := session.Format(session.FormatRequest{Source: source, Dialect: "", Canonical: "", Prefixes: nil})

	require.Empty(t, response.Error)
	require.True(t, strings.HasPrefix(response.Source, "-- sgl.query(GetUser, one)\n"))
}

func TestExplainQuotesOnlyTheStartOfALongCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		code    string
		message string
	}{
		{name: "a code at the limit", code: strings.Repeat("9", 32), message: `unknown diagnostic code "` + strings.Repeat("9", 32) + `"`},
		{name: "a code past the limit", code: strings.Repeat("9", 100_000), message: `unknown diagnostic code "` + strings.Repeat("9", 32) + `..."`},
		{name: "a code of many-byte characters", code: strings.Repeat("é", 40), message: `unknown diagnostic code "` + strings.Repeat("é", 32) + `..."`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := session.Explain(testCase.code)

			require.EqualError(t, err, testCase.message)
		})
	}
}
