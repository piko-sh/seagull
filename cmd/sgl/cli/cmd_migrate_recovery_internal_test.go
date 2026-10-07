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

package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"

	"seagull.sh/seagull/cmd/sgl/internal/output"
)

func TestAskReadsOneBoundedLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		problem string
	}{
		{name: "an answer", input: "  yes \nignored\n", want: "yes"},
		{name: "an answer without a line ending", input: "3", want: "3"},
		{name: "an answer as long as allowed", input: strings.Repeat("7", maxAnswerBytes-1) + "\n", want: strings.Repeat("7", maxAnswerBytes-1)},
		{name: "an answer longer than allowed", input: strings.Repeat("y", maxAnswerBytes*4), problem: "the answer is longer than 1024 bytes"},
		{name: "a stream of zero bytes", input: strings.Repeat("\x00", 1<<16), problem: "the answer is longer than 1024 bytes"},
		{name: "no answer", input: "", problem: "no answer: standard input ended"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			inv := &invocation{streams: output.IO{Stdin: strings.NewReader(testCase.input), Stdout: io.Discard, Stderr: &bytes.Buffer{}}}

			answer, err := inv.ask("? ")

			if testCase.problem != "" {
				require.ErrorContains(t, err, testCase.problem)
				assert.Equal(t, exitUsage, exitCodeFor(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.want, answer)
		})
	}
}

func TestInteractiveRequiresATerminal(t *testing.T) {
	t.Parallel()

	devices, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, devices.Close()) })

	tests := []struct {
		name     string
		stdin    io.Reader
		terminal func(int) bool
		want     bool
	}{
		{name: "a reader with no file", stdin: strings.NewReader("y\n"), terminal: func(int) bool { return true }, want: false},
		{name: "a device that is not a terminal", stdin: devices, terminal: term.IsTerminal, want: false},
		{name: "a terminal", stdin: devices, terminal: func(int) bool { return true }, want: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			inv := &invocation{streams: output.IO{Stdin: testCase.stdin, Stdout: io.Discard, Stderr: io.Discard}, terminal: testCase.terminal}

			assert.Equal(t, testCase.want, inv.interactive())
		})
	}
}
