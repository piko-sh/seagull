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

package dialectkit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

var (
	postgresLikeComments = dialectkit.CommentRules{NestedBlockComments: true}
	mysqlLikeComments    = dialectkit.CommentRules{
		DoubleDashRequiresWhitespace: true,
		HashLineComment:              true,
	}
	sqliteLikeComments = dialectkit.CommentRules{}
)

func TestSkipWhitespaceAndComments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		rules    dialectkit.CommentRules
		input    string
		position int
		want     int
	}{
		{name: "leading whitespace", rules: postgresLikeComments, input: "  \t\n x", want: 5},
		{name: "line comment to newline", rules: postgresLikeComments, input: "-- skip me\nx", want: 11},
		{name: "line comment to end", rules: postgresLikeComments, input: "-- skip me", want: 10},
		{name: "block comment", rules: postgresLikeComments, input: "/* hi */x", want: 8},
		{name: "nested block comment", rules: postgresLikeComments, input: "/* a /* b */ c */x", want: 17},
		{name: "non-nested stops at first close", rules: sqliteLikeComments, input: "/* a /* b */ c */x", want: 13},
		{name: "mysql hash comment", rules: mysqlLikeComments, input: "# skip\nx", want: 7},
		{name: "mysql double dash needs space", rules: mysqlLikeComments, input: "--x", want: 0},
		{name: "mysql double dash with space", rules: mysqlLikeComments, input: "-- x\ny", want: 5},
		{name: "mysql double dash before newline", rules: mysqlLikeComments, input: "--\nFROM", want: 3},
		{name: "mysql double dash before carriage return", rules: mysqlLikeComments, input: "--\rFROM", want: 3},
		{name: "mysql double dash at end of input", rules: mysqlLikeComments, input: "--", want: 2},
		{name: "mysql double dash before tab", rules: mysqlLikeComments, input: "--\tx", want: 4},
		{name: "no comment leaves position", rules: postgresLikeComments, input: "-x", want: 0},
		{name: "interleaved", rules: postgresLikeComments, input: "  -- a\n /* b */\tx", want: 16},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, err := testCase.rules.SkipWhitespaceAndComments(testCase.input, testCase.position)
			require.NoError(t, err)
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestSkipWhitespaceAndCommentsUnterminated(t *testing.T) {
	t.Parallel()

	for _, rules := range []dialectkit.CommentRules{postgresLikeComments, mysqlLikeComments, sqliteLikeComments} {
		_, err := rules.SkipWhitespaceAndComments("/* never closed", 0)
		require.Errorf(t, err, "expected unterminated block comment error for rules %+v", rules)
	}
}

func TestSkipToExecutableCommentStopsOnlyWhereABlockCommentWouldStart(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
		want  int
	}{
		{name: "stops in front of a MySQL executable comment", input: "  /*!50000 x */", want: 2},
		{name: "stops in front of a MariaDB executable comment", input: "\n/*M!100000 x */", want: 1},
		{name: "skips an ordinary block comment before one", input: "/* a */ /*! b */", want: 8},
		{name: "reads an opener inside a double dash comment as comment", input: "-- a /*! b\nx", want: 11},
		{name: "reads an opener inside a hash comment as comment", input: "# a /*M! b\nx", want: 11},
		{name: "reads an opener inside an ordinary block comment as comment", input: "/* a /*! b */x", want: 13},
		{name: "stops at a token that is no comment", input: " x /*! y */", want: 1},
		{name: "runs to the end of comment-only text", input: "-- only a comment", want: 17},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := mysqlLikeComments.SkipToExecutableComment(testCase.input, 0)

			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestSkipWhitespaceAndCommentsSkipsAnExecutableCommentLikeAnyBlockComment(t *testing.T) {
	t.Parallel()

	got, err := mysqlLikeComments.SkipWhitespaceAndComments("/*!50000 x */y", 0)

	require.NoError(t, err)
	assert.Equal(t, 13, got)
}

func TestSkipToExecutableCommentReportsAnUnterminatedBlockComment(t *testing.T) {
	t.Parallel()

	_, err := mysqlLikeComments.SkipToExecutableComment("/* a /*! never closed", 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unterminated block comment")
}

func TestExecutableCommentOpenerLengthMeasuresEachOpener(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		position int
		want     int
	}{
		{name: "a MySQL opener", input: "x /*!", position: 2, want: 3},
		{name: "a MariaDB opener", input: "/*M!1", position: 0, want: 4},
		{name: "an ordinary block comment", input: "/* x */", position: 0, want: 0},
		{name: "a position past the end", input: "/*!", position: 9, want: 0},
		{name: "a negative position", input: "/*!", position: -1, want: 0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, dialectkit.ExecutableCommentOpenerLength(testCase.input, testCase.position))
		})
	}
}
