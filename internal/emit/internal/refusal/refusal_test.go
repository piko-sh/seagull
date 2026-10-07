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

package refusal_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/internal/refusal"
	"seagull.sh/seagull/internal/sqlmodel"
)

var errUnemittable = errors.New("the request cannot be emitted")

func TestErrorDescribesTheErrorProblems(t *testing.T) {
	t.Parallel()

	query := &sqlmodel.AnalysedQuery{Name: "CountRows"}
	testCases := []struct {
		name     string
		want     string
		problems []emit.Problem
	}{
		{name: "no problems", problems: nil, want: ""},
		{name: "a warning alone", problems: []emit.Problem{{Code: "Q059", Message: "only a warning", Severity: diag.SeverityWarning}}, want: ""},
		{
			name: "errors about a query and the request",
			problems: []emit.Problem{
				{Query: query, Code: "Q059", Message: "command: execrows is not reported", Severity: diag.SeverityError},
				{Code: "Q059", Message: "the cache cannot prepare", Severity: diag.SeverityError},
				{Code: "Q059", Message: "only a warning", Severity: diag.SeverityWarning},
			},
			want: "the emitter refuses the request: query CountRows: Q059 command: execrows is not reported; Q059 the cache cannot prepare",
		},
		{
			name: "errors with no code",
			problems: []emit.Problem{
				{Query: query, Code: "", Message: "the request names no driver", Severity: diag.SeverityError},
				{Code: "", Message: "the header is not a comment", Severity: diag.SeverityError},
			},
			want: "the emitter refuses the request: query CountRows: the request names no driver; the header is not a comment",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := refusal.Error(testCase.problems)
			if testCase.want == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, testCase.want)
			require.ErrorIs(t, err, emit.ErrRefused)
		})
	}
}

func TestInvalidRefusesARequestThatCannotBeEmitted(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		err  error
		name string
		want string
	}{
		{name: "no reason refuses nothing", err: nil, want: ""},
		{name: "a reason refuses the request", err: errUnemittable, want: "the emitter refuses the request: the request cannot be emitted"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := refusal.Invalid(testCase.err)
			if testCase.want == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, testCase.want)
			require.ErrorIs(t, err, emit.ErrRefused)
			require.ErrorIs(t, err, errUnemittable)
		})
	}
}
