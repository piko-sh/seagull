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

package reduce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"seagull.sh/seagull/cmd/internal/fuzztool/corpus"
	"seagull.sh/seagull/cmd/internal/fuzztool/fuzztext"
)

const (
	// StatusFailed is the exit status for an error during reduction.
	StatusFailed = 1

	// StatusUsage is the exit status for an unusable input or one that does not fail.
	StatusUsage = 2

	// StatusIntermittent is the exit status for an input that fails only sometimes.
	StatusIntermittent = 3

	// signalStatusBase is added to a signal number when a signal stops the reduction.
	signalStatusBase = 128

	// stoppedBudget and stoppedExhausted say why the reduction ended.
	stoppedBudget = "budget"

	// stoppedExhausted is a reduction with no smaller candidate left.
	stoppedExhausted = "exhausted"

	// tenthsPerSecond rounds the summary's duration to a tenth of a second.
	tenthsPerSecond = 10

	// outputMode is the permission of a written result file.
	outputMode = 0o600

	// outputDirectoryMode is the permission of the result directory.
	outputDirectoryMode = 0o750

	// errorFormat prefixes every reducer error.
	errorFormat = "fuzz-reduce: %v\n"
)

// errOutputExists refuses to write over earlier results.
var errOutputExists = errors.New("the output directory already exists")

// Options configures one reduction.
type Options struct {
	// Go is the Go command.
	Go string

	// Module is the module directory holding the package.
	Module string

	// Package is the package path relative to the module.
	Package string

	// Function is the fuzz target's function name.
	Function string

	// Tags are the build tags.
	Tags string

	// Input is the failing corpus file.
	Input string

	// Output is a new directory for the results.
	Output string

	// Attempts bounds the candidates tried.
	Attempts int

	// Seconds bounds the reduction's wall time.
	Seconds int

	// RunTimeout bounds one replay, in seconds.
	RunTimeout int

	// Confirmations is how many times the original and each reduction must fail.
	Confirmations int
}

// jsonFloat writes a float as the earlier tooling did, always with a decimal point.
type jsonFloat float64

// summary is summary.json and the reducer's printed result.
//
//nolint:govet // The field order is the JSON key order the summary has always had.
type summary struct {
	// Function is the fuzz target.
	Function string `json:"function"`

	// Module is the module directory as given.
	Module string `json:"module"`

	// Package is the package path.
	Package string `json:"package"`

	// Tags are the build tags.
	Tags string `json:"tags"`

	// Identity is the failure's identity.
	Identity []string `json:"identity"`

	// OriginalBytes is the original's payload size.
	OriginalBytes int `json:"original_bytes"`

	// ReducedBytes is the reduction's payload size.
	ReducedBytes int `json:"reduced_bytes"`

	// OriginalSHA256 is the hash of the original file.
	OriginalSHA256 string `json:"original_sha256"`

	// ReducedSHA256 is the hash of the reduced file.
	ReducedSHA256 string `json:"reduced_sha256"`

	// Attempts counts the candidates tried.
	Attempts int `json:"attempts"`

	// Stopped says why the reduction ended.
	Stopped string `json:"stopped"`

	// Seconds is the reduction's duration.
	Seconds jsonFloat `json:"seconds"`

	// Confirmations is the required reproduction count.
	Confirmations int `json:"confirmations"`

	// Revision is the checked-out revision.
	Revision string `json:"revision"`

	// Go is the go version output.
	Go string `json:"go"`

	// reducedContent is the reduced corpus file, which the summary does not print.
	reducedContent []byte
}

// MarshalJSON writes the float with at least one decimal place.
//
// Returns []byte which holds the number.
// Returns error which is always nil.
func (value jsonFloat) MarshalJSON() ([]byte, error) {
	text := strconv.FormatFloat(float64(value), 'f', -1, 64)
	if !strings.Contains(text, ".") {
		text += ".0"
	}
	return []byte(text), nil
}

