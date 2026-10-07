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

package testkit

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const (
	// toolchainBuildProcs caps GOMAXPROCS for the Go toolchain subprocesses that test
	// harnesses spawn, so a full-suite run does not exhaust machine memory.
	toolchainBuildProcs = 2

	// goCommandName is the go command, found on PATH.
	goCommandName = "go"

	// workspaceSetting stops a go command reading the go.work file of the repository a test
	// runs in.
	workspaceSetting = "GOWORK=off"

	// goFlagsKey is the environment variable holding the flags every go command applies.
	goFlagsKey = "GOFLAGS"

	// defaultGoFlags are the flags every go command a test runs applies: no version control
	// stamp, so a git operation running at the same time cannot fail a build.
	defaultGoFlags = "-buildvcs=false"
)

// ToolchainEnvironment builds the environment for a Go toolchain subprocess spawned by a
// test.
//
// Takes extra (...string) which provides "KEY=VALUE" entries appended after the cap, so
// callers can add settings such as "GOWORK=off", or deliberately override the cap by
// setting GOMAXPROCS themselves, since a later entry wins over an earlier one.
//
// Returns []string which holds the current environment, the GOMAXPROCS cap, then extra.
func ToolchainEnvironment(extra ...string) []string {
	environment := os.Environ()
	environment = append(environment, "GOMAXPROCS="+strconv.Itoa(toolchainBuildProcs))
	return append(environment, extra...)
}

// GoCommand builds a go command a test runs in a directory, bound to the test's context.
//
// The command gets the environment ToolchainEnvironment builds, with the workspace off
// and -buildvcs=false among its GOFLAGS. The caller's settings follow these defaults, so
// a later setting wins; a GOFLAGS setting adds its flags after the inherited and default
// ones rather than replacing them.
//
// Takes t (testing.TB) whose context cancels the command when the test ends.
// Takes directory (string) which is where the command runs; empty runs it in the test's
// own directory.
// Takes environment ([]string) which holds "KEY=VALUE" settings beside the defaults.
// Takes arguments (...string) which are the go command's arguments, such as build.
//
// Returns *exec.Cmd which is ready to run.
func GoCommand(t testing.TB, directory string, environment []string, arguments ...string) *exec.Cmd {
	t.Helper()

	return GoCommandContext(t.Context(), directory, environment, arguments...)
}

// GoCommandContext builds a go command as GoCommand does, bound to a context rather than
// a test's, for a kit that runs the toolchain under a deadline of its own.
//
// Takes directory (string) which is where the command runs; empty runs it in the current
// directory.
// Takes environment ([]string) which holds "KEY=VALUE" settings beside the defaults.
// Takes arguments (...string) which are the go command's arguments, such as build.
//
// Returns *exec.Cmd which is ready to run.
func GoCommandContext(ctx context.Context, directory string, environment []string, arguments ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, goCommandName)
	command.Args = append(command.Args, arguments...)
	command.Dir = directory
	command.Env = ToolchainEnvironment(goEnvironment(os.Getenv(goFlagsKey), environment)...)
	return command
}

// goEnvironment puts the workspace default before a caller's settings and gathers every
// GOFLAGS setting into one, last, so the caller's flags add to the defaults.
//
// Takes inherited (string) which holds the GOFLAGS of the process environment.
// Takes environment ([]string) which holds the caller's settings.
//
// Returns []string which holds the settings to append to the process environment.
func goEnvironment(inherited string, environment []string) []string {
	flags := []string{inherited, defaultGoFlags}
	settings := make([]string, 0, len(environment)+2)
	settings = append(settings, workspaceSetting)
	for _, setting := range environment {
		if value, isFlags := strings.CutPrefix(setting, goFlagsKey+"="); isFlags {
			flags = append(flags, value)
			continue
		}
		settings = append(settings, setting)
	}
	return append(settings, goFlagsKey+"="+strings.Join(strings.Fields(strings.Join(flags, " ")), " "))
}
