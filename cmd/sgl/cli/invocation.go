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
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/cmd/sgl/internal/output"
	"seagull.sh/seagull/cmd/sgl/internal/redact"
)

const (
	// defaultLogLevel is the level the migration and seed run time logs at when --log-level
	// is absent. Its warnings, such as a seed file that changed after it was applied, appear
	// nowhere else.
	defaultLogLevel = slog.LevelWarn

	// defaultGeneratorLogLevel is the level the generator logs at when --log-level is
	// absent. Whatever it warns about reaches the user as a diagnostic or as an error sgl
	// prints itself, so its warnings would only repeat them.
	defaultGeneratorLogLevel = slog.LevelError

	// configFlag is the global flag naming the configuration file.
	configFlag = "c"

	// databaseFlag is the flag that restricts a command to one database.
	databaseFlag = "database"

	// logLevelFlag is the global flag that sets the log verbosity.
	logLevelFlag = "log-level"
)

// invocation is one run of the CLI: the streams, the global flags and the options the
// binary was built with.
type invocation struct {
	// streams holds the standard streams. Stderr is the redactor.
	streams output.IO

	// input reads the answers to prompts from standard input; it is created on the first
	// prompt.
	input *bufio.Reader

	// terminal reports whether a file descriptor is a terminal, which is when sgl asks for
	// confirmation.
	terminal func(descriptor int) bool

	// redactor wraps the error stream and hides every connection string registered with it,
	// so driver errors and log records cannot print a password.
	redactor *redact.Writer

	// logger receives the migration and seed run time's log records.
	logger *slog.Logger

	// generatorLogger receives the generator's log records.
	generatorLogger *slog.Logger

	// settings holds the registered dialects, emitters and drivers.
	settings *settings

	// program is the name the CLI prints for itself, set with WithName.
	program string

	// configPath is the -c value, or empty to discover sgl.yaml.
	configPath string

	// database is the --database value, or empty for every database.
	database string

	// directory is the working directory.
	directory string
}

// newInvocation parses the global flags in front of the command.
//
// Takes configured (*settings) which holds the options.
// Takes arguments ([]string) which holds the arguments after the program name.
//
// Returns *invocation which is ready to dispatch.
// Returns []string which holds the command and its arguments.
// Returns error when a global flag is malformed.
func newInvocation(configured *settings, arguments []string) (*invocation, []string, error) {
	flags := flag.NewFlagSet(configured.program, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String(configFlag, "", "configuration file")
	database := flags.String(databaseFlag, "", "database to use")
	levelName := flags.String(logLevelFlag, "", "log level")
	wantsHelp := flags.Bool("h", false, "help")
	flags.BoolVar(wantsHelp, "help", false, "help")
	wantsVersion := flags.Bool("v", false, "version")
	flags.BoolVar(wantsVersion, "version", false, "version")
	if err := flags.Parse(arguments); err != nil {
		return nil, nil, err
	}
	rest := flags.Args()
	switch {
	case *wantsHelp:
		rest = []string{helpCommand}
	case *wantsVersion:
		rest = []string{versionCommand}
	}
	level, generatorLevel, err := logLevels(*levelName)
	if err != nil {
		return nil, nil, err
	}

	directory, err := filepath.Abs(configured.directory)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the working directory: %w", err)
	}

	redactor := redact.NewWriter(configured.stderr)
	streams := output.IO{Stdin: configured.stdin, Stdout: configured.stdout, Stderr: redactor}
	return &invocation{
		streams:         streams,
		input:           nil,
		terminal:        configured.terminal,
		redactor:        redactor,
		logger:          newLogger(streams.Stderr, level),
		generatorLogger: newLogger(streams.Stderr, generatorLevel),
		settings:        configured,
		program:         configured.program,
		configPath:      *configPath,
		database:        *database,
		directory:       directory,
	}, rest, nil
}

// fail prints an error, and the command that resolves it when there is one, and returns
// the exit status it calls for.
//
// An error that cancellation ended prints as interrupted, followed by what it says beyond
// the cancellation, such as the migration that was running, when it says more.
//
// Takes err (error) which is the failure.
//
// Returns int which is the exit status.
func (inv *invocation) fail(err error) int {
	if errors.Is(err, context.Canceled) {
		if detail := cancellationDetail(err); detail != "" {
			fmt.Fprintf(inv.streams.Stderr, "%s: interrupted: %s\n", inv.program, detail)
			inv.writeHint(err)
		} else {
			fmt.Fprintf(inv.streams.Stderr, "%s: interrupted\n", inv.program)
		}
		return exitInterrupted
	}
	fmt.Fprintf(inv.streams.Stderr, errorFormat, inv.program, err)
	inv.writeHint(err)
	return exitCodeFor(err)
}

