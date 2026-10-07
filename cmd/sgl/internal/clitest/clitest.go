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

// Package clitest drives sgl commands in a temporary project for the CLI's tests.
package clitest

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/cmd/sgl/internal/drivers"
)

const (
	// directoryPermissions is the mode of directories a test project creates.
	directoryPermissions = 0o750

	// filePermissions is the mode of files a test project writes.
	filePermissions = 0o600
)

// Result is the outcome of one command.
type Result struct {
	// Stdout is what the command wrote to standard output.
	Stdout string

	// Stderr is what the command wrote to standard error.
	Stderr string

	// Code is the command's exit status.
	Code int
}

// Project is a temporary directory holding an sgl project.
type Project struct {
	// tb reports failures.
	tb testing.TB

	// Dir is the project's root directory.
	Dir string
}

// NewProject creates an empty project in a temporary directory that is removed when the
// test ends.
//
// Takes tb (testing.TB) which owns the directory.
//
// Returns *Project which is empty.
func NewProject(tb testing.TB) *Project {
	tb.Helper()
	return &Project{tb: tb, Dir: tb.TempDir()}
}

// Path returns the absolute path of a file in the project.
//
// Takes name (string) which is slash-separated and relative to the project.
//
// Returns string which is absolute.
func (p *Project) Path(name string) string {
	return filepath.Join(p.Dir, filepath.FromSlash(name))
}

// Write creates or replaces a file, creating its directories.
//
// Takes name (string) which is slash-separated and relative to the project.
// Takes content (string) which is written.
func (p *Project) Write(name, content string) {
	p.tb.Helper()
	path := p.Path(name)
	require.NoError(p.tb, os.MkdirAll(filepath.Dir(path), directoryPermissions), "creating the directory of %s", name)
	require.NoError(p.tb, os.WriteFile(path, []byte(content), filePermissions), "writing %s", name)
}

// Read returns a file's content, failing the test when it cannot be read.
//
// Takes name (string) which is slash-separated and relative to the project.
//
// Returns string which is the content.
func (p *Project) Read(name string) string {
	p.tb.Helper()
	content, err := os.ReadFile(p.Path(name))
	require.NoError(p.tb, err, "reading %s", name)
	return string(content)
}

// Exists reports whether a file or directory exists in the project.
//
// Takes name (string) which is slash-separated and relative to the project.
//
// Returns bool which is true when it exists.
func (p *Project) Exists(name string) bool {
	_, err := os.Stat(p.Path(name))
	return err == nil
}

// Run runs sgl in the project with captured streams, under a context that ends with the
// test.
//
// Takes arguments (...string) which are the command line after the program name.
//
// Returns Result which holds the output and exit status.
func (p *Project) Run(arguments ...string) Result {
	return p.RunWith(p.tb.Context(), nil, arguments...)
}

// RunWith runs sgl in the project with a context and extra options, as the stock binary
// does: the stock drivers are registered first. Standard input is empty, so no command
// reads the terminal the tests run in.
//
// Takes ctx (context.Context) which the command runs under.
// Takes options ([]cli.Option) which are passed after the stock drivers, streams and
// directory, so they can replace any of them.
// Takes arguments (...string) which are the command line after the program name.
//
// Returns Result which holds the output and exit status.
func (p *Project) RunWith(ctx context.Context, options []cli.Option, arguments ...string) Result {
	var stdout, stderr bytes.Buffer
	all := append(drivers.Options(), cli.WithIO(strings.NewReader(""), &stdout, &stderr), cli.WithDirectory(p.Dir))
	all = append(all, options...)
	code := cli.MainContext(ctx, arguments, all...)
	return Result{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}
