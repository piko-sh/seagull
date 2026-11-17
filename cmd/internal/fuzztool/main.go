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

//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"seagull.sh/seagull/cmd/internal/fuzztool/intake"
	"seagull.sh/seagull/cmd/internal/fuzztool/reduce"
	"seagull.sh/seagull/cmd/internal/fuzztool/supervise"
)

const (
	// statusFailed is the exit status for a failed subcommand.
	statusFailed = 1

	// statusUsage is the exit status for a malformed command line.
	statusUsage = 2

	// bytesPerMebibyte converts the resource limits.
	bytesPerMebibyte = 1 << 20

	// bytesPerKibibyte converts the disk usage report.
	bytesPerKibibyte = 1 << 10

	// defaultAttempts is the reducer's default attempt budget.
	defaultAttempts = 2000

	// defaultSeconds is the reducer's default time budget.
	defaultSeconds = 3600

	// defaultRunTimeout is the default bound on one replay, in seconds.
	defaultRunTimeout = 300

	// defaultConfirmations is how many times a failure must reproduce by default.
	defaultConfirmations = 3
)

// errUsage marks a malformed command line.
var errUsage = errors.New("usage")

// streams holds a subcommand's output streams.
type streams struct {
	// stdout receives results.
	stdout io.Writer

	// stderr receives diagnostics.
	stderr io.Writer
}

// main runs the requested subcommand and exits with its status.
func main() {
	os.Exit(run(context.Background(), os.Args[1:], streams{stdout: os.Stdout, stderr: os.Stderr}))
}

// run dispatches one subcommand.
//
// Takes ctx (context.Context) which bounds external commands.
// Takes arguments ([]string) which is the command line after the program name.
// Takes output (streams) which receives results and diagnostics.
//
// Returns int which is the exit status.
func run(ctx context.Context, arguments []string, output streams) int {
	if len(arguments) == 0 {
		fmt.Fprintln(output.stderr, "usage: fuzztool command|disk-usage|intake|provenance|reduce ...")
		return statusUsage
	}
	rest := arguments[1:]
	switch arguments[0] {
	case "command":
		return runCommand(rest, output)
	case "disk-usage":
		return runDiskUsage(rest, output)
	case "intake":
		return runIntake(rest, output)
	case "provenance":
		return runProvenance(ctx, rest, output)
	case "reduce":
		return runReduce(ctx, rest, output)
	default:
		fmt.Fprintf(output.stderr, "fuzztool: unknown subcommand %s\n", arguments[0])
		return statusUsage
	}
}

// runCommand supervises one command under the limits the campaign exports.
//
// Takes arguments ([]string) which is the command line to run.
// Takes output (streams) which receives notices and failures.
//
// Returns int which is the command's status or supervise.StatusFailClosed.
func runCommand(arguments []string, output streams) int {
	config, err := commandConfig(os.LookupEnv)
	if err != nil {
		fmt.Fprintf(output.stderr, "Fuzz command environment is incomplete: %v\n", err)
		return supervise.StatusFailClosed
	}
	signals := make(chan os.Signal, 1)

	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	return supervise.New(config, arguments, signals, output.stdout).Run(output.stderr)
}

// commandConfig reads the supervisor's limits and locations from the environment.
//
// Takes lookup (func(string) (string, bool)) which reads one variable.
//
// Returns supervise.Config which holds the limits.
// Returns error which names the first missing or malformed variable.
func commandConfig(lookup func(string) (string, bool)) (supervise.Config, error) {
	values := make(map[string]string)
	for _, name := range []string{"SEAGULL_FUZZ_MANAGED_ROOT", "SEAGULL_FUZZ_METADATA_DIR", "SEAGULL_FUZZ_MIN_FREE_MB",
		"SEAGULL_FUZZ_MAX_DISK_MB", "GOMODCACHE", "TMPDIR"} {
		value, found := lookup(name)
		if !found {
			return supervise.Config{}, fmt.Errorf("%w: %s is not set", errUsage, name)
		}
		values[name] = value
	}
	minimum, err := strconv.ParseUint(values["SEAGULL_FUZZ_MIN_FREE_MB"], 10, 32)
	if err != nil {
		return supervise.Config{}, fmt.Errorf("SEAGULL_FUZZ_MIN_FREE_MB: %w", err)
	}
	maximum, err := strconv.ParseUint(values["SEAGULL_FUZZ_MAX_DISK_MB"], 10, 32)
	if err != nil {
		return supervise.Config{}, fmt.Errorf("SEAGULL_FUZZ_MAX_DISK_MB: %w", err)
	}
	return supervise.Config{
		ManagedRoot:         values["SEAGULL_FUZZ_MANAGED_ROOT"],
		MetadataDirectory:   values["SEAGULL_FUZZ_METADATA_DIR"],
		Locations:           []string{values["GOMODCACHE"], values["TMPDIR"]},
		MinimumFreeBytes:    minimum * bytesPerMebibyte,
		MaximumManagedBytes: maximum * bytesPerMebibyte,
	}, nil
}