// Run reduces a failing input and writes the original, the reduction and a summary.
//
// Takes ctx (context.Context) which bounds the build and toolchain calls.
// Takes options (Options) which configures the reduction.
// Takes signals (<-chan os.Signal) which delivers external interrupts.
// Takes stdout (io.Writer) which receives the summary.
// Takes stderr (io.Writer) which receives build output and errors.
//
// Returns int which is 0 on success, StatusUsage for an unusable or passing input,
// StatusIntermittent for an intermittent failure, 128 plus the signal for an
// interruption, or StatusFailed for any other error.
func Run(ctx context.Context, options Options, signals <-chan os.Signal, stdout io.Writer, stderr io.Writer) int {
	originalContent, arguments, err := readInput(options)
	if err != nil {
		fmt.Fprintf(stderr, "fuzz-reduce: the input is not a supported corpus file: %v\n", err)
		return StatusUsage
	}
	moduleDirectory, err := filepath.Abs(options.Module)
	if err != nil {
		fmt.Fprintf(stderr, errorFormat, err)
		return StatusFailed
	}
	output, err := createOutput(options.Output)
	if err != nil {
		fmt.Fprintf(stderr, errorFormat, err)
		return StatusFailed
	}
	binary := filepath.Join(output, "target.test")
	if err := build(ctx, options, moduleDirectory, binary, stderr); err != nil {
		fmt.Fprintf(stderr, errorFormat, err)
		return StatusFailed
	}
	run := reduction{
		replayer:  nil,
		arguments: arguments,
		started:   time.Now(),
		options:   options,
	}
	result, status := run.reduce(output, filepath.Join(moduleDirectory, options.Package), signals, stderr)
	if status != 0 {
		return status
	}
	result.OriginalSHA256 = hashBytes(originalContent)
	result.Revision = gitRevision(ctx, moduleDirectory)
	result.Go = goVersion(ctx, options.Go)
	if err := writeResults(output, originalContent, result, stdout); err != nil {
		fmt.Fprintf(stderr, errorFormat, err)
		return StatusFailed
	}
	return 0
}

// reduction holds one reduction's state.
type reduction struct {
	// replayer replays candidates.
	replayer *replayer

	// arguments is the original input.
	arguments []corpus.Argument

	// started is when the reduction began.
	started time.Time

	// options configures the reduction.
	options Options
}

// greedySearch is the state of the greedy search.
type greedySearch struct {
	// deadline ends the search.
	deadline time.Time

	// seen holds every candidate already tried.
	seen map[string]bool

	// best is the smallest verified input.
	best []corpus.Argument

	// attempts counts the candidates tried.
	attempts int

	// spent is true once a budget ran out.
	spent bool
}

