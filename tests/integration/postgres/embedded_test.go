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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

var testConnectionString string

func startEmbeddedPostgres(ctx context.Context) (func(context.Context) error, error) {
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	database := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Username("sgl_test").
			Password("sgl_test").
			Database("sgl_test").
			Port(15432).
			RuntimePath(filepath.Join(os.TempDir(), "sgl-integration-pg-test")).
			StartTimeout(120 * time.Second),
	)
	if err := database.Start(); err != nil {
		return nil, fmt.Errorf("starting embedded postgres: %w", err)
	}

	testConnectionString = "postgres://sgl_test:sgl_test@localhost:15432/sgl_test?sslmode=disable"
	return func(context.Context) error { return database.Stop() }, nil
}
