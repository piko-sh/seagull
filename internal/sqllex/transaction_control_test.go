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

package sqllex

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestControlsTransactionRecognisesTransactionStatementsOnly(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		text string
		want bool
	}{
		{text: "BEGIN;", want: true},
		{text: "begin transaction", want: true},
		{text: "BEGIN IMMEDIATE TRANSACTION;", want: true},
		{text: "BEGIN WORK", want: true},
		{text: "START TRANSACTION READ WRITE", want: true},
		{text: "COMMIT;", want: true},
		{text: "END;", want: true},
		{text: "END TRANSACTION", want: true},
		{text: "ROLLBACK TO SAVEPOINT before_change", want: true},
		{text: "SAVEPOINT before_change", want: true},
		{text: "RELEASE SAVEPOINT before_change", want: true},
		{text: "-- a note\nCOMMIT", want: true},
		{text: "BEGIN NOT ATOMIC SELECT 1; END", want: false},
		{text: "CREATE TABLE commit_log (id INT)", want: false},
		{text: "INSERT INTO t VALUES ('COMMIT')", want: false},
		{text: "START REPLICA", want: false},
		{text: "", want: false},
		{text: ";", want: false},
		{text: "; BEGIN", want: true},
		{text: `SAVEPOINT "before change"`, want: true},
		{text: `RELEASE "before change"`, want: true},
		{text: `BEGIN "x"`, want: false},
		{text: "START", want: false},
		{text: "'COMMIT'", want: false},
	} {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, ControlsTransaction(SignificantTokens(Rules{}, test.text)))
		})
	}
}