// reduce confirms the original failure and then reduces it, cleaning the corpus up.
//
// Takes output (string) which is the result directory.
// Takes packageDirectory (string) which is where the binary runs.
// Takes signals (<-chan os.Signal) which delivers external interrupts.
// Takes stderr (io.Writer) which receives refusals and errors.
//
// Returns summary which describes the result, its hashes still to fill in.
// Returns int which is a non-zero exit status when the reduction did not complete.
func (run *reduction) reduce(output string, packageDirectory string, signals <-chan os.Signal, stderr io.Writer) (summary, int) {
	log, err := os.OpenFile(filepath.Join(output, "attempts.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, outputMode) //nolint:gosec // Inside the new output.
	if err != nil {
		fmt.Fprintf(stderr, "fuzz-reduce: creating the attempt log: %v\n", err)
		return summary{}, StatusFailed
	}
	defer func() { _ = log.Close() }()
	timeout := time.Duration(run.options.RunTimeout) * time.Second
	run.replayer = newReplayer(filepath.Join(output, "target.test"), packageDirectory, run.options.Function, timeout, log, signals)
	defer run.replayer.cleanup()
	result, err := run.search(stderr)
	if refused, isRefused := errors.AsType[exitError](err); isRefused {
		return summary{}, int(refused)
	}
	if stopped, isStopped := errors.AsType[*interruption](err); isStopped {
		return summary{}, signalStatus(stopped.signal)
	}
	if err != nil {
		fmt.Fprintf(stderr, errorFormat, err)
		return summary{}, StatusFailed
	}
	return result, 0
}

// search confirms the original failure and greedily replaces it with smaller
// candidates that fail the same way.
//
// Takes stderr (io.Writer) which receives refusals.
//
// Returns summary which describes the result.
// Returns error when a replay fails, or exitError for a refused input.
func (run *reduction) search(stderr io.Writer) (summary, error) {
	original := corpus.Encode(run.arguments)
	wanted, failed, err := run.replayer.run(original)
	if err != nil {
		return summary{}, err
	}
	if !failed {
		fmt.Fprintln(stderr, "The input does not fail its target.")
		return summary{}, exitError(StatusUsage)
	}
	reproduced, err := run.replayer.reproduces(original, wanted, run.options.Confirmations-1)
	if err != nil {
		return summary{}, err
	}
	if !reproduced {
		fmt.Fprintln(stderr, "The input fails intermittently, so it is not reduced.")
		return summary{}, exitError(StatusIntermittent)
	}
	best, attempts, stopped, err := run.greedy(wanted)
	if err != nil {
		return summary{}, err
	}
	reducedContent := corpus.Encode(best)
	return summary{
		Function:       run.options.Function,
		Module:         run.options.Module,
		Package:        run.options.Package,
		Tags:           run.options.Tags,
		Identity:       wanted.list(),
		OriginalBytes:  corpus.TotalLength(run.arguments),
		ReducedBytes:   corpus.TotalLength(best),
		OriginalSHA256: "",
		ReducedSHA256:  hashBytes(reducedContent),
		Attempts:       attempts,
		Stopped:        stopped,
		Seconds:        roundedSeconds(time.Since(run.started), tenthsPerSecond),
		Confirmations:  run.options.Confirmations,
		Revision:       "",
		Go:             "",
		reducedContent: reducedContent,
	}, nil
}

// greedy repeatedly takes the first smaller candidate that reproduces the failure.
//
// Takes wanted (Identity) which is the original failure.
//
// Returns []corpus.Argument which is the best verified input.
// Returns int which counts the candidates tried.
// Returns string which says why the search stopped.
// Returns error when a replay fails.
func (run *reduction) greedy(wanted Identity) ([]corpus.Argument, int, string, error) {
	search := greedySearch{
		deadline: run.started.Add(time.Duration(run.options.Seconds) * time.Second),
		seen:     map[string]bool{string(corpus.Encode(run.arguments)): true},
		best:     run.arguments,
		attempts: 0,
		spent:    false,
	}
	for {
		improved, err := run.improve(&search, wanted)
		if err != nil {
			return nil, 0, "", err
		}
		if search.spent {
			return search.best, search.attempts, stoppedBudget, nil
		}
		if !improved {
			return search.best, search.attempts, stoppedExhausted, nil
		}
	}
}

// improve tries candidates of the best input until one reproduces the failure.
//
// Takes search (*greedySearch) which holds the search state.
// Takes wanted (Identity) which is the original failure.
//
// Returns bool which is true when a smaller input replaced the best one.
// Returns error when a replay fails.
func (run *reduction) improve(search *greedySearch, wanted Identity) (bool, error) {
	for candidate := range corpus.Candidates(search.best, run.options.Function) {
		if search.attempts >= run.options.Attempts || !time.Now().Before(search.deadline) {
			search.spent = true
			return false, nil
		}
		encoded := corpus.Encode(candidate)
		if search.seen[string(encoded)] || corpus.Complexity(candidate, search.best) >= 0 {
			continue
		}
		search.seen[string(encoded)] = true
		search.attempts++
		accepted, err := run.accepts(encoded, wanted)
		if err != nil {
			return false, err
		}
		if accepted {
			search.best = candidate
			return true, nil
		}
	}
	return false, nil
}

// accepts reports whether a candidate fails the wanted way every confirmation.
//
// Takes encoded ([]byte) which is the candidate corpus file.
// Takes wanted (Identity) which is the original failure.
//
// Returns bool which is true for a verified reduction.
// Returns error when a replay fails.
func (run *reduction) accepts(encoded []byte, wanted Identity) (bool, error) {
	identity, failed, err := run.replayer.run(encoded)
	if err != nil || !failed || identity != wanted {
		return false, err
	}
	return run.replayer.reproduces(encoded, wanted, run.options.Confirmations-1)
}

// exitError carries the exit status of a refused input.
type exitError int

// Error describes the refusal.
//
// Returns string which names the status.
func (status exitError) Error() string {
	return "refused with status " + strconv.Itoa(int(status))
}

// readInput reads and decodes the failing corpus file.
//
// Takes options (Options) which names the input and target.
//
// Returns []byte which holds the file as read.
// Returns []corpus.Argument which holds its arguments.
// Returns error when the file cannot be read or is not a supported input.
func readInput(options Options) ([]byte, []corpus.Argument, error) {
	file, err := os.Open(options.Input)
	if err != nil {
		return nil, nil, fmt.Errorf("opening the input: %w", err)
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, corpus.MaxEnvelope+1))
	if err != nil {
		return nil, nil, fmt.Errorf("reading the input: %w", err)
	}
	arguments, err := corpus.Decode(content)
	if err != nil {
		return nil, nil, err
	}
	if err := corpus.Validate(arguments, options.Function); err != nil {
		return nil, nil, err
	}
	return content, arguments, nil
}

// createOutput creates the new result directory.
//
// Takes output (string) which is the directory to create.
//
// Returns string which is its absolute path.
// Returns error when it exists or cannot be created.
func createOutput(output string) (string, error) {
	absolute, err := filepath.Abs(output)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", output, err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), outputDirectoryMode); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(absolute), err)
	}
	if err := os.Mkdir(absolute, outputDirectoryMode); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%w: %s", errOutputExists, output)
		}
		return "", fmt.Errorf("creating %s: %w", output, err)
	}
	return absolute, nil
}

