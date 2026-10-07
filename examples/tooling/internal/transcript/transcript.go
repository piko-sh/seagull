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

// Package transcript replays the terminal session a tooling example records in its
// transcript.txt, running sgl in-process, and fails when a command's output or exit
// status no longer matches the recording.
//
// A transcript is a shell session:
//
//	# Lines starting with # narrate; blank lines separate steps.
//	$ sgl vet --strict
//	db/queries/tasks.sql:4:1: warning[Q009]: ...
//	# exit 1
//
// A line starting "$ " is a command. The lines after it, up to "# exit N", are what it
// printed, standard output and standard error together in the order they were written,
// and N is its exit status. Commands starting "sgl" or "go tool sgl" run through
// cli.MainContext; cat, cd, cp and mkdir are carried out by the test itself. Arguments
// are separated by spaces, with no quoting.
//
// The session runs in a temporary copy of the example's directory, so commands that write
// files never touch the committed ones. Times such as 2026-05-01 09:30:00 are recorded as
// YYYY-MM-DD hh:mm:ss, since they change on every run.
//
// Run the tests with -update to record every command's output again.
package transcript

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite" // registers the sqlite driver the stock sgl opens for SQLite

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/internal/leakcheck"
)

const (
	// File is the recording beside each tooling example's test.
	File = "transcript.txt"

	// commandPrefix starts a command line.
	commandPrefix = "$ "

	// timePlaceholder replaces every time in the output.
	timePlaceholder = "YYYY-MM-DD hh:mm:ss"

	// directoryPermissions is the mode of directories the session creates.
	directoryPermissions = 0o750

	// filePermissions is the mode of files the session copies.
	filePermissions = 0o644
)

var (
	// update records the transcript again instead of comparing against it.
	update = flag.Bool("update", false, "record transcript.txt again from the commands' output")

	// exitPattern matches the line that ends a command's output.
	exitPattern = regexp.MustCompile(`^# exit (\d+)$`)

	// timePattern matches the applied times migrate status and seed status print.
	timePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)

	// stockDrivers names the database/sql driver the stock sgl binary opens for each dialect
	// family, so `sgl dialects` prints what it prints. Only the SQLite driver is linked
	// here; the examples use no other database.
	stockDrivers = []cli.Option{
		cli.WithDriver("clickhouse", "clickhouse"),
		cli.WithDriver("mysql", "mysql"),
		cli.WithDriver("postgres", "pgx"),
		cli.WithDriver("sqlite", "sqlite"),
	}
)

// Prepare readies the working copy before the first command, for state the session cannot
// create itself, such as a database that has drifted from its migrations.
type Prepare func(t *testing.T, directory string)

// step is one command of a session.
type step struct {
	// command is the command line without its "$ " prefix.
	command string

	// narration holds the comment and blank lines before the command.
	narration []string

	// output holds the lines the command printed.
	output []string

	// exit is the command's exit status.
	exit int
}

// session is a parsed transcript.
type session struct {
	// steps holds the commands in order.
	steps []step

	// trailer holds the comment and blank lines after the last command.
	trailer []string
}

// render writes a session back in transcript form.
//
// Returns string which is the transcript.
func (s *session) render() string {
	var text strings.Builder
	for _, current := range s.steps {
		text.WriteString(joinLines(current.narration))
		text.WriteString(commandPrefix + current.command + "\n")
		text.WriteString(joinLines(current.output))
		fmt.Fprintf(&text, "# exit %d\n", current.exit)
	}
	text.WriteString(joinLines(s.trailer))
	return text.String()
}

// shell carries out a session's commands.
type shell struct {
	// t is the test the session belongs to.
	t *testing.T

	// directory is the current working directory.
	directory string
}

// run carries out one command line.
//
// Takes command (string) which is the command line.
//
// Returns string which is what the command printed.
// Returns int which is its exit status.
func (s *shell) run(command string) (string, int) {
	for _, prefix := range []string{"sgl", "go tool sgl"} {
		if command == prefix || strings.HasPrefix(command, prefix+" ") {
			return s.sgl(strings.Fields(strings.TrimPrefix(command, prefix)))
		}
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		s.t.Fatalf("an empty command")
	}

	output, err := s.builtin(fields[0], fields[1:])
	if err != nil {
		s.t.Fatalf("$ %s: %v", command, err)
	}
	return output, 0
}

// sgl runs the sgl command in-process in the current directory.
//
// Takes arguments ([]string) which holds the arguments after "sgl".
//
// Returns string which is what sgl printed to standard output and standard error.
// Returns int which is its exit status.
func (s *shell) sgl(arguments []string) (string, int) {
	var output bytes.Buffer
	options := append([]cli.Option{
		cli.WithDirectory(s.directory),
		cli.WithIO(strings.NewReader(""), &output, &output),
	}, stockDrivers...)
	exit := cli.MainContext(s.t.Context(), arguments, options...)
	return output.String(), exit
}

