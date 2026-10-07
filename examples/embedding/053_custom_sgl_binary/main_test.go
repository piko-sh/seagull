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
	"strings"
	"testing"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/examples/embedding/internal/exampledb"
)

func TestCustomSgl(t *testing.T) {
	var output bytes.Buffer
	for _, arguments := range [][]string{{"generate", "--check"}, {"vet", "--strict"}, {"dialects"}} {
		fmt.Fprintf(&output, "$ sgl %s\n", strings.Join(arguments, " "))
		configured := append(options(), cli.WithDirectory("."), cli.WithIO(nil, &output, &output))
		if code := cli.MainContext(t.Context(), arguments, configured...); code != 0 {
			t.Fatalf("sgl %s exited %d\n%s", strings.Join(arguments, " "), code, output.String())
		}
	}
	exampledb.MatchOutput(t, output.String())
}
