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

package provenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"seagull.sh/seagull/cmd/internal/fuzztool/fuzztext"
)

const (
	// statusPassed is the run status a resume may skip.
	statusPassed = "passed"

	// maxRecordBytes bounds a provenance record.
	maxRecordBytes = 1 << 20

	// maxCommandOutput bounds what git and go may print.
	maxCommandOutput = 64 << 20

	// reducePrefix names the reducer's temporary corpus entries.
	reducePrefix = "sgl-reduce-"

	// fileMode is the permission of a written record.
	fileMode = 0o600
)

var (
	// excludedPrefixes are repository paths that never affect a run.
	excludedPrefixes = []string{".cache/", "out/", "node_modules/"}

	// comparedFields are the record fields a resumed target must match.
	comparedFields = []string{"target", "command", "budget", "workers", "source", "modules", "toolchain", "corpus"}

	// environmentNames are the go env settings a toolchain record holds, in order.
	environmentNames = []string{"GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT", "CC"}

	// ErrMismatch reports a record a resume must not skip.
	ErrMismatch = errors.New("provenance does not match")

	// errOutputTooLarge refuses a command that printed past its bound.
	errOutputTooLarge = errors.New("command output exceeds its bound")
)

// Target names one fuzz target's run as the campaign would start it.
type Target struct {
	// Root is the repository root.
	Root string

	// Go is the Go command.
	Go string

	// Name is the registered target name.
	Name string

	// Module is the module directory relative to Root.
	Module string

	// Package is the package path relative to Module.
	Package string

	// Function is the fuzz or test function.
	Function string

	// Command is the campaign action.
	Command string

	// Budget is the time and round budget.
	Budget string

	// Workers is the worker count.
	Workers string
}

// Outcome is what a finished run adds to its description.
type Outcome struct {
	// Status is the run's status.
	Status string

	// SourceBefore is the source fingerprint taken before the run.
	SourceBefore string

	// Engine is the engine identity the run recorded.
	Engine string

	// BudgetCompleted is true when the run spent its whole budget.
	BudgetCompleted bool
}

// toolchainRecord describes the Go toolchain.
type toolchainRecord struct {
	// Version is the go version output.
	Version string `json:"version"`

	// Environment holds the build settings.
	Environment environmentRecord `json:"env"`

	// Compile is the hash of the compiler.
	Compile string `json:"compile"`

	// Link is the hash of the linker.
	Link string `json:"link"`
}

// environmentRecord holds the go env settings, in a fixed order.
type environmentRecord struct {
	// GOOS is the target operating system.
	GOOS string `json:"GOOS"`

	// GOARCH is the target architecture.
	GOARCH string `json:"GOARCH"`

	// CgoEnabled is the CGO_ENABLED setting.
	CgoEnabled string `json:"CGO_ENABLED"`

	// GOFLAGS is the default go flags.
	GOFLAGS string `json:"GOFLAGS"`

	// GOEXPERIMENT is the enabled experiments.
	GOEXPERIMENT string `json:"GOEXPERIMENT"`

	// CC is the C compiler.
	CC string `json:"CC"`
}

// modulesRecord holds the module file hashes.
type modulesRecord struct {
	// GoMod is the hash of go.mod.
	GoMod string `json:"go.mod"`

	// GoSum is the hash of go.sum.
	GoSum string `json:"go.sum"`
}

// record is a provenance file, the comparable description followed by the outcome.
type record struct {
	// Target is the registered target name.
	Target string `json:"target"`

	// Command is the campaign action.
	Command string `json:"command"`

	// Budget is the time and round budget.
	Budget string `json:"budget"`

	// Workers is the worker count.
	Workers string `json:"workers"`

	// Source is the source fingerprint.
	Source string `json:"source"`

	// Corpus is the seed corpus fingerprint.
	Corpus string `json:"corpus"`

	// Status is the run's status.
	Status string `json:"status"`

	// Engine is the engine identity the run recorded.
	Engine string `json:"engine"`

	// Modules holds the module file hashes.
	Modules modulesRecord `json:"modules"`

	// Toolchain describes the Go toolchain.
	Toolchain toolchainRecord `json:"toolchain"`

	// BudgetCompleted is true when the run spent its whole budget.
	BudgetCompleted bool `json:"budget_completed"`

	// SourceUnchanged is true when the source matched before and after the run.
	SourceUnchanged bool `json:"source_unchanged"`
}

