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
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/examples/catalogue"
)

// examplesDirectory is the examples directory, from this command's directory.
const examplesDirectory = "../../.."

// exitedWith is the error a stub runner returns for an sgl that exited with a status.
type exitedWith int

func (e exitedWith) Error() string { return "exit status " + strconv.Itoa(int(e)) }

func (e exitedWith) ExitCode() int { return int(e) }

// generateSame is a runner that stands in for an sgl whose generate --check finds every
// twin's code current.
func generateSame(context.Context, string, []string) ([]byte, []byte, error) {
	return nil, nil, nil
}

func TestRunChecksEveryEquivalentCellWithTheRunner(t *testing.T) {
	t.Parallel()

	var diffArguments []string
	tests := []struct {
		runner     catalogue.CommandRunner
		name       string
		wantStdout string
		wantStderr string
		arguments  []string
		wantStatus int
	}{
		{
			name:       "an sgl that finds every twin current",
			arguments:  []string{"-examples", examplesDirectory, "-sgl", "sgl"},
			runner:     generateSame,
			wantStatus: 0,
			wantStdout: "equivalent cells generate their twin's code\n",
			wantStderr: "",
		},
		{
			name:      "an sgl that finds every twin differs",
			arguments: []string{"-examples", examplesDirectory, "-sgl", "sgl", "-diff"},
			runner: func(_ context.Context, _ string, arguments []string) ([]byte, []byte, error) {
				diffArguments = arguments
				return []byte("-old\n+new\n"), nil, exitedWith(1)
			},
			wantStatus: 1,
			wantStdout: "      -old\n      +new\n",
			wantStderr: "",
		},
		{
			name:      "an sgl that cannot start",
			arguments: []string{"-examples", examplesDirectory, "-sgl", "sgl"},
			runner: func(context.Context, string, []string) ([]byte, []byte, error) {
				return nil, nil, errors.New("no such file")
			},
			wantStatus: 1,
			wantStdout: "",
			wantStderr: "running sgl: no such file",
		},
		{
			name:       "no sgl",
			arguments:  []string{"-examples", examplesDirectory},
			runner:     generateSame,
			wantStatus: 2,
			wantStdout: "",
			wantStderr: "twins: -sgl is required\n",
		},
		{
			name:       "an unknown flag",
			arguments:  []string{"-sgl", "sgl", "-v"},
			runner:     generateSame,
			wantStatus: 2,
			wantStdout: "",
			wantStderr: "flag provided but not defined: -v",
		},
		{
			name:       "a request for help",
			arguments:  []string{"-help"},
			runner:     generateSame,
			wantStatus: 0,
			wantStdout: "",
			wantStderr: "-diff",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := run(t.Context(), test.arguments, &stdout, &stderr, test.runner)

			require.Equal(t, test.wantStatus, status, "stdout: %s\nstderr: %s", stdout.String(), stderr.String())
			require.Contains(t, stdout.String(), test.wantStdout)
			require.Contains(t, stderr.String(), test.wantStderr)
			if test.wantStderr == "" {
				require.Empty(t, stderr.String())
			}
		})
	}
	require.Equal(t, []string{"sgl", "generate", "--check", "--diff"}, diffArguments)
}

func TestRunStopsWhenTheContextIsDone(t *testing.T) {
	t.Parallel()

	stopped := errors.New("interrupted")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stopped)
	var stdout, stderr bytes.Buffer

	status := run(ctx, []string{"-examples", examplesDirectory, "-sgl", "sgl"}, &stdout, &stderr, generateSame)

	require.Equal(t, 1, status)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "interrupted")
}

func TestReportListsEveryCellThatDiffers(t *testing.T) {
	t.Parallel()

	cell := catalogue.Equivalent{
		Concept:       &catalogue.Concept{Number: 2, Slug: "commands"},
		Column:        catalogue.Column{ID: "cockroachdb", Engine: "cockroachdb"},
		Twin:          catalogue.Column{ID: "postgres", Engine: "postgres"},
		TwinDirectory: "postgres/002_commands",
		Dialect:       "cockroachdb",
	}
	tests := []struct {
		name       string
		wantOutput string
		results    []catalogue.TwinResult
		wantStatus int
	}{
		{
			name:       "every cell holds",
			results:    []catalogue.TwinResult{{Cell: cell, Same: true, Output: ""}},
			wantStatus: 0,
			wantOutput: "twins: all 1 equivalent cells generate their twin's code\n",
		},
		{
			name: "one cell of two differs",
			results: []catalogue.TwinResult{
				{Cell: cell, Same: true, Output: ""},
				{Cell: cell, Same: false, Output: "internal/store/models.go is stale\n"},
			},
			wantStatus: 1,
			wantOutput: "twins: 1 of 2 equivalent cells generate code that differs from their twin's:\n" +
				"  2 commands (cockroachdb, as postgres/002_commands)\n" +
				"      internal/store/models.go is stale\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.Equal(t, test.wantStatus, report(&out, test.results))
			require.Equal(t, test.wantOutput, out.String())
		})
	}
}
