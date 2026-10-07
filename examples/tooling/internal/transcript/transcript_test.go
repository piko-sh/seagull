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

package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// recordingTest stands in for a test, recording the clean-ups and failures a helper
// registers instead of acting on them.
type recordingTest struct {
	testing.TB

	// cleanups holds the clean-ups registered, in order.
	cleanups []func()

	// failures holds each failure reported.
	failures []string
}

// Helper does nothing; the recording test reports no lines.
func (*recordingTest) Helper() {}

// Cleanup records a clean-up instead of registering it.
//
// Takes cleanup (func()) which is recorded.
func (recorder *recordingTest) Cleanup(cleanup func()) {
	recorder.cleanups = append(recorder.cleanups, cleanup)
}

// Errorf records a failure.
//
// Takes format (string) which is the message's format.
// Takes arguments (...any) which are its values.
func (recorder *recordingTest) Errorf(format string, arguments ...any) {
	recorder.failures = append(recorder.failures, fmt.Sprintf(format, arguments...))
}

func TestCheckLeaksFailsATestLeavingAGoroutineRunning(t *testing.T) {
	testCases := []struct {
		name         string
		leak         bool
		wantFailures int
	}{
		{name: "a test that leaves nothing running passes", leak: false, wantFailures: 0},
		{name: "a test that leaves a goroutine running fails", leak: true, wantFailures: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := &recordingTest{TB: t}
			checkLeaks(recorder)
			if len(recorder.cleanups) != 1 {
				t.Fatalf("checkLeaks registered %d clean-ups, want 1", len(recorder.cleanups))
			}

			release := make(chan struct{})
			finished := make(chan struct{})
			if testCase.leak {
				go func() {
					defer close(finished)
					<-release
				}()
			} else {
				close(finished)
			}
			recorder.cleanups[0]()
			close(release)
			<-finished

			if len(recorder.failures) != testCase.wantFailures {
				t.Fatalf("the check reported %q, want %d failures", recorder.failures, testCase.wantFailures)
			}
		})
	}
}

func TestParseReadsCommandsTheirOutputAndNarration(t *testing.T) {
	testCases := []struct {
		name      string
		text      string
		wantError string
		want      session
	}{
		{
			name: "commands with narration, output and a trailer",
			text: "# list it\n$ cat note.txt\nhello\n# exit 0\n\n$ sgl vet\n# exit 1\n# done\n",
			want: session{
				steps: []step{
					{command: "cat note.txt", narration: []string{"# list it"}, output: []string{"hello"}, exit: 0},
					{command: "sgl vet", narration: []string{""}, output: nil, exit: 1},
				},
				trailer: []string{"# done"},
			},
		},
		{name: "a line that is not a command or a comment", text: "hello\n", wantError: "line 1 is neither"},
		{name: "a command without an exit line", text: "$ cat note.txt\nhello\n", wantError: "has no # exit line"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			parsed, err := parse(testCase.text)
			if testCase.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
					t.Fatalf("parse = %v, want an error containing %q", err, testCase.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !sameSession(parsed, testCase.want) {
				t.Fatalf("parse = %+v, want %+v", parsed, testCase.want)
			}
			if rendered := parsed.render(); rendered != testCase.text {
				t.Fatalf("render = %q, want the transcript back, %q", rendered, testCase.text)
			}
		})
	}
}

func TestReplayRecordsATranscriptAndThenMatchesIt(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "note.txt"), []byte("hello\n"), filePermissions); err != nil {
		t.Fatal(err)
	}
	recording := "# copy a file and read it back\n$ mkdir out\n# exit 0\n$ cp note.txt out\n# exit 0\n$ cd out\n# exit 0\n" +
		"$ cat note.txt\n# exit 0\n$ sgl no-such-command\n# exit 0\n"
	if err := os.WriteFile(filepath.Join(source, File), []byte(recording), filePermissions); err != nil {
		t.Fatal(err)
	}
	t.Chdir(source)

	testCases := []struct {
		name   string
		update bool
	}{
		{name: "recorded again with -update", update: true},
		{name: "replayed against the new recording", update: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			previous := *update
			*update = testCase.update
			t.Cleanup(func() { *update = previous })

			prepared := false
			Replay(t, func(t *testing.T, directory string) {
				t.Helper()
				prepared = true
				if _, err := os.Stat(filepath.Join(directory, File)); err == nil {
					t.Fatal("the working copy holds the recording")
				}
			})
			if !prepared {
				t.Fatal("Replay did not prepare the working copy")
			}
		})
	}

	recorded, err := os.ReadFile(filepath.Join(source, File))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(recorded), "$ cat note.txt\nhello\n# exit 0\n") {
		t.Fatalf("the recording does not hold what cat printed:\n%s", recorded)
	}
	if strings.Contains(string(recorded), "$ sgl no-such-command\n# exit 0\n") {
		t.Fatalf("the recording kept the exit status sgl did not return:\n%s", recorded)
	}
}

// sameSession reports whether two parsed sessions hold the same steps and trailer.
//
// Takes left (session) which is one session.
// Takes right (session) which is the other.
//
// Returns bool which is true when they are equal.
func sameSession(left, right session) bool {
	return slices.EqualFunc(left.steps, right.steps, func(leftStep, rightStep step) bool {
		return leftStep.command == rightStep.command && leftStep.exit == rightStep.exit &&
			slices.Equal(leftStep.narration, rightStep.narration) && slices.Equal(leftStep.output, rightStep.output)
	}) && slices.Equal(left.trailer, right.trailer)
}
