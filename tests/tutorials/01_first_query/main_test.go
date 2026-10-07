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
	"os"
	"path/filepath"
	"seagull.sh/seagull/tests/tutorials/internal/support"
	"testing"
)

func TestCheckpoint(t *testing.T) {
	db, err := support.Open(t.Context(), migrations, filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var output bytes.Buffer
	if err := run(t.Context(), db, &output); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("output.txt")
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != string(want) {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}
