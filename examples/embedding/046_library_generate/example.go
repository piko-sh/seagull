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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

// run generates the store package from the project's migrations and queries, as a build
// tool that embeds Seagull does. It writes the package into directory or, when check is
// set, writes nothing and reports whether directory is out of date.
//
// Takes project (fs.FS) which holds db/migrations and db/queries.
// Takes directory (string) which is the package directory, such as internal/store.
// Takes check (bool) which compares the generated files with directory instead of writing
// them.
// Takes output (io.Writer) which receives the diagnostics and a summary.
//
// Returns bool which is true when check found directory out of date.
// Returns error when generation fails or a query has an error diagnostic.
func run(ctx context.Context, project fs.FS, directory string, check bool, output io.Writer) (bool, error) {
	sqlite, err := dialect_registry.New("sqlite", dialect_registry.Options{})
	if err != nil {
		return false, err
	}
	generator, err := seagull.New(sqlite, seagull.WithTypeOverrides(seagull.TypeOverride{
		SQLTypeName: "",
		Column:      "notes.settings",
		GoPackage:   "encoding/json",
		GoName:      "RawMessage",
	}))
	if err != nil {
		return false, fmt.Errorf("configuring the generator: %w", err)
	}

	result, err := generator.Generate(ctx, seagull.Input{
		Migrations:   project,
		Queries:      nil,
		Package:      "store",
		MigrationDir: "db/migrations",
		QueryDir:     "db/queries",
	})
	if err != nil {
		return false, err
	}
	for _, diagnostic := range result.Diagnostics {
		fmt.Fprintf(output, "%s:%d:%d: %s[%s]: %s\n", diagnostic.Filename, diagnostic.Line, diagnostic.Column,
			diagnostic.Severity, diagnostic.Code, diagnostic.Message)
	}
	if err := result.Err(); err != nil {
		return false, err
	}

	if check {
		stale, err := outOfDate(result, directory)
		if err != nil {
			return false, err
		}
		if len(stale) == 0 {
			fmt.Fprintln(output, "up to date")
			return false, nil
		}
		fmt.Fprintf(output, "out of date: %s\n", strings.Join(stale, ", "))
		return true, nil
	}

	if err := seagull.WriteFiles(directory, result); err != nil {
		return false, err
	}
	fmt.Fprintf(output, "wrote %d files for %d queries\n", len(result.Files), result.QueryCount)
	return false, nil
}

// outOfDate lists the files WriteFiles would change: a generated file that is missing or
// differs, and a file an earlier run generated that this run no longer does, such as the
// file of a deleted query, which WriteFiles would remove.
//
// Takes result (*seagull.Result) which is the generation to compare.
// Takes directory (string) which holds the package as it is now.
//
// Returns []string which holds the names of the files that would change, sorted.
// Returns error when the directory cannot be read.
func outOfDate(result *seagull.Result, directory string) ([]string, error) {
	var stale []string
	generated := make(map[string]bool, len(result.Files))
	for _, file := range result.Files {
		generated[file.Name] = true
		existing, err := os.ReadFile(filepath.Join(directory, file.Name))
		if err != nil || !bytes.Equal(existing, file.Content) {
			stale = append(stale, file.Name)
		}
	}

	entries, err := os.ReadDir(directory)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if generated[entry.Name()] || entry.IsDir() {
			continue
		}
		if firstLine(filepath.Join(directory, entry.Name())) == result.Header {
			stale = append(stale, entry.Name())
		}
	}
	slices.Sort(stale)
	return stale, nil
}

// firstLine reads the first line of a file, where a generated file carries its header.
//
// Takes path (string) which is the file.
//
// Returns string which is the first line, or empty when the file cannot be read.
func firstLine(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Scan()
	return scanner.Text()
}
