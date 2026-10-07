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

package dialect_engine_cockroachdb_test

import (
	"testing"

	"seagull.sh/seagull/internal/apitest"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_cockroachdb"
)

func TestDialectEngineCockroachdbFacadeAPI(t *testing.T) {
	t.Parallel()

	surface := apitest.Surface{
		"Option": dialect_engine_cockroachdb.Option(nil),

		"New":                       dialect_engine_cockroachdb.New,
		"WithName":                  dialect_engine_cockroachdb.WithName,
		"WithSearchPath":            dialect_engine_cockroachdb.WithSearchPath,
		"WithMaxParseDepth":         dialect_engine_cockroachdb.WithMaxParseDepth,
		"WithMaxTokensPerStatement": dialect_engine_cockroachdb.WithMaxTokensPerStatement,
	}

	apitest.Check(t, surface, "facade_test.golden.yaml")
}
