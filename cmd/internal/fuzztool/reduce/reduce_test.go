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

//go:build linux || darwin

package reduce

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

const plantedModule = "module planted\n\ngo 1.24\n"

const plantedTest = `package planted

import (
	"os"
	"strings"
	"testing"
)

func FuzzPlanted(f *testing.F) {
	f.Fuzz(func(t *testing.T, text string) {
		if index := strings.Index(text, "Z"); index >= 0 {
			t.Fatalf("found the planted byte at %d", index)
		}
	})
}

func FuzzUnstable(f *testing.F) {
	f.Fuzz(func(t *testing.T, text string) {
		if _, err := os.Stat("marker"); err == nil {
			return
		}
		if err := os.WriteFile("marker", nil, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Fatal("fails only the first time")
	})
}
`

func newPlantedModule(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	module := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(module, "go.mod"), []byte(plantedModule), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(module, "planted_test.go"), []byte(plantedTest), 0o600))
	return module
}

func TestRunReducesPlantedFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and replays a test binary")
	}
	t.Parallel()

	tests := []struct {
		name        string
		function    string
		text        string
		wantReduced string
		wantStopped string
		wantStatus  int
		attempts    int
	}{
		{
			name: "a planted failure reduces to its one byte", function: "FuzzPlanted", text: "aaaaaaaaaaaaZbbbbbbbbbbbb",
			attempts: 2000, wantReduced: "go test fuzz v1\nstring(\"Z\")\n", wantStopped: stoppedExhausted,
		},
		{
			name: "a spent budget keeps the best verified input", function: "FuzzPlanted", text: "aaaaaaaaaaaaZbbbbbbbbbbbb",
			attempts: 1, wantStopped: stoppedBudget,
		},
		{name: "a passing input is refused", function: "FuzzPlanted", text: "abc", attempts: 2000, wantStatus: StatusUsage},
		{name: "an intermittent failure is refused", function: "FuzzUnstable", text: "abc", attempts: 2000, wantStatus: StatusIntermittent},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			module := newPlantedModule(t)
			input := filepath.Join(module, "input")
			require.NoError(t, os.WriteFile(input, []byte("go test fuzz v1\nstring(\""+testCase.text+"\")\n"), 0o600))
			output := filepath.Join(module, "reduced")
			options := Options{
				Go: "go", Module: module, Package: ".", Function: testCase.function, Tags: "", Input: input, Output: output,
				Attempts: testCase.attempts, Seconds: 600, RunTimeout: 60, Confirmations: 3,
			}
			var stdout, stderr bytes.Buffer

			status := Run(context.Background(), options, nil, &stdout, &stderr)

			require.Equal(t, testCase.wantStatus, status, stderr.String())
			corpusDirectory := filepath.Join(module, "testdata", "fuzz", testCase.function)
			require.NoDirExists(t, corpusDirectory, "replayed inputs and the directories made for them are removed")
			if testCase.wantStatus != 0 {
				return
			}
			var result map[string]any
			content, err := os.ReadFile(filepath.Join(output, "summary.json"))
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(content, &result))
			require.JSONEq(t, string(content), stdout.String())
			require.Equal(t, testCase.wantStopped, result["stopped"])
			require.InDelta(t, 25, result["original_bytes"], 0)
			require.Equal(t, []any{"FuzzPlanted", "planted_test.go:12", "found the planted byte at <n>"}, result["identity"])
			if testCase.wantReduced != "" {
				reduced, err := os.ReadFile(filepath.Join(output, "reduced"))
				require.NoError(t, err)
				require.Equal(t, testCase.wantReduced, string(reduced))
				require.InDelta(t, 1, result["reduced_bytes"], 0)
			}
		})
	}
}

