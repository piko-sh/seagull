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

package structtag_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/structtag"
)

var parseCases = []struct {
	want    map[string]string
	name    string
	text    string
	problem string
}{
	{name: "one tag", text: `validate:"required"`, want: map[string]string{"validate": "required"}},
	{name: "several tags separated by spaces", text: `validate:"required,email"  db:"email"`, want: map[string]string{"validate": "required,email", "db": "email"}},
	{name: "a quoted value keeps its escapes", text: `pattern:"a\"b\\c"`, want: map[string]string{"pattern": `a"b\c`}},
	{name: "an empty value", text: `json:""`, want: map[string]string{"json": ""}},
	{name: "the name placeholder is kept for expansion", text: `db:"{name}"`, want: map[string]string{"db": "{name}"}},
	{name: "a key without a quoted value", text: `validate:required`, problem: "key:\"value\""},
	{name: "a value without a key", text: `:"required"`, problem: "key:\"value\""},
	{name: "a key with nothing after its colon", text: `validate:`, problem: "key:\"value\""},
	{name: "a key without a colon", text: `validate`, problem: "key:\"value\""},
	{name: "a value holding a colon", text: `pattern:"a:b"`, want: map[string]string{"pattern": "a:b"}},
	{name: "a value left open", text: `validate:"required`, problem: "not closed"},
	{name: "pairs run together", text: `a:"1"b:"2"`, problem: "separate key:\"value\" pairs by spaces"},
	{name: "a repeated key", text: `db:"a" db:"b"`, problem: "more than once"},
	{name: "nothing but spaces", text: "   ", problem: "holds no tag"},
	{name: "a key holding a quote", text: `"a":"b"`, problem: "cannot hold"},
	{name: "a value holding a back quote", text: "a:\"`\"", problem: "back quote"},
}

var checkCases = []struct {
	name    string
	key     string
	value   string
	problem string
}{
	{name: "an ordinary tag", key: "validate", value: "required,email"},
	{name: "a value holding quotes and backslashes", key: "pattern", value: `"a\b"`},
	{name: "an empty key", key: "", value: "x", problem: "must not be empty"},
	{name: "a key holding a space", key: "my key", value: "x", problem: "cannot hold"},
	{name: "a key holding a colon", key: "a:b", value: "x", problem: "cannot hold"},
	{name: "a key holding a quote", key: `a"b`, value: "x", problem: "cannot hold"},
	{name: "a value holding a back quote", key: "a", value: "x`y", problem: "back quote"},
	{name: "a value holding a new line", key: "a", value: "x\ny", problem: "control character"},
}

func TestParseReadsATagAsGoWritesOne(t *testing.T) {
	t.Parallel()

	for _, testCase := range parseCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tags, err := structtag.Parse(testCase.text)
			if testCase.problem != "" {
				require.ErrorContains(t, err, testCase.problem)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.want, tags)
		})
	}
}

func TestCheckRefusesWhatAGeneratedTagCannotHold(t *testing.T) {
	t.Parallel()

	for _, testCase := range checkCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := structtag.Check(testCase.key, testCase.value)
			if testCase.problem != "" {
				require.ErrorContains(t, err, testCase.problem)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCheckAllReportsTheFirstProblemInKeyOrder(t *testing.T) {
	t.Parallel()

	require.NoError(t, structtag.CheckAll(nil))
	require.NoError(t, structtag.CheckAll(map[string]string{"db": "{name}", "validate": "required"}))
	err := structtag.CheckAll(map[string]string{"z key": "x", "a key": "y"})
	require.ErrorContains(t, err, `"a key"`)
}

func TestRenderWritesAKeyAndAQuotedValue(t *testing.T) {
	t.Parallel()

	require.Equal(t, `json:"id"`, structtag.Render("json", "id"))
	require.Equal(t, `pattern:"a\"b"`, structtag.Render("pattern", `a"b`))
	require.Equal(t, `title:"Prénom"`, structtag.Render("title", "Prénom"))
}
