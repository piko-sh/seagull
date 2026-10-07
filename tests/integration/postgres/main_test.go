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

package postgres_test

import (
	"flag"
	"testing"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

var updateGoldenFiles = flag.Bool("update", false, "Update golden files")

func caseSchemas() harness.Schemas {
	return harness.Schemas{Driver: "pgx", Connection: testConnectionString, Setup: nil, SearchPath: nil}
}

func TestPostgresCases(t *testing.T) {
	harness.Run(t, &harness.Suite{
		Dialect:  func() dialect.Dialect { return dialect_engine_postgres.New() },
		Prepare:  caseSchemas().Prepare,
		Emitters: map[string]seagull.Option{"pgx": seagull.WithEmitter(emit_driver_pgx.New())},
		Driver:   harness.Driver{Module: "github.com/jackc/pgx/v5", Import: "github.com/jackc/pgx/v5/stdlib", Name: "pgx"},
		Update:   *updateGoldenFiles,
	})
}
