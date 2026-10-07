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

package check

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
)

func TestReportIndexAnswersWhatWasReportedAlready(t *testing.T) {
	t.Parallel()

	reported := newReportIndex([]diag.SourceError{
		{Code: diag.CodeUnknownColumn, Message: `unknown column "nmae" in table "users"`},
		{Code: diag.CodeUnknownFunction, Message: `unknown function "lowr"`},
		{Code: diag.CodeEngineReading, Message: `a "quoted \"inner\" name" and an unmatched " quote`},
	})

	testCases := []struct {
		name    string
		code    string
		text    string
		quoted  bool
		message bool
	}{
		{name: "a quoted column", code: diag.CodeUnknownColumn, text: "nmae", quoted: true},
		{name: "a quoted table", code: diag.CodeUnknownColumn, text: "users", quoted: true},
		{name: "a name under another code", code: diag.CodeUnknownFunction, text: "nmae", quoted: false},
		{name: "a quoted function", code: diag.CodeUnknownFunction, text: "lowr", quoted: true},
		{name: "a name with escaped quotes", code: diag.CodeEngineReading, text: `quoted "inner" name`, quoted: true},
		{name: "an unquoted word", code: diag.CodeUnknownColumn, text: "column", quoted: false},
		{name: "a whole message", code: diag.CodeUnknownFunction, text: `unknown function "lowr"`, message: true},
		{name: "a message under another code", code: diag.CodeUnknownColumn, text: `unknown function "lowr"`, message: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.quoted, reported.quotes(testCase.code, testCase.text))
			require.Equal(t, testCase.message, reported.has(testCase.code, testCase.text))
		})
	}
}
