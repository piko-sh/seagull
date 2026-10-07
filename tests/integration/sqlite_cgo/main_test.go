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

package sqlite_cgo_test

import (
	"flag"
	"testing"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

var updateGoldenFiles = flag.Bool("update", false, "Update golden files")

func TestSQLiteCGOCases(t *testing.T) {
	harness.Run(t, &harness.Suite{
		Dialect:          func() dialect.Dialect { return dialect_engine_sqlite.New() },
		Testdata:         "../sqlite/testdata",
		Driver:           harness.Driver{Module: "github.com/mattn/go-sqlite3", Import: "github.com/mattn/go-sqlite3", Name: "sqlite3"},
		BuildEnvironment: []string{"CGO_ENABLED=1"},
		Update:           *updateGoldenFiles,
	})
}