// build compiles the target's test binary once.
//
// Takes ctx (context.Context) which bounds the build.
// Takes options (Options) which names the package and tags.
// Takes moduleDirectory (string) which is where the build runs.
// Takes binary (string) which is the binary to write.
// Takes stderr (io.Writer) which receives the build's output.
//
// Returns error when the build fails.
func build(ctx context.Context, options Options, moduleDirectory string, binary string, stderr io.Writer) error {
	arguments := []string{"test", "-c", "-o", binary}
	if options.Tags != "" {
		arguments = append(arguments, "-tags", options.Tags)
	}
	arguments = append(arguments, options.Package)
	command := exec.CommandContext(ctx, options.Go, arguments...) //nolint:gosec // Runs the campaign's Go command.
	command.Dir = moduleDirectory
	command.Stdout = stderr
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("building the test binary: %w", err)
	}
	return nil
}

// writeResults writes the original, the reduction and the summary, and prints it.
//
// Takes output (string) which is the result directory.
// Takes originalContent ([]byte) which is the input as read.
// Takes result (summary) which describes the reduction.
// Takes stdout (io.Writer) which receives the summary.
//
// Returns error when a file cannot be written.
func writeResults(output string, originalContent []byte, result summary, stdout io.Writer) error {
	encoded, err := fuzztext.MarshalASCII(result, true)
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"original":     originalContent,
		"reduced":      result.reducedContent,
		"summary.json": append(encoded, '\n'),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(output, name), content, outputMode); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	_, err = fmt.Fprintf(stdout, "%s\n", encoded)
	return err
}

// signalStatus returns the shell status of a signal.
//
// Takes signal (os.Signal) which is the signal received.
//
// Returns int which is 128 plus the signal number.
func signalStatus(signal os.Signal) int {
	if number, isNumber := signal.(syscall.Signal); isNumber {
		return signalStatusBase + int(number)
	}
	return StatusFailed
}

// hashBytes returns the hexadecimal SHA-256 of bytes.
//
// Takes data ([]byte) which is the content to hash.
//
// Returns string which is the hash.
func hashBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// roundedSeconds rounds a duration to a fraction of a second.
//
// Takes duration (time.Duration) which is the duration.
// Takes fractions (float64) which is the number of steps per second.
//
// Returns jsonFloat which is the rounded seconds.
func roundedSeconds(duration time.Duration, fractions float64) jsonFloat {
	return jsonFloat(math.Round(duration.Seconds()*fractions) / fractions)
}

// gitRevision returns the checked-out revision, or unknown outside a repository.
//
// Takes ctx (context.Context) which bounds the call.
// Takes directory (string) which is inside the repository.
//
// Returns string which is the revision.
func gitRevision(ctx context.Context, directory string) string {
	command := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	command.Dir = directory
	revision, err := command.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(revision))
}

// goVersion returns the go version output, empty when it cannot run.
//
// Takes ctx (context.Context) which bounds the call.
// Takes goCommand (string) which is the Go command.
//
// Returns string which is the version line.
func goVersion(ctx context.Context, goCommand string) string {
	version, _ := exec.CommandContext(ctx, goCommand, "version").Output()
	return strings.TrimSpace(string(version))
}
