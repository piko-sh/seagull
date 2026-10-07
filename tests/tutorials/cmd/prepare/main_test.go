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
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit"
)

func TestRunPreparesAWorkspaceWithACopiedGenerator(t *testing.T) {
	t.Parallel()

	checkout := testCheckout(t)
	generator := filepath.Join(t.TempDir(), "built-sgl")
	require.NoError(t, os.WriteFile(generator, []byte("#!/bin/sh\necho sgl\n"), 0o600))
	destination := filepath.Join(t.TempDir(), "practice")
	var stdout, stderr bytes.Buffer

	status := run(t.Context(), []string{"-sgl", generator, destination}, &stdout, &stderr)

	require.Equal(t, exitOK, status, stderr.String())
	resolved, err := resolvePath(destination)
	require.NoError(t, err)
	assert.Equal(t, "Practice workspace: "+resolved+"\nStart in "+filepath.Join(resolved, "01_first_query")+"\n", stdout.String())
	requireWorkspace(t, checkout, resolved)

	copied, err := os.ReadFile(filepath.Join(resolved, "sgl"))
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/sh\necho sgl\n", string(copied))
	info, err := os.Stat(filepath.Join(resolved, "sgl"))
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o755), info.Mode().Perm(), "the copied sgl is executable")
}

func TestRunBuildsTheGeneratorFromTheCheckout(t *testing.T) {
	if testing.Short() {
		t.Skip("builds sgl from the checkout")
	}
	t.Parallel()

	checkout := testCheckout(t)
	destination := filepath.Join(t.TempDir(), "practice")
	var stdout, stderr bytes.Buffer

	status := run(t.Context(), []string{destination}, &stdout, &stderr)

	require.Equal(t, exitOK, status, stderr.String())
	resolved, err := resolvePath(destination)
	require.NoError(t, err)
	requireWorkspace(t, checkout, resolved)
	version, err := testkit.GoCommand(t, resolved, nil, "version", "-m", filepath.Join(resolved, "sgl")).CombinedOutput()
	require.NoError(t, err, "the built sgl is a Go executable:\n%s", version)
	assert.Contains(t, string(version), "seagull.sh/seagull/cmd/sgl")
}

func TestRunRefusesAnExistingDestination(t *testing.T) {
	t.Parallel()

	destination := t.TempDir()
	var stdout, stderr bytes.Buffer

	status := run(t.Context(), []string{"-sgl", "unused", destination}, &stdout, &stderr)

	require.Equal(t, exitFailure, status)
	assert.Contains(t, stderr.String(), "choose a new practice directory")
	assert.Empty(t, stdout.String())
	entries, err := os.ReadDir(destination)
	require.NoError(t, err)
	assert.Empty(t, entries, "the existing directory is left alone")
}

func TestRunRefusesADestinationInsideTheCheckout(t *testing.T) {
	t.Parallel()

	checkout := testCheckout(t)
	destination := filepath.Join(checkout, "tests", "tutorials", "practice-inside-the-checkout")
	var stdout, stderr bytes.Buffer

	status := run(t.Context(), []string{"-sgl", "unused", destination}, &stdout, &stderr)

	require.Equal(t, exitFailure, status)
	assert.Contains(t, stderr.String(), "practice workspaces must be outside the Seagull checkout")
	assert.Empty(t, stdout.String())
	assert.NoDirExists(t, destination)
}

func TestRunNeedsExactlyOneDestination(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		arguments []string
		message   string
	}{
		{name: "no destination", arguments: nil, message: "usage: prepare"},
		{name: "two destinations", arguments: []string{"first", "second"}, message: "usage: prepare"},
		{name: "unknown flag", arguments: []string{"-nope", "first"}, message: "flag provided but not defined"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			status := run(t.Context(), testCase.arguments, &stdout, &stderr)

			require.Equal(t, exitUsage, status)
			assert.Contains(t, stderr.String(), testCase.message)
			assert.Empty(t, stdout.String())
		})
	}
}