func TestRunRefusesUnusableRequests(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	unsupported := filepath.Join(directory, "unsupported")
	require.NoError(t, os.WriteFile(unsupported, []byte("go test fuzz v1\nint(1)\n"), 0o600))
	valid := filepath.Join(directory, "valid")
	require.NoError(t, os.WriteFile(valid, []byte("go test fuzz v1\nstring(\"a\")\n"), 0o600))
	existing := filepath.Join(directory, "existing")
	require.NoError(t, os.Mkdir(existing, 0o700))

	tests := []struct {
		name       string
		input      string
		output     string
		wantStatus int
	}{
		{name: "a missing input", input: filepath.Join(directory, "missing"), output: filepath.Join(directory, "a"), wantStatus: StatusUsage},
		{name: "an unsupported input", input: unsupported, output: filepath.Join(directory, "b"), wantStatus: StatusUsage},
		{name: "an existing output", input: valid, output: existing, wantStatus: StatusFailed},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			options := Options{
				Go: "go", Module: directory, Package: ".", Function: "FuzzThing", Tags: "", Input: testCase.input,
				Output: testCase.output, Attempts: 1, Seconds: 1, RunTimeout: 1, Confirmations: 1,
			}
			var stdout, stderr bytes.Buffer

			require.Equal(t, testCase.wantStatus, Run(context.Background(), options, nil, &stdout, &stderr))
			require.Empty(t, stdout.String())
		})
	}
}

func TestIdentifyNamesTheFirstFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		output     string
		want       Identity
		wantFailed bool
	}{
		{name: "a pass", output: "--- PASS: FuzzThing (0.00s)\nPASS\n"},
		{
			name: "a located failure", wantFailed: true,
			output: "=== RUN   FuzzThing/x\n    thing_test.go:42: value 0xc000123 after 15ms in /tmp/abc/def\n--- FAIL: FuzzThing (0.00s)\n",
			want:   Identity{Function: "FuzzThing", Location: "thing_test.go:42", Message: "value <address> after <duration> in <temp>"},
		},
		{
			name: "an empty message borrows the lines below", wantFailed: true,
			output: "    thing_test.go:7: \n        first\n        second 12\n--- FAIL: FuzzThing (0.00s)\n",
			want:   Identity{Function: "FuzzThing", Location: "thing_test.go:7", Message: "first second <n> --- FAIL: FuzzThing (<duration>)"},
		},
		{
			name: "a panic", wantFailed: true,
			output: "panic: runtime error: index out of range [3] with length 3\n",
			want:   Identity{Function: "FuzzThing", Location: "panic", Message: "runtime error: index out of range [<n>] with length <n>"},
		},
		{
			name: "a failure without a source line", wantFailed: true,
			output: "--- FAIL: FuzzThing (0.00s)\n",
			want:   Identity{Function: "FuzzThing", Location: "unlocated", Message: ""},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			identity, failed := identify("FuzzThing", testCase.output)
			require.Equal(t, testCase.wantFailed, failed)
			require.Equal(t, testCase.want, identity)
		})
	}
}

func TestSignalStatusFollowsTheShell(t *testing.T) {
	t.Parallel()

	require.Equal(t, 130, signalStatus(syscall.SIGINT))
	require.Equal(t, 143, signalStatus(syscall.SIGTERM))
}

func TestRunStopsAndCleansUpOnASignal(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and replays a test binary")
	}
	t.Parallel()

	module := newPlantedModule(t)
	input := filepath.Join(module, "input")
	require.NoError(t, os.WriteFile(input, []byte("go test fuzz v1\nstring(\"aZ\")\n"), 0o600))
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGTERM
	options := Options{
		Go: "go", Module: module, Package: ".", Function: "FuzzPlanted", Tags: "", Input: input,
		Output: filepath.Join(module, "reduced"), Attempts: 10, Seconds: 60, RunTimeout: 60, Confirmations: 1,
	}
	var stdout, stderr bytes.Buffer

	status := Run(context.Background(), options, signals, &stdout, &stderr)

	require.Equal(t, 143, status)
	require.NoDirExists(t, filepath.Join(module, "testdata"))
	require.NoFileExists(t, filepath.Join(module, "reduced", "summary.json"))
}
