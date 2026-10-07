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

package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestCheckpoint(t *testing.T) {
	dsn := os.Getenv("SGL_EXAMPLE_DSN")
	if dsn == "" {
		if os.Getenv("SGL_EXAMPLE_REQUIRE_DSN") == "1" {
			t.Fatal("SGL_EXAMPLE_DSN is required in the PostgreSQL integration lane")
		}
		t.Skip("set SGL_EXAMPLE_DSN to run PostgreSQL")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("tutorial test requires a postgres:// URL")
	}
	connection, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close(t.Context())
	schema := fmt.Sprintf("sgl_tutorial_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := connection.Exec(t.Context(), "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer connection.Exec(t.Context(), "DROP SCHEMA "+quoted+" CASCADE")
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	var output bytes.Buffer
	if err := run(t.Context(), parsed.String(), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Feed the seagulls\n" {
		t.Fatalf("output: %s", output.String())
	}
}