// writeHint prints the command that resolves an error, when there is one.
//
// Takes err (error) which is the failure.
func (inv *invocation) writeHint(err error) {
	if hint := commandHint(err, inv.program); hint != "" {
		fmt.Fprintf(inv.streams.Stderr, hintFormat, hint)
	}
}

// path resolves a path given on the command line against the working directory.
//
// Takes name (string) which is the path as given.
//
// Returns string which is absolute.
func (inv *invocation) path(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(inv.directory, name)
}

// display shortens a path for printing: relative to the working directory when it is
// inside it.
//
// Takes name (string) which is an absolute path.
//
// Returns string which is the path to print.
func (inv *invocation) display(name string) string {
	relative, err := filepath.Rel(inv.directory, name)
	if err != nil || strings.HasPrefix(relative, "..") {
		return name
	}
	return filepath.ToSlash(relative)
}

// exitError carries an exit status chosen where the error was raised.
type exitError struct {
	// err is the underlying error.
	err error

	// code is the exit status.
	code int
}

// Error returns the underlying error's message.
//
// Returns string which is the message.
func (e *exitError) Error() string {
	return e.err.Error()
}

// Unwrap returns the underlying error.
//
// Returns error which is the wrapped error.
func (e *exitError) Unwrap() error {
	return e.err
}

// usageError marks an error as a usage error, exit status 2.
//
// Takes format (string) which is the message format.
// Takes arguments (...any) which fill the format.
//
// Returns error which carries exit status 2.
func usageError(format string, arguments ...any) error {
	return &exitError{err: fmt.Errorf(format, arguments...), code: exitUsage}
}

// databaseError marks an error as a database failure, exit status 4.
//
// Takes err (error) which is the failure.
//
// Returns error which carries exit status 4, or nil when err is nil.
func databaseError(err error) error {
	if err == nil {
		return nil
	}
	return &exitError{err: err, code: exitDatabase}
}

// newLogger creates a logger that writes text records at or above a level.
//
// Takes writer (io.Writer) which receives the records.
// Takes level (slog.Level) which is the least severe level written.
//
// Returns *slog.Logger which is ready to use.
func newLogger(writer io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: level, AddSource: false, ReplaceAttr: nil}))
}

// logLevels maps a --log-level value to the levels the migration and seed run time and
// the generator log at. An explicit value sets both; without one, each keeps its default.
//
// Takes name (string) which is the --log-level value, or empty when it is absent.
//
// Returns slog.Level which is the migration and seed run time's level.
// Returns slog.Level which is the generator's level.
// Returns error when the name is unknown.
func logLevels(name string) (runTime, generator slog.Level, err error) {
	if name == "" {
		return defaultLogLevel, defaultGeneratorLogLevel, nil
	}
	level, err := parseLogLevel(name)
	return level, level, err
}

// parseLogLevel maps a --log-level value to its slog level.
//
// Takes name (string) which is debug, info, warn or error.
//
// Returns slog.Level which is the level.
// Returns error when the name is unknown.
func parseLogLevel(name string) (slog.Level, error) {
	switch strings.ToLower(name) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return defaultLogLevel, fmt.Errorf("unknown --%s %q: want debug, info, warn or error", logLevelFlag, name)
	}
}

// exitCodeFor chooses the exit status for an error.
//
// Takes err (error) which is the failure.
//
// Returns int which is the exit status.
func exitCodeFor(err error) int {
	if coded, isCoded := errors.AsType[*exitError](err); isCoded {
		return coded.code
	}
	switch {
	case errors.Is(err, config.ErrInvalid), errors.Is(err, config.ErrNotFound), errors.Is(err, config.ErrTooLarge),
		errors.Is(err, config.ErrUnsetVariable), errors.Is(err, seagull.ErrInvalidConfig),
		errors.Is(err, fs.ErrNotExist), errors.Is(err, errFileTooLarge), errors.Is(err, errNotRegularFile),
		errors.Is(err, errTooManyEntries):
		return exitUsage
	default:
		return exitInternal
	}
}

// cancellationDetail returns what an error says beyond the cancellation that ended it.
//
// Takes err (error) which matches context.Canceled.
//
// Returns string which is the error's message, or empty when the message is only the
// cancellation's own: the context's error, or the signal that caused it.
func cancellationDetail(err error) string {
	message := err.Error()
	if message == context.Canceled.Error() {
		return ""
	}
	if interrupt, interrupted := errors.AsType[*interruptError](err); interrupted && message == interrupt.Error() {
		return ""
	}
	return message
}