func TestEmptyRunFunctionKeepsEverythingBeforeTheBody(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		text      string
		want      string
		wantError bool
	}{
		{
			name: "the body is replaced",
			text: "package main\n\nfunc main() {}\n\nfunc run(ctx context.Context) error {\n\tfmt.Println(1)\n\n\treturn nil\n}\n\nfunc helper() {}\n",
			want: "package main\n\nfunc main() {}\n\nfunc run(ctx context.Context) error {\n\treturn nil\n}\n",
		},
		{
			name: "a multi-line signature is kept",
			text: "package main\n\nfunc run(\n\tctx context.Context,\n) error {\n\treturn work(ctx)\n}\n",
			want: "package main\n\nfunc run(\n\tctx context.Context,\n) error {\n\treturn nil\n}\n",
		},
		{name: "no run function", text: "package main\n\nfunc main() {}\n", wantError: true},
		{name: "a run function without a body", text: "package main\n\nfunc run() error", wantError: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := emptyRunFunction(testCase.text)

			if testCase.wantError {
				require.ErrorIs(t, err, errNoRunFunction)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestFindCheckoutSearchesUpwards(t *testing.T) {
	t.Parallel()

	root, err := resolvePath(t.TempDir())
	require.NoError(t, err)
	checkout := filepath.Join(root, "seagull")
	nested := filepath.Join(checkout, "docs", "tutorials")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(checkout, "tests", "tutorials"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(checkout, "go.work"), []byte("go 1.27.0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(checkout, "tests", "tutorials", "go.mod"), []byte("module tutorials\n"), 0o600))

	found, err := findCheckout(nested)
	require.NoError(t, err)
	assert.Equal(t, checkout, found)

	found, err = findCheckout(checkout)
	require.NoError(t, err)
	assert.Equal(t, checkout, found)

	_, err = findCheckout(root)
	require.ErrorIs(t, err, errNoCheckout)
}

func TestResolvePathFollowsLinksInTheExistingPart(t *testing.T) {
	t.Parallel()

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	target := filepath.Join(root, "target")
	require.NoError(t, os.Mkdir(target, 0o755))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(target, link))

	resolved, err := resolvePath(filepath.Join(link, "missing", "practice"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(target, "missing", "practice"), resolved)

	resolved, err = resolvePath(filepath.Join(root, "missing", "..", "target"))
	require.NoError(t, err)
	assert.Equal(t, target, resolved)
}

func TestIsWithin(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		parent string
		path   string
		want   bool
	}{
		{name: "the directory itself", parent: "/work/seagull", path: "/work/seagull", want: true},
		{name: "a path beneath it", parent: "/work/seagull", path: "/work/seagull/tests/practice", want: true},
		{name: "a sibling sharing its prefix", parent: "/work/seagull", path: "/work/seagull-practice", want: false},
		{name: "its parent", parent: "/work/seagull", path: "/work", want: false},
		{name: "elsewhere", parent: "/work/seagull", path: "/tmp/practice", want: false},
		{name: "a name starting with dots", parent: "/work/seagull", path: "/work/seagull/..practice", want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, isWithin(testCase.parent, testCase.path))
		})
	}
}

func testCheckout(t *testing.T) string {
	t.Helper()

	working, err := os.Getwd()
	require.NoError(t, err)
	checkout, err := findCheckout(working)
	require.NoError(t, err)
	return checkout
}

func requireWorkspace(t *testing.T, checkout, destination string) {
	t.Helper()

	tutorials := filepath.Join(checkout, "tests", "tutorials")
	requireSameFile(t, filepath.Join(tutorials, "go.sum"), filepath.Join(destination, "go.sum"))
	requireSameTree(t, filepath.Join(tutorials, "internal"), filepath.Join(destination, "internal"))

	moduleFile, err := os.ReadFile(filepath.Join(destination, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, string(moduleFile), "module seagull.sh/seagull/tests/tutorials\n")
	assert.Contains(t, string(moduleFile), "seagull.sh/seagull => "+checkout+"\n")
	workspaceFile, err := os.ReadFile(filepath.Join(destination, "go.work"))
	require.NoError(t, err)
	assert.Contains(t, string(workspaceFile), "\t"+checkout+"\n")
	assert.Contains(t, string(workspaceFile), "\t"+destination+"\n")

	expected := map[string][]string{
		"01_first_query": {"db", "main.go"},
		"07_postgres":    {"db", "main.go", "main_test.go", "output.txt"},
		"08_embedding":   {"db", "main.go", "main_test.go"},
	}
	for name, entries := range expected {
		lessonDirectory := filepath.Join(destination, name)
		assert.Equal(t, entries, directoryNames(t, lessonDirectory), "the files supplied with %s", name)
		for _, entry := range entries {
			if entry != "db" && entry != "main.go" {
				requireSameFile(t, filepath.Join(tutorials, name, entry), filepath.Join(lessonDirectory, entry))
			}
		}
		original, err := os.ReadFile(filepath.Join(tutorials, name, "main.go"))
		require.NoError(t, err)
		want, err := emptyRunFunction(string(original))
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(lessonDirectory, "main.go"))
		require.NoError(t, err)
		assert.Equal(t, want, string(got), "%s/main.go has an empty run function", name)
		assert.Equal(t, []string{"migrations", "queries", "seeds"}, directoryNames(t, filepath.Join(lessonDirectory, "db")))
		for _, directory := range []string{"migrations", "queries", "seeds"} {
			assert.Empty(t, directoryNames(t, filepath.Join(lessonDirectory, "db", directory)))
		}
	}
}

func requireSameFile(t *testing.T, original, copied string) {
	t.Helper()

	want, err := os.ReadFile(original)
	require.NoError(t, err)
	got, err := os.ReadFile(copied)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got), "%s is copied unchanged", copied)
}

func requireSameTree(t *testing.T, original, copied string) {
	t.Helper()

	var originals []string
	require.NoError(t, filepath.WalkDir(original, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(original, path)
		if err != nil {
			return err
		}
		originals = append(originals, relative)
		requireSameFile(t, path, filepath.Join(copied, relative))
		return nil
	}))
	var copies []string
	require.NoError(t, filepath.WalkDir(copied, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(copied, path)
		if err != nil {
			return err
		}
		copies = append(copies, relative)
		return nil
	}))
	require.NotEmpty(t, originals)
	assert.Equal(t, originals, copies, "%s holds exactly the original files", copied)
}

func directoryNames(t *testing.T, directory string) []string {
	t.Helper()

	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
