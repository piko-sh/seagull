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

// Package cli is the importable shell of the sgl command.
//
// The stock sgl binary is a one-line wrapper around [Main] that also registers the stock
// database drivers. The cli package links no database driver itself. A project that needs
// a customised sgl, for example with an extra dialect, emitter or a cgo database driver,
// depends on cli, imports the drivers it needs, passes the extra pieces as options
// ([WithDialect], [WithEmitter], [WithDriver]) and calls [Main] from its own main
// package. [WithName] sets the name the CLI prints for itself, which is how the seagull
// command runs the same tool under the project's name.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
)

const (
	// defaultProgram is the name the CLI prints for itself unless WithName sets another.
	defaultProgram = "sgl"

	// errorFormat is the shared template written to stderr for CLI errors: the program name,
	// then the error.
	errorFormat = "%s: %v\n"

	// hintFormat is the template of the line naming the command that resolves an error,
	// written under it.
	hintFormat = "hint: %s\n"

	// exitOK is the status for a successful run.
	exitOK = 0

	// exitFindings is the status when the command found problems: error diagnostics, or
	// generated code that is out of date under --check.
	exitFindings = 1

	// exitUsage is the status for a malformed command line or an unusable configuration.
	exitUsage = 2

	// exitInternal is the status for a failure inside sgl itself.
	exitInternal = 3

	// exitDatabase is the status for a database that cannot be reached or a migration that
	// fails.
	exitDatabase = 4

	// exitInterrupted is the status for a cancelled run, and for termination by SIGINT,
	// which is 128 + 2. A run another signal ended exits with 128 plus its number.
	exitInterrupted = 130

	// helpCommand is the command that prints the overview.
	helpCommand = "help"

	// versionCommand is the command that prints the versions.
	versionCommand = "version"

	// directoryPermissions is the mode of directories sgl creates.
	directoryPermissions = 0o750

	// sourceFilePermissions is the mode of SQL files sgl creates, readable like any other
	// source file.
	sourceFilePermissions = 0o644
)

// signalWatcher turns interrupts into cancellation and, on a second interrupt, a forced
// exit.
type signalWatcher struct {
	// signals delivers interrupts.
	signals <-chan os.Signal

	// done is closed when the command has finished.
	done <-chan struct{}

	// stderr receives the messages about interrupts.
	stderr io.Writer

	// cancel cancels the command's context with the signal as its cause.
	cancel context.CancelCauseFunc

	// forceExit ends the process.
	forceExit func()

	// program is the name the messages start with.
	program string
}

// watch waits for the first interrupt and cancels the command, then waits for a second
// interrupt and forces an exit. It returns as soon as the command finishes, whichever
// wait it is in.
func (w *signalWatcher) watch() {
	select {
	case received := <-w.signals:
		cause := &interruptError{signal: received}
		w.cancel(cause)
		fmt.Fprintf(w.stderr, "\n%s: %v; cancelling. Press Ctrl-C again to force-exit.\n", w.program, cause)
	case <-w.done:
		return
	}
	select {
	case <-w.signals:
		fmt.Fprintf(w.stderr, "%s: forcing exit.\n", w.program)
		w.forceExit()
	case <-w.done:
	}
}

// internalError reports a panic that escaped a command. Its stack is logged once at
// warning level and a line without it is printed, both through the redactor, so a
// connection string the panic quotes is hidden.
//
// Takes recovered (any) which is the value recover returned.
//
// Returns int which is the internal-error exit status.
func (inv *invocation) internalError(ctx context.Context, recovered any) int {
	inv.logger.WarnContext(ctx, "recovered a panic",
		slog.String("panic", fmt.Sprint(recovered)),
		slog.String("stack", string(debug.Stack())),
	)
	fmt.Fprintf(inv.streams.Stderr, "%s: internal error: %v\n", inv.program, recovered)
	return exitInternal
}

// dispatch routes the arguments to a command.
//
// Takes arguments ([]string) which starts with the command name.
//
// Returns int which is the command's exit status.
func (inv *invocation) dispatch(ctx context.Context, arguments []string) int {
	if len(arguments) == 0 {
		printHelp(inv.streams.Stdout, inv.program)
		return exitUsage
	}
	command, rest := arguments[0], arguments[1:]
	switch command {
	case "-h", "--help", "-help", helpCommand:
		printHelp(inv.streams.Stdout, inv.program)
		return exitOK
	case "-v", "--version", "-version", versionCommand:
		return inv.runVersion(ctx, rest)
	case "init":
		return inv.runInit(ctx, rest)
	case "generate":
		return inv.runGenerate(ctx, rest)
	case "vet":
		return inv.runVet(ctx, rest)
	case "fmt":
		return inv.runFmt(ctx, rest)
	case "explain":
		return inv.runExplain(ctx, rest)
	case "describe":
		return inv.runDescribe(ctx, rest)
	default:
		return inv.dispatchDatabaseCommand(ctx, command, rest)
	}
}