// SourceFingerprint hashes the path and content of every tracked and untracked,
// unignored source file outside caches and reducer scratch inputs.
//
// Takes ctx (context.Context) which bounds the git call.
// Takes root (string) which is the repository root.
//
// Returns string which is the hexadecimal fingerprint.
// Returns error when git cannot list the files.
func SourceFingerprint(ctx context.Context, root string) (string, error) {
	listed, err := output(ctx, root, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	paths := bytes.Split(listed, []byte{0})
	slices.SortFunc(paths, bytes.Compare)
	digest := sha256.New()
	for _, path := range paths {
		if len(path) == 0 || excluded(string(path)) {
			continue
		}
		fmt.Fprintf(digest, "%s\x00%s\n", path, hashFile(filepath.Join(root, string(path))))
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// Record writes a run's provenance, replacing any earlier record whole.
//
// Takes ctx (context.Context) which bounds the git and go calls.
// Takes target (Target) which names the run.
// Takes outcome (Outcome) which is how the run ended.
// Takes file (string) which is the record to write.
//
// Returns error when the description cannot be read or the record written.
func Record(ctx context.Context, target Target, outcome Outcome, file string) error {
	current, err := describe(ctx, target)
	if err != nil {
		return err
	}
	current.Status = outcome.Status
	current.BudgetCompleted = outcome.BudgetCompleted
	current.SourceUnchanged = outcome.SourceBefore == current.Source
	current.Engine = outcome.Engine
	encoded, err := fuzztext.MarshalASCII(current, true)
	if err != nil {
		return err
	}
	temporary := file + ".tmp"
	if err := os.WriteFile(temporary, append(encoded, '\n'), fileMode); err != nil {
		return fmt.Errorf("writing the provenance record: %w", err)
	}
	if err := os.Rename(temporary, file); err != nil {
		return fmt.Errorf("writing the provenance record: %w", err)
	}
	return nil
}

// Matches checks that a recorded run passed whole and still describes the target.
//
// Takes ctx (context.Context) which bounds the git and go calls.
// Takes target (Target) which names the run as it would start now.
// Takes file (string) which is the earlier record.
//
// Returns error wrapping ErrMismatch when the target must run again, or another error
// when the current description cannot be read.
func Matches(ctx context.Context, target Target, file string) error {
	recorded, err := readRecord(file)
	if err != nil {
		return err
	}
	if recorded["status"] != statusPassed || !isTrue(recorded["budget_completed"]) || !isTrue(recorded["source_unchanged"]) {
		return fmt.Errorf("%w: the recorded run did not pass whole", ErrMismatch)
	}
	current, err := describe(ctx, target)
	if err != nil {
		return err
	}
	currentFields, err := generic(&current)
	if err != nil {
		return err
	}
	var differing []string
	for _, field := range comparedFields {
		if !reflect.DeepEqual(recorded[field], currentFields[field]) {
			differing = append(differing, field)
		}
	}
	if len(differing) > 0 {
		return fmt.Errorf("%w: provenance differs: %s", ErrMismatch, strings.Join(differing, ", "))
	}
	return nil
}

// SourceUnchanged reports whether a record says the source held still during its run.
//
// Takes file (string) which is the record.
//
// Returns error wrapping ErrMismatch when the source changed, or another error when
// the record cannot be read.
func SourceUnchanged(file string) error {
	recorded, err := readRecord(file)
	if err != nil {
		return err
	}
	if !isTrue(recorded["source_unchanged"]) {
		return fmt.Errorf("%w: the source changed during the run", ErrMismatch)
	}
	return nil
}

// describe builds the comparable part of a record for a target as it would run now.
//
// Takes ctx (context.Context) which bounds the git and go calls.
// Takes target (Target) which names the run.
//
// Returns record which holds the description.
// Returns error when the source or toolchain cannot be read.
func describe(ctx context.Context, target Target) (record, error) {
	source, err := SourceFingerprint(ctx, target.Root)
	if err != nil {
		return record{}, err
	}
	toolchain, err := describeToolchain(ctx, target.Go)
	if err != nil {
		return record{}, err
	}
	module := filepath.Join(target.Root, target.Module)
	corpus := filepath.Join(module, target.Package, "testdata", "fuzz", target.Function)
	return record{
		Target:    target.Name,
		Command:   target.Command,
		Budget:    target.Budget,
		Workers:   target.Workers,
		Source:    source,
		Corpus:    directoryFingerprint(corpus),
		Status:    "",
		Engine:    "",
		Modules:   modulesRecord{GoMod: hashFile(filepath.Join(module, "go.mod")), GoSum: hashFile(filepath.Join(module, "go.sum"))},
		Toolchain: toolchain,

		BudgetCompleted: false,
		SourceUnchanged: false,
	}, nil
}

// describeToolchain records the Go version, build settings and tool hashes.
//
// Takes ctx (context.Context) which bounds the go calls.
// Takes goCommand (string) which is the Go command.
//
// Returns toolchainRecord which describes the toolchain.
// Returns error when go cannot report its version or settings.
func describeToolchain(ctx context.Context, goCommand string) (toolchainRecord, error) {
	version, err := output(ctx, "", goCommand, "version")
	if err != nil {
		return toolchainRecord{}, err
	}
	settingsJSON, err := output(ctx, "", goCommand, slices.Concat([]string{"env", "-json", "GOROOT"}, environmentNames)...)
	if err != nil {
		return toolchainRecord{}, err
	}
	var settings map[string]string
	if err := json.Unmarshal(settingsJSON, &settings); err != nil {
		return toolchainRecord{}, fmt.Errorf("reading go env: %w", err)
	}
	tools := filepath.Join(settings["GOROOT"], "pkg", "tool", settings["GOOS"]+"_"+settings["GOARCH"])
	return toolchainRecord{
		Version: strings.TrimSpace(string(version)),
		Environment: environmentRecord{
			GOOS:         settings["GOOS"],
			GOARCH:       settings["GOARCH"],
			CgoEnabled:   settings["CGO_ENABLED"],
			GOFLAGS:      settings["GOFLAGS"],
			GOEXPERIMENT: settings["GOEXPERIMENT"],
			CC:           settings["CC"],
		},
		Compile: hashFile(filepath.Join(tools, "compile")),
		Link:    hashFile(filepath.Join(tools, "link")),
	}, nil
}

// directoryFingerprint hashes the names and contents of a directory's files, empty
// when the directory is missing.
//
// Takes directory (string) which is the directory to hash.
//
// Returns string which is the hexadecimal fingerprint, or empty.
func directoryFingerprint(directory string) string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return ""
	}
	digest := sha256.New()
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		fmt.Fprintf(digest, "%s\x00%s\n", entry.Name(), hashFile(path))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// hashFile returns the SHA-256 of a file's bytes, or empty when it cannot be read.
//
// Takes path (string) which is the file to hash.
//
// Returns string which is the hexadecimal hash, or empty.
func hashFile(path string) string {
	file, err := os.Open(path) //nolint:gosec // Hashes files the repository lists.
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return ""
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// excluded reports whether a listed path never affects a run.
//
// Takes path (string) which is a repository-relative path.
//
// Returns bool which is true for caches, reports and reducer scratch inputs.
func excluded(path string) bool {
	for _, prefix := range excludedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return strings.Contains(path, "/testdata/fuzz/") && strings.Contains(path, reducePrefix)
}

// readRecord reads a provenance record as generic JSON.
//
// Takes file (string) which is the record.
//
// Returns map[string]any which holds the record.
// Returns error which wraps ErrMismatch when the record is missing or unreadable.
func readRecord(file string) (map[string]any, error) {
	handle, err := os.Open(file) //nolint:gosec // The campaign names its own record.
	if err != nil {
		return nil, fmt.Errorf("%w: no readable record", ErrMismatch)
	}
	defer func() { _ = handle.Close() }()
	content, err := io.ReadAll(io.LimitReader(handle, maxRecordBytes+1))
	if err != nil || len(content) > maxRecordBytes {
		return nil, fmt.Errorf("%w: no readable record", ErrMismatch)
	}
	var recorded map[string]any
	if err := json.Unmarshal(content, &recorded); err != nil || recorded == nil {
		return nil, fmt.Errorf("%w: no readable record", ErrMismatch)
	}
	return recorded, nil
}

// generic converts a record to the generic JSON form readRecord returns.
//
// Takes current (*record) which is the description.
//
// Returns map[string]any which holds the record as decoded JSON.
// Returns error when the record cannot be converted.
func generic(current *record) (map[string]any, error) {
	encoded, err := json.Marshal(current)
	if err != nil {
		return nil, fmt.Errorf("converting the provenance record: %w", err)
	}
	var converted map[string]any
	if err := json.Unmarshal(encoded, &converted); err != nil {
		return nil, fmt.Errorf("converting the provenance record: %w", err)
	}
	return converted, nil
}

// output runs a command and returns its bounded standard output.
//
// Takes ctx (context.Context) which bounds the command.
// Takes directory (string) which is the working directory, empty for the current one.
// Takes name (string) and arguments (...string) which form the command line.
//
// Returns []byte which holds the output.
// Returns error when the command fails or prints past its bound.
func output(ctx context.Context, directory string, name string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...) //nolint:gosec // Runs git or the campaign's Go command.
	command.Dir = directory
	var stdout limitedBuffer
	command.Stdout = &stdout
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("running %s %s: %w", name, strings.Join(arguments, " "), err)
	}
	if stdout.overflowed {
		return nil, fmt.Errorf("running %s: %w", name, errOutputTooLarge)
	}
	return stdout.buffer.Bytes(), nil
}

// limitedBuffer keeps at most maxCommandOutput bytes and notes any overflow.
type limitedBuffer struct {
	// buffer holds the kept bytes.
	buffer bytes.Buffer

	// overflowed is true once a write passed the bound.
	overflowed bool
}

// Write keeps bytes up to the bound and discards the rest.
//
// Takes data ([]byte) which is the output to keep.
//
// Returns int which is always len(data), so the writer never fails the command.
// Returns error which is always nil.
func (limited *limitedBuffer) Write(data []byte) (int, error) {
	room := maxCommandOutput - limited.buffer.Len()
	if len(data) > room {
		limited.overflowed = true
		_, _ = limited.buffer.Write(data[:max(0, room)])
		return len(data), nil
	}
	_, _ = limited.buffer.Write(data)
	return len(data), nil
}

// isTrue reports whether a decoded JSON value is the boolean true.
//
// Takes value (any) which is the decoded value.
//
// Returns bool which is true only for a true boolean.
func isTrue(value any) bool {
	flag, isBool := value.(bool)
	return isBool && flag
}
