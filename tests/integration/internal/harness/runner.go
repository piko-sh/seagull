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
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"

	"seagull.sh/seagull/internal/testkit"
	"seagull.sh/seagull/sdk/emit"
)

const (
	// RunnerModule is the module a case's runner is built in. A runner imports the generated
	// package as RunnerModule/db, and a type override names a package of its own as
	// RunnerModule/<directory>.
	RunnerModule = "case_runner"

	// runnerSource is the case file holding the runner's main package.
	runnerSource = "runner.go"

	// runnerMain is the name the runner's source is written under.
	runnerMain = "main.go"

	// driverSource is the file the harness writes beside the runner to link its driver.
	driverSource = "driver.go"

	// moduleFile is a Go module's manifest.
	moduleFile = "go.mod"

	// runnerGoVersion is the Go version a runner's module declares.
	runnerGoVersion = "1.27.0"

	// directoryMode is the mode of the directories the harness writes a runner into.
	directoryMode = 0o750

	// fileMode is the mode of the files the harness writes.
	fileMode = 0o600

	// commandWaitDelay is how long a command the harness starts has, once its context ends
	// and it is killed, before its output pipes are closed, so a process it started that
	// still holds them cannot keep the case waiting.
	commandWaitDelay = 10 * time.Second
)

// DriverSource returns the driver file of a runner: it imports the driver for its side
// effect and declares the name the driver registers as databaseDriver, the name the
// runner opens its database with.
//
// Takes driver (Driver) which names the driver.
//
// Returns string which is the file's source.
func DriverSource(driver Driver) string {
	return fmt.Sprintf("// Code written by the integration harness. DO NOT EDIT.\n\npackage main\n\n"+
		"import _ %q\n\n// databaseDriver is the name the runner opens its database with.\nconst databaseDriver = %q\n",
		driver.Import, driver.Name)
}

// RunnerModuleFile returns the go.mod of a module that requires each listed module at its
// version.
//
// Takes module (string) which is the module's path.
// Takes requirements (map[string]string) which maps each required module to its version.
//
// Returns string which is the go.mod's content.
func RunnerModuleFile(module string, requirements map[string]string) string {
	var content strings.Builder
	fmt.Fprintf(&content, "module %s\n\ngo %s\n", module, runnerGoVersion)
	for _, required := range slices.Sorted(maps.Keys(requirements)) {
		fmt.Fprintf(&content, "\nrequire %s %s\n", required, requirements[required])
	}
	return content.String()
}

// ModuleVersion returns the version of a module that the suite's go.mod, in the working
// directory, requires, so a runner builds against the version the suite pins.
//
// Takes modulePath (string) which is the required module.
//
// Returns string which is its version.
func ModuleVersion(t *testing.T, modulePath string) string {
	t.Helper()

	manifest, err := fs.ReadFile(os.DirFS("."), moduleFile)
	require.NoError(t, err, "reading the suite's %s", moduleFile)
	version, found, err := RequiredVersion(manifest, modulePath)
	require.NoError(t, err)
	require.True(t, found, "the suite's %s does not require %s", moduleFile, modulePath)
	return version
}

// Requirements returns the modules a go.mod requires, from its require directives only,
// written alone or in a block; replace and exclude directives are not requirements.
//
// Takes manifest ([]byte) which is the go.mod's content.
//
// Returns []*modfile.Require which holds the requirements in the order written.
// Returns error when the go.mod does not parse.
func Requirements(manifest []byte) ([]*modfile.Require, error) {
	file, err := modfile.Parse(moduleFile, manifest, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", moduleFile, err)
	}
	return file.Require, nil
}

// RequiredVersion returns the version a go.mod requires a module at.
//
// Takes manifest ([]byte) which is the go.mod's content.
// Takes modulePath (string) which is the required module.
//
// Returns string which is the version.
// Returns bool which is false when the go.mod does not require the module.
// Returns error when the go.mod does not parse.
func RequiredVersion(manifest []byte, modulePath string) (string, bool, error) {
	requirements, err := Requirements(manifest)
	if err != nil {
		return "", false, err
	}
	for _, requirement := range requirements {
		if requirement.Mod.Path == modulePath {
			return requirement.Mod.Version, true, nil
		}
	}
	return "", false, nil
}

