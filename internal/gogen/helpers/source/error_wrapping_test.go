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

package source

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var errDeadline = errors.New("the deadline passed")

func TestWrapErrorLabelsAnErrorAndKeepsWhatItWraps(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		err   error
		name  string
		want  string
		finds error
	}{
		{name: "no error stays nil", err: nil, want: ""},
		{name: "a driver error is labelled", err: errDeadline, want: "sgl: GetBoat: the deadline passed", finds: errDeadline},
		{name: "no rows stays no rows", err: sql.ErrNoRows, want: "sgl: GetBoat: sql: no rows in result set", finds: sql.ErrNoRows},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wrapped := sglWrapError("sgl: GetBoat", testCase.err)
			if testCase.want == "" {
				require.NoError(t, wrapped)
				return
			}
			require.EqualError(t, wrapped, testCase.want)
			require.ErrorIs(t, wrapped, testCase.finds)
		})
	}
}

func TestWrapResultPassesTheValueThroughAndLabelsTheError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		err   error
		name  string
		want  string
		value int64
	}{
		{name: "a value with no error", value: 3, err: nil, want: ""},
		{name: "a value with an error", value: 0, err: errDeadline, want: "sgl: CountBoats: the deadline passed"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value, err := sglWrapResult[int64]("sgl: CountBoats")(testCase.value, testCase.err)

			require.Equal(t, testCase.value, value)
			if testCase.want == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, testCase.want)
			require.ErrorIs(t, err, testCase.err)
		})
	}
}