// builtin carries out one of the file commands the test performs itself.
//
// Takes name (string) which is cat, cd, cp or mkdir.
// Takes arguments ([]string) which holds its arguments.
//
// Returns string which is what the command printed.
// Returns error when the command is unknown or fails.
func (s *shell) builtin(name string, arguments []string) (string, error) {
	switch {
	case name == "cat" && len(arguments) > 0:
		var output strings.Builder
		for _, argument := range arguments {
			content, err := os.ReadFile(s.path(argument))
			if err != nil {
				return "", err
			}
			output.Write(content)
		}
		return output.String(), nil
	case name == "cd" && len(arguments) == 1:
		s.directory = s.path(arguments[0])
		s.t.Chdir(s.directory)
		return "", nil
	case name == "cp" && len(arguments) == 2:
		target := s.path(arguments[1])
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			target = filepath.Join(target, filepath.Base(arguments[0]))
		}
		return "", copyFile(s.path(arguments[0]), target)
	case name == "mkdir" && len(arguments) == 1:
		return "", os.Mkdir(s.path(arguments[0]), directoryPermissions)
	default:
		return "", errors.New("unknown command; a transcript runs sgl, go tool sgl, cat, cd, cp and mkdir")
	}
}

// path resolves a command-line path against the current directory.
//
// Takes name (string) which is a slash-separated path.
//
// Returns string which is the absolute path.
func (s *shell) path(name string) string {
	return filepath.Join(s.directory, filepath.FromSlash(name))
}

// Replay runs every command of the transcript.txt in the working directory against a
// temporary copy of that directory, and compares what each printed with the recording.
// With -update it writes the recording again instead. The test also fails when a
// goroutine is still running once it and its clean-ups have finished.
//
// Takes t (*testing.T) which is the test.
// Takes prepare (Prepare) which readies the copy before the first command, or is nil.
func Replay(t *testing.T, prepare Prepare) {
	t.Helper()
	checkLeaks(t)
	recorded, err := os.ReadFile(File)
	if err != nil {
		t.Fatalf("reading %s: %v", File, err)
	}
	parsed, err := parse(string(recorded))
	if err != nil {
		t.Fatalf("%s: %v", File, err)
	}

	source, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), filepath.Base(source))
	if err := copyTree(source, root); err != nil {
		t.Fatalf("copying the example: %v", err)
	}
	t.Setenv("SGL_DATABASE_URL", "")
	t.Chdir(root)
	if prepare != nil {
		prepare(t, root)
	}

	shell := &shell{t: t, directory: root}
	for index := range parsed.steps {
		current := &parsed.steps[index]
		output, exit := shell.run(current.command)
		lines := splitLines(timePattern.ReplaceAllString(output, timePlaceholder))
		if *update {
			current.output, current.exit = lines, exit
			continue
		}
		if exit != current.exit || !slices.Equal(lines, current.output) {
			t.Errorf("$ %s\n--- got (exit %d)\n%s--- want (exit %d)\n%s(run with -update if the change is intended)",
				current.command, exit, joinLines(lines), current.exit, joinLines(current.output))
		}
	}

	if *update {
		if err := os.WriteFile(filepath.Join(source, File), []byte(parsed.render()), filePermissions); err != nil {
			t.Fatal(err)
		}
	}
}

// parse reads a transcript.
//
// Takes text (string) which is the transcript.
//
// Returns session which holds its commands.
// Returns error when a line outside a command's output is not a comment, or the last
// command has no exit line.
func parse(text string) (session, error) {
	var parsed session
	var current *step
	var narration []string
	for number, line := range splitLines(text) {
		if current == nil {
			if command, found := strings.CutPrefix(line, commandPrefix); found {
				current = &step{command: command, narration: narration, output: nil, exit: 0}
				narration = nil
				continue
			}
			if line != "" && !strings.HasPrefix(line, "#") {
				return session{}, fmt.Errorf("line %d is neither a command, a comment nor a blank line", number+1)
			}
			narration = append(narration, line)
			continue
		}
		if match := exitPattern.FindStringSubmatch(line); match != nil {
			current.exit, _ = strconv.Atoi(match[1])
			parsed.steps = append(parsed.steps, *current)
			current = nil
			continue
		}
		current.output = append(current.output, line)
	}
	if current != nil {
		return session{}, fmt.Errorf("the command %q has no # exit line", current.command)
	}
	parsed.trailer = narration
	return parsed, nil
}

// copyTree copies the example's files into the working copy, leaving out the recording
// itself.
//
// Takes source (string) which is the example's directory.
// Takes target (string) which is the working copy.
//
// Returns error when a file cannot be copied.
func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(target, relative), directoryPermissions)
		}
		if relative == File {
			return nil
		}
		return copyFile(path, filepath.Join(target, relative))
	})
}

// copyFile copies one file.
//
// Takes source (string) which is the file to copy.
// Takes target (string) which is the file to write.
//
// Returns error when the file cannot be read or written.
func copyFile(source, target string) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, content, filePermissions)
}

// splitLines splits text into lines, without a final empty line for a trailing newline.
//
// Takes text (string) which is the text.
//
// Returns []string which holds its lines.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// joinLines joins lines, each ending with a newline.
//
// Takes lines ([]string) which are the lines.
//
// Returns string which is the text.
func joinLines(lines []string) string {
	var text strings.Builder
	for _, line := range lines {
		text.WriteString(line + "\n")
	}
	return text.String()
}

// checkLeaks fails the test when a goroutine is still running once the test and its
// clean-ups have finished. It registers the check before the session's own clean-ups, so
// it runs after them.
//
// Takes t (testing.TB) which is the test.
func checkLeaks(t testing.TB) {
	t.Helper()
	t.Cleanup(func() {
		if err := leakcheck.FindLeaks(); err != nil {
			t.Errorf("goroutines still running after the test: %v", err)
		}
	})
}
