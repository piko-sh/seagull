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

package harness

import (
	"sync"
	"testing"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// CheckFuzzTypeAliases compares native alias metadata across database configurations.
//
// Takes t (*testing.T) which reports native or inferred type disagreements.
// Takes suite (FuzzSuite) which supplies an isolated native connection.
func CheckFuzzTypeAliases(t *testing.T, suite FuzzSuite) {
	t.Helper()
	var metadata sync.Once
	for _, alias := range fuzzcase.TypeAliases(suite.Engine) {
		t.Run(alias.Name, func(t *testing.T) {
			runFuzzScenario(t, suite, OracleTypes, fuzzcase.AliasScenario(suite.Engine, alias), &metadata)
		})
	}
}