// runDiskUsage prints a tree's allocated size in KiB, rounded up.
//
// Takes arguments ([]string) which holds the one directory to measure.
// Takes output (streams) which receives the size or the failure.
//
// Returns int which is 0, statusFailed or statusUsage.
func runDiskUsage(arguments []string, output streams) int {
	if len(arguments) != 1 {
		fmt.Fprintln(output.stderr, "usage: fuzztool disk-usage DIR")
		return statusUsage
	}
	used, err := supervise.AllocatedBytes(arguments[0])
	if err != nil {
		fmt.Fprintf(output.stderr, "Fuzz storage could not be measured: %v\n", err)
		return statusFailed
	}
	fmt.Fprintln(output.stdout, (used+bytesPerKibibyte-1)/bytesPerKibibyte)
	return 0
}

// runIntake admits an external suite under the output root, in a directory named
// after the suite.
//
// Takes arguments ([]string) which holds the flags.
// Takes output (streams) which receives the report or the failure.
//
// Returns int which is 0, statusFailed or statusUsage.
func runIntake(arguments []string, output streams) int {
	flags := newFlagSet("intake", output)
	source := flags.String("source", "", "the directory holding the suite's files")
	manifestPath := flags.String("manifest", "", "the JSON file describing the suite")
	outputRoot := flags.String("output-root", "", "the directory receiving a new directory named after the suite")
	if err := parse(flags, arguments, "source", "manifest", "output-root"); err != nil {
		return statusUsage
	}
	manifest, err := intake.ReadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintf(output.stderr, "fuzz-intake: %v\n", err)
		return statusUsage
	}
	report, err := intake.Run(*source, manifest, filepath.Join(*outputRoot, manifest.Name))
	if errors.Is(err, intake.ErrOutputExists) {
		fmt.Fprintf(output.stderr, "fuzz-intake: %v\n", err)
		return statusUsage
	}
	if err != nil {
		fmt.Fprintf(output.stderr, "fuzz-intake: %v\n", err)
		return statusFailed
	}
	fmt.Fprintf(output.stdout, "admitted %d files and %d seeds, rejected %d files, skipped %d files beyond the file limit and %d duplicate seeds\n",
		len(report.Admitted), report.Seeds, len(report.Rejected), report.BeyondFileLimit, report.Duplicates)
	return 0
}

// runReduce reduces a failing corpus input.
//
// Takes ctx (context.Context) which bounds the build.
// Takes arguments ([]string) which holds the flags.
// Takes output (streams) which receives the summary and diagnostics.
//
// Returns int which is the reducer's exit status, or statusUsage.
func runReduce(ctx context.Context, arguments []string, output streams) int {
	flags := newFlagSet("reduce", output)
	options := reduce.Options{
		Go: "", Module: "", Package: "", Function: "", Tags: "", Input: "", Output: "",
		Attempts: 0, Seconds: 0, RunTimeout: 0, Confirmations: 0,
	}
	flags.StringVar(&options.Go, "go", "go", "the Go command")
	flags.StringVar(&options.Module, "module", "", "the module directory holding the package")
	flags.StringVar(&options.Package, "package", "", "the package path relative to the module")
	flags.StringVar(&options.Function, "function", "", "the fuzz target's function name")
	flags.StringVar(&options.Tags, "tags", "", "the build tags")
	flags.StringVar(&options.Input, "input", "", "the failing corpus file")
	flags.StringVar(&options.Output, "output", "", "a new directory for the results")
	flags.IntVar(&options.Attempts, "attempts", defaultAttempts, "the most candidates to try")
	flags.IntVar(&options.Seconds, "seconds", defaultSeconds, "the most seconds to spend")
	flags.IntVar(&options.RunTimeout, "run-timeout", defaultRunTimeout, "the most seconds one replay may take")
	flags.IntVar(&options.Confirmations, "confirmations", defaultConfirmations, "how many times each failure must reproduce")
	if err := parse(flags, arguments, "module", "package", "function", "input", "output"); err != nil {
		return statusUsage
	}
	if options.Attempts < 1 || options.Seconds < 1 || options.RunTimeout < 1 || options.Confirmations < 1 {
		fmt.Fprintln(output.stderr, "fuzz-reduce: budgets must be positive")
		return statusUsage
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	return reduce.Run(ctx, options, signals, output.stdout, output.stderr)
}

// newFlagSet returns a flag set that reports errors instead of exiting.
//
// Takes name (string) which names the subcommand.
// Takes output (streams) which receives usage messages.
//
// Returns *flag.FlagSet which is ready for flags.
func newFlagSet(name string, output streams) *flag.FlagSet {
	flags := flag.NewFlagSet("fuzztool "+name, flag.ContinueOnError)
	flags.SetOutput(output.stderr)
	return flags
}

// parse parses flags, refusing positional arguments and missing required flags.
//
// Takes flags (*flag.FlagSet) which holds the defined flags.
// Takes arguments ([]string) which is the command line.
// Takes required (...string) which names the flags that must be given.
//
// Returns error wrapping errUsage when the command line is malformed.
func parse(flags *flag.FlagSet, arguments []string, required ...string) error {
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(flags.Output(), "%s: unexpected argument %s\n", flags.Name(), flags.Arg(0))
		return errUsage
	}
	given := make(map[string]bool)
	flags.Visit(func(defined *flag.Flag) { given[defined.Name] = true })
	for _, name := range required {
		if !given[name] {
			fmt.Fprintf(flags.Output(), "%s: -%s is required\n", flags.Name(), name)
			return errUsage
		}
	}
	return nil
}
