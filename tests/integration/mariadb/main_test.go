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
	"context"
	"flag"
	"testing"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mariadb"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

var updateGoldenFiles = flag.Bool("update", false, "Update golden files")

func startServer(ctx context.Context) (func(context.Context) error, error) {
	container, connectionString, err := startMariaDBContainer(ctx)
	if err != nil {
		return nil, err
	}
	testConnectionString = connectionString
	return func(ctx context.Context) error { return container.Terminate(ctx) }, nil
}

func TestMariaDBCases(t *testing.T) {
	harness.Run(t, &harness.Suite{
		Dialect: func() dialect.Dialect { return dialect_engine_mariadb.New() },
		Prepare: harness.Databases{Driver: "mysql", Connection: testConnectionString}.Prepare,
		Driver:  harness.Driver{Module: "github.com/go-sql-driver/mysql", Import: "github.com/go-sql-driver/mysql", Name: "mysql"},
		Update:  *updateGoldenFiles,
	})
}
