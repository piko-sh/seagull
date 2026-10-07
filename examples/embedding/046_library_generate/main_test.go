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
	"io"
	"os"
	"testing"

	"seagull.sh/seagull/examples/embedding/internal/exampledb"
)

func TestExample(t *testing.T) {
	directory := t.TempDir()
	var output bytes.Buffer
	for _, check := range []bool{true, false, true} {
		if _, err := run(t.Context(), os.DirFS("."), directory, check, &output); err != nil {
			t.Fatal(err)
		}
	}
	exampledb.MatchOutput(t, output.String())
}

func TestCommittedStoreIsCurrent(t *testing.T) {
	stale, err := run(t.Context(), os.DirFS("."), outputDirectory, true, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatalf("%s is out of date; run go run .", outputDirectory)
	}
}
