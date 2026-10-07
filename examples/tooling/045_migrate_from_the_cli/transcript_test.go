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

// Package example replays transcript.txt, a terminal session that starts a project with
// sgl init, then applies, rolls back and inspects migrations and seeds.
//
// The transcript is a shell session: a "$ " line is a command, the lines after it are
// what it printed (stdout and stderr together), "# exit N" is its exit status, and other
// "#" lines narrate. sgl runs in-process in a temporary copy of this directory, so
// library.db and the new migration never touch the committed files. Applied times are
// recorded as YYYY-MM-DD hh:mm:ss. Run `go test . -update` to record it again.
package example

import (
	"testing"

	"seagull.sh/seagull/examples/tooling/internal/transcript"
)

func TestTranscript(t *testing.T) {
	transcript.Replay(t, nil)
}
