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

package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDriverSourceLinksTheDriverAndNamesIt(t *testing.T) {
	t.Parallel()

	source := DriverSource(Driver{Module: "github.com/mattn/go-sqlite3", Import: "github.com/mattn/go-sqlite3", Name: "sqlite3"})

	require.Equal(t, "// Code written by the integration harness. DO NOT EDIT.\n\npackage main\n\n"+
		"import _ \"github.com/mattn/go-sqlite3\"\n\n// databaseDriver is the name the runner opens its database with.\n"+
		"const databaseDriver = \"sqlite3\"\n", source)
}

func TestRunnerModuleFileRequiresEachModuleInOrder(t *testing.T) {
	t.Parallel()

	module := RunnerModuleFile(RunnerModule, map[string]string{"modernc.org/sqlite": "v1.56.0", "github.com/jackc/pgx/v5": "v5.9.0"})

	require.Equal(t, "module case_runner\n\ngo 1.27.0\n\nrequire github.com/jackc/pgx/v5 v5.9.0\n\nrequire modernc.org/sqlite v1.56.0\n", module)
}

func TestRequiredVersionReadsOnlyRequireDirectives(t *testing.T) {
	t.Parallel()

	manifest := []byte("module example\n\ngo 1.27.0\n\nrequire modernc.org/sqlite v1.56.0\n\n" +
		"require (\n\tgithub.com/stretchr/testify v1.12.1\n\tgo.uber.org/goleak v1.3.0 // indirect\n)\n\n" +
		"replace (\n\tgithub.com/mattn/go-sqlite3 => ../go-sqlite3\n\tmodernc.org/sqlite v1.56.0 => ../sqlite\n)\n\n" +
		"exclude github.com/jackc/pgx/v5 v5.0.0\n")
	tests := []struct {
		name    string
		module  string
		version string
		found   bool
	}{
		{name: "a require on its own line", module: "modernc.org/sqlite", version: "v1.56.0", found: true},
		{name: "a require in a block", module: "github.com/stretchr/testify", version: "v1.12.1", found: true},
		{name: "an indirect requirement", module: "go.uber.org/goleak", version: "v1.3.0", found: true},
		{name: "a module only replaced is not required", module: "github.com/mattn/go-sqlite3", version: "", found: false},
		{name: "a module only excluded is not required", module: "github.com/jackc/pgx/v5", version: "", found: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			version, found, err := RequiredVersion(manifest, test.module)
			require.NoError(t, err)
			require.Equal(t, test.found, found)
			require.Equal(t, test.version, version)
		})
	}
}

func TestRequirementsReportsAManifestThatDoesNotParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		manifest  string
		wantError string
		want      []string
	}{
		{name: "requirements in the order written", manifest: "module example\n\nrequire (\n\tb.example/two v1.0.0\n\ta.example/one v0.1.0\n)\n",
			want: []string{"b.example/two v1.0.0", "a.example/one v0.1.0"}},
		{name: "a require with no version", manifest: "module example\n\nrequire a.example/one\n", wantError: "parsing go.mod"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requirements, err := Requirements([]byte(test.manifest))
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			written := make([]string, 0, len(requirements))
			for _, requirement := range requirements {
				written = append(written, requirement.Mod.Path+" "+requirement.Mod.Version)
			}
			require.Equal(t, test.want, written)
		})
	}
}

func TestCopyRunnerSourcesWritesTheRunnerAndItsPackages(t *testing.T) {
	t.Parallel()

	caseDirectory := t.TempDir()
	for name, content := range map[string]string{
		"runner.go":         "package main\n",
		"money/money.go":    "package money\n",
		"money/notes.txt":   "not Go",
		"testspec.json":     "{}",
		"queries/boats.sql": "SELECT 1;",
	} {
		writeFile(t, filepath.Join(caseDirectory, filepath.FromSlash(name)), []byte(content))
	}
	tests := []struct {
		name    string
		file    string
		want    string
		present bool
	}{
		{name: "the runner becomes main.go", file: runnerMain, want: "package main\n", present: true},
		{name: "a package beside the runner is copied", file: "money/money.go", want: "package money\n", present: true},
		{name: "a file that is not Go is left out", file: "money/notes.txt", present: false},
		{name: "the spec is left out", file: specFile, present: false},
	}
	directory := t.TempDir()
	copyRunnerSources(t, caseDirectory, directory)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(test.file)))
			if !test.present {
				require.ErrorIs(t, err, os.ErrNotExist)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, string(content))
		})
	}
}

func TestModuleVersionReadsTheSuitesManifest(t *testing.T) {
	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, moduleFile), []byte("module suite\n\ngo 1.27.0\n\nrequire (\n\tmodernc.org/sqlite v1.56.0\n)\n"))
	t.Chdir(directory)

	tests := []struct {
		name   string
		module string
		want   string
	}{
		{name: "the driver the suite pins", module: "modernc.org/sqlite", want: "v1.56.0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, ModuleVersion(t, test.module))
		})
	}
}
