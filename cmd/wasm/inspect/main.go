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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"piko.sh/safedisk"

	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/cmd/wasm/internal/session"
)

const (
	// exitOK is the status of a run that did everything asked.
	exitOK = 0

	// exitFailure is the status of a run that failed or found a project it could not run.
	exitFailure = 1

	// exitUsage is the status of a run with unusable flags.
	exitUsage = 2

	// inspectFile is the name of the file -batch writes for each project.
	inspectFile = "inspect.json"

	// projectSQLDirectory is the directory that, with a configuration file, marks a project
	// for -batch.
	projectSQLDirectory = "db"

	// directoryPermissions is the mode of the directories inspect creates.
	directoryPermissions = 0o755

	// filePermissions is the mode of the files inspect writes.
	filePermissions = 0o644
)

// errUsage reports flags that do not select exactly one mode.
var errUsage = errors.New("choose exactly one of -batch, -starters, -codes and -dialects")

// options holds the parsed flags.
type options struct {
	// batch is the directory -batch walks, or empty.
	batch string

	// starters is the directory -starters writes to, or empty.
	starters string

	// out is the mirror tree root for -batch, or the output file for -codes and -dialects.
	out string

	// codes selects -codes.
	codes bool

	// dialects selects -dialects.
	dialects bool

	// timings keeps the run timings in the output.
	timings bool
}

// starterView is one dialect's starter as -starters writes it.
type starterView struct {
	// Files maps each starter file's path to its content.
	Files map[string]string `json:"files"`

	// Dialect names the dialect.
	Dialect string `json:"dialect"`

	// Response is the playground's response for the starter files.
	Response session.Response `json:"response"`
}

// main runs inspect and exits with its status.
func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run parses the flags and runs the chosen mode.
//
// Takes arguments ([]string) which are the command-line arguments.
// Takes stdout (io.Writer) which receives JSON for -codes and -dialects without -out.
// Takes stderr (io.Writer) which receives progress and errors.
//
// Returns int which is the exit status.
func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	chosen, err := parseOptions(arguments, stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stderr, "inspect:", err)
		}
		return exitUsage
	}
	switch {
	case chosen.batch != "":
		return report(stderr, batch(ctx, chosen, stderr))
	case chosen.starters != "":
		return report(stderr, writeStarters(ctx, chosen))
	case chosen.codes:
		return report(stderr, emit(chosen.out, stdout, codeViews(ctx)))
	default:
		listed, err := session.Dialects()
		if err != nil {
			return report(stderr, err)
		}
		return report(stderr, emit(chosen.out, stdout, listed))
	}
}

// parseOptions parses the flags and checks that they select exactly one mode.
//
// Takes arguments ([]string) which are the command-line arguments.
// Takes stderr (io.Writer) which receives the usage text.
//
// Returns options which holds the parsed flags.
// Returns error when the flags cannot be parsed or select no mode or several.
func parseOptions(arguments []string, stderr io.Writer) (options, error) {
	chosen := options{batch: "", starters: "", out: "", codes: false, dialects: false, timings: false}
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&chosen.batch, "batch", "", "walk `DIR` and write inspect.json for every project in it")
	flags.StringVar(&chosen.starters, "starters", "", "write one starter JSON per dialect into `DIR`")
	flags.BoolVar(&chosen.codes, "codes", false, "print every diagnostic code as JSON")
	flags.BoolVar(&chosen.dialects, "dialects", false, "print the dialects as JSON")
	flags.StringVar(&chosen.out, "out", "", "mirror tree `PATH` for -batch, or output file for -codes and -dialects")
	flags.BoolVar(&chosen.timings, "timings", false, "keep run timings in -batch and -starters output")
	if err := flags.Parse(arguments); err != nil {
		return chosen, err
	}
	if flags.NArg() > 0 {
		return chosen, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	modes := 0
	for _, selected := range []bool{chosen.batch != "", chosen.starters != "", chosen.codes, chosen.dialects} {
		if selected {
			modes++
		}
	}
	if modes != 1 {
		return chosen, errUsage
	}
	if chosen.starters != "" && chosen.out != "" {
		return chosen, errors.New("-out does not apply to -starters, which writes into its own directory")
	}
	return chosen, nil
}

// batch walks a directory and writes inspect.json for every project in it.
//
// Takes chosen (options) which holds the root, the mirror tree and the timings flag.
// Takes stderr (io.Writer) which receives one line per project.
//
// Returns error when the walk fails, a file cannot be written, or a project's run did not
// complete.
func batch(ctx context.Context, chosen options, stderr io.Writer) error {
	var failed []string
	err := filepath.WalkDir(chosen.batch, func(directory string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.IsDir() {
			return walkErr
		}
		if directory != chosen.batch && strings.HasPrefix(entry.Name(), ".") {
			return filepath.SkipDir
		}
		if !isProject(directory) {
			return nil
		}
		response, err := inspectProject(ctx, chosen, directory)
		if err != nil {
			return err
		}
		fmt.Fprintf(stderr, "%s: %s\n", directory, summary(response))
		if !response.OK {
			failed = append(failed, directory)
		}
		return filepath.SkipDir
	})
	if err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("the playground could not run %s", strings.Join(failed, ", "))
	}
	return nil
}

