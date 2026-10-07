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
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
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

// reportExpected is a runner that stands in for sgl vet and reports, as warnings,
// exactly the codes the variant's example.yaml expects.
func reportExpected(_ context.Context, directory string, _ []string) ([]byte, []byte, error) {
	variant, err := catalogue.LoadVariant(directory)
	if err != nil {
		return nil, nil, err
	}
	var output strings.Builder
	for _, code := range variant.ExpectedDiagnostics {
		_, _ = fmt.Fprintf(&output, "{\"severity\":\"warning\",\"code\":%q}\n", code)
	}
	return []byte(output.String()), nil, nil
}

func TestRunVetsEveryVariantWithTheRunner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		runner     catalogue.CommandRunner
		name       string
		wantStdout string
		wantStderr string
		arguments  []string
		wantStatus int
	}{
		{
			name:       "an sgl that reports what each variant expects",
			arguments:  []string{"-examples", examplesDirectory, "-sgl", "sgl"},
			runner:     reportExpected,
			wantStatus: 0,
			wantStdout: "vetted variants report their expected_diagnostics\n",
			wantStderr: "",
		},
		{
			name:      "an sgl that reports an unexpected error",
			arguments: []string{"-examples", examplesDirectory, "-sgl", "sgl", "-v"},
			runner: func(context.Context, string, []string) ([]byte, []byte, error) {
				return []byte(`{"severity":"error","code":"Q999"}` + "\n"), nil, exitedWith(1)
			},
			wantStatus: 1,
			wantStdout: "unexpected Q999",
			wantStderr: "",
		},
		{
			name:       "an examples directory without the variants vetcheck knows",
			arguments:  []string{"-examples", filepath.Join(examplesDirectory, "catalogue"), "-sgl", "sgl"},
			runner:     reportExpected,
			wantStatus: 1,
			wantStdout: "",
			wantStderr: "vetcheck: vetSpecials names",
		},
		{
			name:       "no sgl",
			arguments:  []string{"-examples", examplesDirectory},
			runner:     reportExpected,
			wantStatus: 2,
			wantStdout: "",
			wantStderr: "vetcheck: -sgl is required\n",
		},
		{
			name:       "an unknown flag",
			arguments:  []string{"-sgl", "sgl", "-verbose"},
			runner:     reportExpected,
			wantStatus: 2,
			wantStdout: "",
			wantStderr: "flag provided but not defined: -verbose",
		},
		{
			name:       "a request for help",
			arguments:  []string{"-h"},
			runner:     reportExpected,
			wantStatus: 0,
			wantStdout: "",
			wantStderr: "-sgl string",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

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
}

func TestRunStopsWhenTheContextIsDone(t *testing.T) {
	t.Parallel()

	stopped := errors.New("interrupted")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stopped)
	var stdout, stderr bytes.Buffer

	status := run(ctx, []string{"-examples", examplesDirectory, "-sgl", "sgl"}, &stdout, &stderr, reportExpected)

	require.Equal(t, 1, status)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "interrupted")
}

func TestReportPassesWhenOnlySkipsAndMatches(t *testing.T) {
	var out bytes.Buffer
	status := report(&out, []catalogue.VetResult{
		{Variant: "sqlite/014_runtime_builder", Expected: []string{"Q022"}, Reported: []string{"Q022"}},
		{Variant: "embedding/050_schema_from_live_database", Skipped: "needs a live database"},
	}, false)

	require.Equal(t, 0, status)
	require.Equal(t, "vetcheck: embedding/050_schema_from_live_database: skipped; needs a live database\n"+
		"vetcheck: all 1 vetted variants report their expected_diagnostics\n", out.String())
}

func TestReportListsEveryVariantThatDiffers(t *testing.T) {
	var out bytes.Buffer
	status := report(&out, []catalogue.VetResult{
		{Variant: "sqlite/001_first_query"},
		{Variant: "tooling/042_ci_pipeline", Expected: []string{"Q046"}, Missing: []string{"Q046"}},
		{Variant: "sqlite/002_commands", Failure: "sgl vet exited 2"},
	}, true)

	require.Equal(t, 1, status)
	require.Equal(t, "  sqlite/001_first_query: reports [] as expected\n"+
		"vetcheck: 2 of 3 vetted variants differ from their expected_diagnostics:\n"+
		"  tooling/042_ci_pipeline: missing Q046 (expected_diagnostics [Q046], sgl vet reported [])\n"+
		"  sqlite/002_commands: sgl vet exited 2\n", out.String())
}