// dispatchDatabaseCommand routes the commands that talk to a database or describe the
// configuration.
//
// Takes command (string) which is the command name.
// Takes rest ([]string) which holds the command's arguments.
//
// Returns int which is the command's exit status.
func (inv *invocation) dispatchDatabaseCommand(ctx context.Context, command string, rest []string) int {
	switch command {
	case "schema":
		return inv.runSchema(ctx, rest)
	case "migrate":
		return inv.runMigrate(ctx, rest)
	case "seed":
		return inv.runSeed(ctx, rest)
	case "dialects":
		return inv.runDialects(ctx, rest)
	case "config":
		return inv.runConfig(ctx, rest)
	default:
		fmt.Fprintf(inv.streams.Stderr, errorFormat, inv.program, fmt.Errorf("unknown command %q; run `%s help`", command, inv.program))
		return exitUsage
	}
}

// Main is the canonical CLI entry point.
//
// The first SIGINT or SIGTERM cancels the run, using the signal as the cancellation
// cause. A second signal ends the process immediately. A run ended by a signal exits with
// 128 plus the signal number: 130 for SIGINT and 143 for SIGTERM.
//
// Takes arguments ([]string) which holds the arguments after the program name.
// Takes options (...Option) which add dialects, emitters and drivers.
//
// Returns int which is the exit status the process should end with.
func Main(arguments []string, options ...Option) int {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	return mainWithSignals(signals, func() { os.Exit(exitInterrupted) }, arguments, options...)
}

// MainContext runs one command under a context the caller owns, for tests and for
// programs that drive cancellation themselves.
//
// A run whose context was cancelled with an interrupt cause, as Main's watcher does,
// exits with 128 plus the signal's number. A panic that escapes the command is recovered:
// its stack is logged once at warning level and the run exits with status 3.
//
// Takes arguments ([]string) which holds the arguments after the program name.
// Takes options (...Option) which add dialects, emitters, drivers and streams.
//
// Returns int which is the exit status.
func MainContext(ctx context.Context, arguments []string, options ...Option) int {
	return run(ctx, newSettings(options), arguments)
}

// mainWithSignals runs one command, cancelling it on the first signal and calling
// forceExit on the second.
//
// Takes signals (<-chan os.Signal) which delivers interrupts.
// Takes forceExit (func()) which ends the process on a second interrupt.
// Takes arguments ([]string) which holds the arguments after the program name.
// Takes options (...Option) which add dialects, emitters, drivers and streams; the
// watcher writes its messages to the error stream they choose.
//
// Returns int which is the command's exit status.
//
// Concurrency: the watcher goroutine it starts has returned, or is about to, by the time
// it returns.
func mainWithSignals(signals <-chan os.Signal, forceExit func(), arguments []string, options ...Option) int {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	done := make(chan struct{})
	defer close(done)

	configured := newSettings(options)
	watcher := &signalWatcher{
		signals:   signals,
		done:      done,
		cancel:    cancel,
		forceExit: forceExit,
		stderr:    configured.stderr,
		program:   configured.program,
	}
	go watcher.watch()

	return run(ctx, configured, arguments)
}

// run runs one command with the settings the options configured.
//
// Takes configured (*settings) which holds the options.
// Takes arguments ([]string) which holds the arguments after the program name.
//
// Returns status (int) which is the exit status.
func run(ctx context.Context, configured *settings, arguments []string) (status int) {
	if err := configured.err(); err != nil {
		fmt.Fprintf(configured.stderr, errorFormat, configured.program, err)
		return exitUsage
	}
	inv, rest, err := newInvocation(configured, arguments)
	if err != nil {
		fmt.Fprintf(configured.stderr, errorFormat, configured.program, err)
		return exitUsage
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			status = inv.internalError(ctx, recovered)
		}
	}()
	return interruptedStatus(ctx, inv.dispatch(ctx, rest))
}

// printHelp writes the command overview.
//
// Takes writer (io.Writer) which receives the overview.
// Takes program (string) which is the name the overview gives the CLI.
func printHelp(writer io.Writer, program string) {
	const help = `%[1]s turns plain SQL into type-safe Go.

Usage:
  %[1]s [global flags] <command> [flags] [args]

Commands:
  init        Write sgl.yaml, a first migration and an example query
  generate    Generate Go code for every database
  vet         Analyse migrations and queries and report problems
  fmt         Canonicalise directive comments in SQL files
  explain     Explain a diagnostic code, such as Q012
  describe    Show a query's parameters, columns and Go signature
  schema      Dump the schema, or compare it with a live database
  migrate     Apply, roll back, inspect, validate or create migrations
  seed        Apply and inspect seed data
  dialects    List dialects and their capabilities
  config      Print the configuration JSON Schema, or validate a configuration
  version     Print the CLI, library, generated-code and Go versions
  help        Show this help

Global flags, accepted before the command:
  -c path                Use this configuration file instead of searching for one
  --database name        Restrict the command to one database
  --log-level level      Log verbosity on stderr: debug, info, warn or error. Without it,
                         migrate and seed log warnings and code generation only errors,
                         since its warnings are printed as diagnostics

Run "%[1]s <command> -h" for a command's flags.
`
	fmt.Fprintf(writer, help, program)
}

// interruptedStatus turns the status of a run a signal ended into the status a shell
// reports for a process that signal killed.
//
// Takes status (int) which is the run's status.
//
// Returns int which is 128 plus the signal's number when a signal cancelled the run, and
// status otherwise.
func interruptedStatus(ctx context.Context, status int) int {
	if status != exitInterrupted {
		return status
	}
	interrupt, interrupted := errors.AsType[*interruptError](context.Cause(ctx))
	if !interrupted {
		return status
	}
	return interrupt.exitStatus()
}