// TidyModule runs go mod tidy in a module outside the workspace.
//
// Takes directory (string) which is the module's directory.
// Takes environment (...string) which holds variables to set besides the inherited ones.
func TidyModule(ctx context.Context, t *testing.T, directory string, environment ...string) {
	t.Helper()

	RunGo(ctx, t, directory, environment, "mod", "tidy")
}

// RunGo runs the go command in a directory, outside the workspace, with the toolchain
// capped and -buildvcs=false among its flags as every go command a test starts is, so a
// git operation running in the repository at the same time cannot fail a build, and fails
// the test with its output when it fails. The command is killed when ctx ends.
//
// Takes directory (string) which is the directory to run in.
// Takes environment ([]string) which holds variables to set besides the inherited ones.
// Takes arguments (...string) which are the go command's arguments.
func RunGo(ctx context.Context, t *testing.T, directory string, environment []string, arguments ...string) {
	t.Helper()

	command := testkit.GoCommandContext(ctx, directory, environment, arguments...)
	command.WaitDelay = commandWaitDelay
	output, err := command.CombinedOutput()
	require.NoError(t, err, "go %s failed:\n%s", strings.Join(arguments, " "), output)
}

// buildRunner writes a case's runner module, with the generated package, the runner's
// sources and the driver file, and builds it.
//
// Takes suite (*Suite) which names the driver and the build environment.
// Takes caseDirectory (string) which is the case's directory.
// Takes files ([]emit.GeneratedFile) which are the generated files.
//
// Returns string which is the path of the built runner.
func buildRunner(ctx context.Context, t *testing.T, suite *Suite, caseDirectory string, files []emit.GeneratedFile) string {
	t.Helper()

	directory := t.TempDir()
	for _, file := range files {
		writeFile(t, filepath.Join(directory, generatedPackage, filepath.FromSlash(file.Name)), file.Content)
	}
	copyRunnerSources(t, caseDirectory, directory)
	writeFile(t, filepath.Join(directory, driverSource), []byte(DriverSource(suite.Driver)))
	module := RunnerModuleFile(RunnerModule, map[string]string{suite.Driver.Module: ModuleVersion(t, suite.Driver.Module)})
	writeFile(t, filepath.Join(directory, moduleFile), []byte(module))

	TidyModule(ctx, t, directory, suite.BuildEnvironment...)
	binary := filepath.Join(directory, "runner")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	RunGo(ctx, t, directory, suite.BuildEnvironment, "build", "-o", binary, ".")
	return binary
}

// copyRunnerSources copies a case's runner.go as the runner's main.go, and the Go files
// of each directory beside it as a package of the runner's module, such as the types a
// type override names.
//
// Takes caseDirectory (string) which is the case's directory.
// Takes directory (string) which is the runner module's directory.
func copyRunnerSources(t *testing.T, caseDirectory, directory string) {
	t.Helper()

	source := os.DirFS(caseDirectory)
	content, err := fs.ReadFile(source, runnerSource)
	require.NoError(t, err, "reading %s", runnerSource)
	writeFile(t, filepath.Join(directory, runnerMain), content)

	packages, err := fs.Glob(source, "*/*.go")
	require.NoError(t, err)
	for _, name := range packages {
		content, err := fs.ReadFile(source, name)
		require.NoError(t, err, "reading %s", name)
		writeFile(t, filepath.Join(directory, filepath.FromSlash(name)), content)
	}
}

// executeRunner runs a built runner and returns what it printed to standard output. The
// runner is killed when ctx ends.
//
// Takes binary (string) which is the runner's path.
// Takes environment ([]string) which holds variables to set besides the inherited ones.
//
// Returns []byte which is the runner's standard output.
func executeRunner(ctx context.Context, t *testing.T, binary string, environment []string) []byte {
	t.Helper()

	command := exec.CommandContext(ctx, binary)
	command.Dir = filepath.Dir(binary)
	command.Env = append(os.Environ(), environment...)
	command.WaitDelay = commandWaitDelay
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	require.NoError(t, command.Run(), "the runner failed:\nstderr: %s\nstdout: %s", stderr.String(), stdout.String())
	return stdout.Bytes()
}

// writeFile writes a file, creating the directories it is in.
//
// Takes name (string) which is the file's path.
// Takes content ([]byte) which is written.
func writeFile(t *testing.T, name string, content []byte) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(name), directoryMode))
	require.NoError(t, os.WriteFile(name, content, fileMode))
}
