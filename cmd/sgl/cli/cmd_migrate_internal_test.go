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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/internal/output"
	"seagull.sh/seagull/sdk/migrate"
)

func TestCheckMigrateArgumentsParsesEachArgumentOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		subcommand string
		rest       []string
		want       migrateArguments
		problem    string
	}{
		{name: "up", subcommand: "up", want: migrateArguments{version: 0, steps: 1, hasVersion: false}},
		{name: "down one step by default", subcommand: "down", want: migrateArguments{version: 0, steps: 1, hasVersion: false}},
		{name: "down a number of steps", subcommand: "down", rest: []string{"3"}, want: migrateArguments{version: 0, steps: 3, hasVersion: false}},
		{name: "to a version", subcommand: "to", rest: []string{"20240101"}, want: migrateArguments{version: 20240101, steps: 1, hasVersion: true}},
		{name: "force a version", subcommand: "force", rest: []string{"7"}, want: migrateArguments{version: 7, steps: 1, hasVersion: true}},
		{name: "repair every migration", subcommand: "repair", want: migrateArguments{version: 0, steps: 1, hasVersion: false}},
		{name: "repair one migration", subcommand: "repair", rest: []string{"4"}, want: migrateArguments{version: 4, steps: 1, hasVersion: true}},
		{name: "down a step count that is not a number", subcommand: "down", rest: []string{"many"}, problem: `"many" is not a positive number of steps`},
		{name: "to a version that is not a number", subcommand: "to", rest: []string{"latest"}, problem: `version "latest" is not a number`},
		{name: "force without a version", subcommand: "force", problem: "migrate force needs one version"},
		{name: "up with an argument", subcommand: "up", rest: []string{"1"}, problem: "migrate up takes no arguments"},
		{name: "an unknown subcommand", subcommand: "sideways", problem: `unknown migrate subcommand "sideways"`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			arguments, err := checkMigrateArguments(testCase.subcommand, testCase.rest)

			if testCase.problem != "" {
				require.ErrorContains(t, err, testCase.problem)
				assert.Equal(t, exitUsage, exitCodeFor(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.want, arguments)
		})
	}
}

func TestNextVersionRefusesToOverflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   []string
		version int64
		width   int
		problem string
	}{
		{name: "an empty directory", version: 1, width: defaultVersionWidth},
		{name: "after the highest version", files: []string{"0001_a.up.sql", "0009_b.up.sql", "0003_c.up.sql"}, version: 10, width: 4},
		{name: "the largest version", files: []string{"9223372036854775807_last.up.sql"}, problem: "is the largest a version can be"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			for _, name := range testCase.files {
				require.NoError(t, os.WriteFile(filepath.Join(directory, name), nil, 0o600))
			}

			version, width, err := nextVersion(directory)

			if testCase.problem != "" {
				require.ErrorContains(t, err, testCase.problem)
				assert.Equal(t, exitUsage, exitCodeFor(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.version, version)
			assert.Equal(t, testCase.width, width)
		})
	}
}

func TestReportOutcomePrintsTheCountBeforeTheFailure(t *testing.T) {
	t.Parallel()

	interrupted := &migrate.MigrationExecutionError{Cause: context.Canceled, Name: "add_users", Version: 3, Direction: migrate.MigrationDirectionUp}
	tests := []struct {
		name       string
		count      int
		err        error
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "a run that finished", count: 2, err: nil, wantCode: exitOK, wantStdout: "2 applied\n"},
		{name: "a run interrupted after two migrations", count: 2, err: interrupted, wantCode: exitInterrupted, wantStdout: "2 applied\n",
			wantStderr: "sgl: interrupted: migration 3 (add_users): " + context.Canceled.Error() + "\n" +
				"hint: run `sgl migrate status`; if migration 3 is dirty, `sgl migrate repair 3` resolves it\n"},
		{name: "a run interrupted before any migration", count: 0, err: context.Canceled, wantCode: exitInterrupted, wantStderr: "sgl: interrupted\n"},
		{name: "a run that failed after one migration", count: 1, err: fmt.Errorf("migration 2 (b): %w", os.ErrPermission), wantCode: exitDatabase, wantStdout: "1 applied\n",
			wantStderr: "sgl: migration 2 (b): permission denied\n"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			inv := &invocation{streams: output.IO{Stdin: nil, Stdout: &stdout, Stderr: &stderr}, program: defaultProgram}

			code := inv.reportOutcome(testCase.count, outcomeApplied, testCase.err)

			assert.Equal(t, testCase.wantCode, code)
			assert.Equal(t, testCase.wantStdout, stdout.String())
			assert.Equal(t, testCase.wantStderr, stderr.String())
		})
	}
}