// isProject reports whether a directory holds a configuration file and a db directory.
//
// Takes directory (string) which is checked.
//
// Returns bool which is true for a project directory.
func isProject(directory string) bool {
	if info, err := os.Stat(filepath.Join(directory, projectSQLDirectory)); err != nil || !info.IsDir() {
		return false
	}
	for _, name := range config.FileNames() {
		if info, err := os.Stat(filepath.Join(directory, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// inspectProject runs the playground over one project directory and writes the response
// as its inspect.json: beside the project, or under the mirror tree.
//
// Takes chosen (options) which holds the root, the mirror tree and the timings flag.
// Takes directory (string) which is the project.
//
// Returns session.Response which is the playground's response.
// Returns error when the project's files cannot be read or the response written.
func inspectProject(ctx context.Context, chosen options, directory string) (session.Response, error) {
	request, err := session.LoadRequest(os.DirFS(directory))
	if err != nil {
		return session.Response{}, fmt.Errorf("%s: %w", directory, err)
	}
	response := withTimings(session.Run(ctx, request), chosen.timings)
	if chosen.out == "" {
		return response, writeJSON(directory, inspectFile, response)
	}
	relative, err := filepath.Rel(chosen.batch, directory)
	if err != nil {
		return response, err
	}
	return response, writeJSON(chosen.out, filepath.Join(relative, inspectFile), response)
}

// writeStarters writes one starter JSON per dialect.
//
// Takes chosen (options) which holds the directory and the timings flag.
//
// Returns error when a starter cannot be built or written.
func writeStarters(ctx context.Context, chosen options) error {
	listed, err := session.Dialects()
	if err != nil {
		return err
	}
	for index := range listed {
		name := listed[index].Name
		files, err := session.Starter(name)
		if err != nil {
			return err
		}
		response := session.Run(ctx, session.Request{Files: files, Override: session.Override{}, V: session.ProtocolVersion})
		view := starterView{Files: files, Dialect: name, Response: withTimings(response, chosen.timings)}
		if err := writeJSON(chosen.starters, name+".json", view); err != nil {
			return err
		}
	}
	return nil
}

// withTimings clears a response's timings unless they are wanted.
//
// Takes response (session.Response) which is cleared.
// Takes keep (bool) which keeps the timings.
//
// Returns session.Response which has no timings unless keep is set.
func withTimings(response session.Response, keep bool) session.Response {
	if keep {
		return response
	}
	response.DurationMS = 0
	response.Stages = []session.StageView{}
	return response
}

// summary describes a response in one line.
//
// Takes response (session.Response) which is described.
//
// Returns string which names the files and diagnostics, or the error.
func summary(response session.Response) string {
	if !response.OK {
		return "failed: " + response.Error
	}
	return fmt.Sprintf("%d files, %d queries, %d errors, %d warnings, %d hints", len(response.Files), len(response.Queries),
		response.Counts.Errors, response.Counts.Warnings, response.Counts.Hints)
}

// emit writes a value as JSON to a file, or to stdout when no file is named.
//
// Takes target (string) which is the file, or empty.
// Takes stdout (io.Writer) which receives the JSON when target is empty.
// Takes value (any) which is encoded.
//
// Returns error when the value cannot be encoded or written.
func emit(target string, stdout io.Writer, value any) error {
	if target != "" {
		return writeJSON(filepath.Dir(target), filepath.Base(target), value)
	}
	data, err := encodeJSON(value)
	if err != nil {
		return err
	}
	_, err = stdout.Write(data)
	return err
}

// writeJSON writes a value as JSON to a file within a directory, creating both as needed.
// The write is sandboxed to the directory.
//
// Takes directory (string) which is the directory the file is written under.
// Takes name (string) which is the file's path relative to directory.
// Takes value (any) which is encoded.
//
// Returns error when the value cannot be encoded or the file cannot be written.
func writeJSON(directory, name string, value any) (err error) {
	data, err := encodeJSON(value)
	if err != nil {
		return err
	}
	sandbox, err := safedisk.NewSandbox(directory, safedisk.ModeReadWrite)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, sandbox.Close()) }()
	if err := sandbox.MkdirAll(filepath.Dir(name), directoryPermissions); err != nil {
		return err
	}
	return sandbox.WriteFile(name, data, filePermissions)
}

// encodeJSON renders a value as indented JSON with a final newline, leaving HTML
// characters unescaped.
//
// Takes value (any) which is encoded.
//
// Returns []byte which is the JSON.
// Returns error when the value cannot be encoded.
func encodeJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// report prints an error and turns it into an exit status.
//
// Takes stderr (io.Writer) which receives the error.
// Takes err (error) which is the outcome.
//
// Returns int which is exitOK for nil and exitFailure otherwise.
func report(stderr io.Writer, err error) int {
	if err == nil {
		return exitOK
	}
	fmt.Fprintln(stderr, "inspect:", err)
	return exitFailure
}
