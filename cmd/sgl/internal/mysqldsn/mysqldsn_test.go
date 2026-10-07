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

package mysqldsn_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/internal/mysqldsn"
)

func TestMultiStatements(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		dsn  string
		want bool
	}{
		{name: "no parameters", dsn: "user:pass@tcp(localhost:3306)/app", want: false},
		{name: "enabled", dsn: "user:pass@tcp(localhost:3306)/app?multiStatements=true", want: true},
		{name: "enabled with a number", dsn: "user:pass@tcp(localhost:3306)/app?parseTime=true&multiStatements=1", want: true},
		{name: "enabled in title case", dsn: "user:pass@tcp(localhost:3306)/app?multiStatements=True", want: true},
		{name: "disabled", dsn: "user:pass@tcp(localhost:3306)/app?multiStatements=false", want: false},
		{name: "another parameter only", dsn: "user:pass@tcp(localhost:3306)/app?parseTime=true", want: false},
		{name: "a password holding the parameter", dsn: "user:multiStatements=true@tcp(localhost:3306)/app", want: false},
		{name: "a password holding a question mark", dsn: "user:p?multiStatements=true@tcp(localhost:3306)/app", want: false},
		{name: "a database named like the parameter", dsn: "user:pass@tcp(localhost:3306)/multiStatements=true", want: false},
		{name: "an unparsable query", dsn: "user:pass@tcp(localhost:3306)/app?multiStatements=%zz", want: false},
		{name: "an unparsable value", dsn: "user:pass@tcp(localhost:3306)/app?multiStatements=yes", want: false},
		{name: "no slash", dsn: "multiStatements=true", want: false},
		{name: "empty", dsn: "", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, mysqldsn.MultiStatements(testCase.dsn))
		})
	}
}
