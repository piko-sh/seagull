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

//go:build integration

package mariadb_test

import (
	"testing"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mariadb"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

func TestMariaDBInferredNumericTypesMatchTheServer(t *testing.T) {
	harness.CheckTypes(t, &harness.TypeSuite{
		Dialect:   func() dialect.Dialect { return dialect_engine_mariadb.New() },
		Database:  openMigrationDatabase(t),
		Probe:     harness.ColumnTypeProbe,
		Parse:     harness.MySQLTypeName,
		Allowlist: harness.TypeAllowlist{},
		Schema:    harness.MySQLNumericSchema(),
		Rows:      harness.MySQLNumericRows(),
		Queries:   harness.MySQLNumericProbes(),
		Width:     false,
	})
}
