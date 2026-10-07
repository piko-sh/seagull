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

package session

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"testing/fstest"
	"time"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/observe"
)

const (
	// ProtocolVersion is the version of the Request and Response shapes. A request may leave
	// V at zero to mean this version.
	ProtocolVersion = 1

	// MaxFiles is the most files one request may hold.
	MaxFiles = 64

	// MaxFileBytes is the largest file one request may hold.
	MaxFileBytes = 256 << 10

	// MaxTotalBytes is the largest total size of the files of one request.
	MaxTotalBytes = 1 << 20

	// maxQueries caps the queries one run analyses.
	maxQueries = 1000

	// fileMode is the mode of the files in the in-memory file system.
	fileMode = 0o644

	// directoryMode is the mode of the directories added to the in-memory file system.
	directoryMode = fs.ModeDir | 0o755

	// runOperation names a playground run in the record of a panic it recovered.
	runOperation = "playground run"
)

// Request is one playground run: the project's files and any settings the playground
// applies on top of its sgl.yaml.
type Request struct {
	// Files maps slash-separated paths relative to the project root, such as
	// "db/queries/tasks.sql", to file contents.
	Files map[string]string `json:"files"`

	// Override holds settings applied on top of sgl.yaml.
	Override Override `json:"override"`

	// V is the protocol version; zero means ProtocolVersion.
	V int `json:"v"`
}

// Override holds playground settings applied over sgl.yaml, with zero fields keeping
// configured values. Options unsupported by the selected dialect are omitted with a
// warning.
type Override struct {
	// Prepared, when set, replaces go.prepared.
	Prepared *bool `json:"prepared,omitempty"`

	// SkipFailingQueries, when set to true, generates the queries without an error of their
	// own and leaves out those with one, as sgl generate does, rather than generating
	// nothing while any query has an error.
	SkipFailingQueries *bool `json:"skipFailingQueries,omitempty"`

	// GroupByUseNulls, when set, replaces dialect_options.group_by_use_nulls (ClickHouse).
	GroupByUseNulls *bool `json:"groupByUseNulls,omitempty"`

	// ANSIQuotes, when set, replaces dialect_options.ansi_quotes (MySQL and MariaDB).
	ANSIQuotes *bool `json:"ansiQuotes,omitempty"`

	// NonNullElements, when set, replaces go.types.non_null_elements.
	NonNullElements *bool `json:"nonNullElements,omitempty"`

	// KeepStars, when set, replaces go.keep_stars.
	KeepStars *bool `json:"keepStars,omitempty"`

	// Interface, when set, replaces go.interface.enabled.
	Interface *bool `json:"interface,omitempty"`

	// JSONOmitEmpty, when set, replaces go.json_tags.omitempty.
	JSONOmitEmpty *bool `json:"jsonOmitEmpty,omitempty"`

	// JSONOmit, when set, replaces go.json_tags.omit.
	JSONOmit *bool `json:"jsonOmit,omitempty"`

	// EmptySlices, when set, replaces go.empty_slices.
	EmptySlices *bool `json:"emptySlices,omitempty"`

	// DocComments, when set, replaces go.doc_comments.
	DocComments *bool `json:"docComments,omitempty"`

	// ReuseModels, when set, replaces go.reuse_models.
	ReuseModels *bool `json:"reuseModels,omitempty"`

	// WrapErrors, when set, replaces go.wrap_errors.
	WrapErrors *bool `json:"wrapErrors,omitempty"`

	// StructTags, when not nil, replaces go.struct_tags.
	StructTags map[string]string `json:"structTags,omitempty"`

	// PackageNames, when not nil, replaces go.types.package_names.
	PackageNames map[string]string `json:"packageNames,omitempty"`

	// Dialect, when set, replaces the database's dialect.
	Dialect string `json:"dialect,omitempty"`

	// Driver, when set, replaces go.driver.
	Driver string `json:"driver,omitempty"`

	// InterfaceName, when set, replaces go.interface.name.
	InterfaceName string `json:"interfaceName,omitempty"`

	// JSONCase, when set, replaces go.json_tags.case: snake, camel or pascal.
	JSONCase string `json:"jsonCase,omitempty"`

	// SearchPath, when not nil, replaces dialect_options.search_path (PostgreSQL,
	// CockroachDB and TimescaleDB).
	SearchPath []string `json:"searchPath,omitempty"`
}

// passes holds the outcome of the three passes of one run.
type passes struct {
	// result is the outcome of Generate.
	result *seagull.Result

	// report is the outcome of Vet.
	report *seagull.Report

	// catalogue is the schema catalogue.
	catalogue *dialect.Catalogue
}

// Run generates, vets and catalogues a project held in memory.
//
// A panic anywhere in the run becomes a response with OK false and Internal set. Its
// stack is logged once at warning level through ctx's logger, or slog.Default() if none
// is set. The response never includes the stack.
//
// Takes request (Request) which holds the project's files and overrides.
//
// Returns Response which is ready to encode as JSON. OK is true when the run completed,
// even when diagnostics stopped code generation; it is false, with Error set, when the
// request or its configuration cannot be used or the run fails.
func Run(ctx context.Context, request Request) Response {
	return runWithClock(ctx, request, observe.SystemClock())
}

// runWithClock is Run with the clock that times the run and each of its stages.
//
// Takes request (Request) which holds the project's files and overrides.
// Takes clock (observe.Clock) which reads the time.
//
// Returns Response which describes the run.
func runWithClock(ctx context.Context, request Request, clock observe.Clock) Response {
	return guarded(ctx, clock, func() Response {
		return run(ctx, request, clock)
	})
}

// guarded runs work, setting the response's duration from clock and turning a panic in
// work into an internal failure. The panic's stack is logged once through the logger ctx
// carries; the response says only what panicked.
//
// Takes clock (observe.Clock) which times the work.
// Takes work (func() Response) which is the run.
//
// Returns Response which is work's response, or an internal failure when it panicked,
// with DurationMS set either way.
func guarded(ctx context.Context, clock observe.Clock, work func() Response) (response Response) {
	started := clock.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			logging.LogRecoveredPanic(ctx, runOperation, recovered)
			response = failure(fmt.Errorf("%w: %v", errInternal, recovered), nil)
			response.Internal = true
		}
		response.DurationMS = milliseconds(clock.Now().Sub(started))
	}()
	return work()
}

// run is Run without the panic guard.
//
// Takes request (Request) which holds the project's files and overrides.
// Takes clock (observe.Clock) which times each stage.
//
// Returns Response which describes the run.
func run(ctx context.Context, request Request, clock observe.Clock) Response {
	if request.V != 0 && request.V != ProtocolVersion {
		return failure(fmt.Errorf("protocol version %d is not supported; this playground speaks version %d", request.V, ProtocolVersion), nil)
	}
	files, err := checkFiles(request.Files)
	if err != nil {
		return failure(err, nil)
	}
	resolved, err := resolveProject(files, request.Override)
	if err != nil {
		return failure(err, nil)
	}
	timer := assemble.NewStageTimer(clock)
	built, err := newGenerator(resolved, memoryFS(files, resolved), timer)
	if err != nil {
		return failure(err, resolved)
	}
	outcome, err := execute(ctx, built, timer)
	if err != nil {
		return failure(err, resolved)
	}
	return respond(files, resolved, outcome, stageViews(timer.Stages()))
}

// execute runs one generation with its report and catalogue, recording actual stages.
//
// Takes built (*generator) which holds the configured generator and its input.
// Takes timer (*assemble.StageTimer) which collects timings and is stopped after
// generation.
//
// Returns *passes which holds the generation result, report and catalogue.
// Returns error when generation cannot complete.
func execute(ctx context.Context, built *generator, timer *assemble.StageTimer) (*passes, error) {
	details, err := built.generator.GenerateDetailed(ctx, built.input)
	timer.Stop()
	if err != nil {
		return nil, err
	}
	return &passes{result: details.Result, report: details.Report, catalogue: details.Catalogue}, nil
}

// checkFiles checks a request's files against the limits and normalises their paths.
//
// Takes files (map[string]string) which maps paths to contents.
//
// Returns map[string]string which maps each cleaned path to its content.
// Returns error when there are too many files, one is too large, the total is too large,
// or a path is empty, absolute, escapes the project or repeats once cleaned.
func checkFiles(files map[string]string) (map[string]string, error) {
	if len(files) > MaxFiles {
		return nil, fmt.Errorf("the project has %d files; the playground takes at most %d", len(files), MaxFiles)
	}
	cleaned := make(map[string]string, len(files))
	total := 0
	for _, name := range slices.Sorted(maps.Keys(files)) {
		content := files[name]
		clean, err := cleanFilePath(name)
		if err != nil {
			return nil, err
		}
		if _, repeated := cleaned[clean]; repeated {
			return nil, fmt.Errorf("file %q is given twice", clean)
		}
		if len(content) > MaxFileBytes {
			return nil, fmt.Errorf("file %q is %d bytes; the playground takes files of at most %d bytes", clean, len(content), MaxFileBytes)
		}
		total += len(content)
		if total > MaxTotalBytes {
			return nil, fmt.Errorf("the project is larger than %d bytes", MaxTotalBytes)
		}
		cleaned[clean] = content
	}
	return cleaned, nil
}

// cleanFilePath normalises one request path.
//
// Takes name (string) which is the path as given.
//
// Returns string which is the cleaned, slash-separated path.
// Returns error when the path is empty, absolute or escapes the project.
func cleanFilePath(name string) (string, error) {
	if name == "" {
		return "", errors.New("a file has an empty path")
	}
	if strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, ":") {
		return "", fmt.Errorf("file %q must have a relative, slash-separated path", name)
	}
	if slices.Contains(strings.Split(name, "/"), "..") {
		return "", fmt.Errorf("file %q must not use .. in its path", name)
	}
	clean := path.Clean(name)
	if clean == "." || !fs.ValidPath(clean) {
		return "", fmt.Errorf("file %q does not name a file inside the project", name)
	}
	return clean, nil
}

// memoryFS builds the in-memory file system a run reads, adding the migration and query
// directories when no file lives in them, so an empty project still generates.
//
// Takes files (map[string]string) which maps cleaned paths to contents.
// Takes resolved (*project) which names the migration and query directories.
//
// Returns fstest.MapFS which holds the files.
func memoryFS(files map[string]string, resolved *project) fstest.MapFS {
	filesystem := make(fstest.MapFS, len(files)+2)
	for name, content := range files {
		filesystem[name] = &fstest.MapFile{Data: []byte(content), Mode: fileMode, ModTime: time.Time{}, Sys: nil}
	}
	for _, directory := range []string{resolved.migrations, resolved.queries} {
		if directory == "." || hasFileUnder(files, directory) {
			continue
		}
		filesystem[directory] = &fstest.MapFile{Data: nil, Mode: directoryMode, ModTime: time.Time{}, Sys: nil}
	}
	return filesystem
}

// hasFileUnder reports whether any file lives below a directory.
//
// Takes files (map[string]string) which maps cleaned paths to contents.
// Takes directory (string) which is a cleaned directory.
//
// Returns bool which is true when a path starts with the directory.
func hasFileUnder(files map[string]string, directory string) bool {
	prefix := directory + "/"
	for name := range files {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// failure builds the response of a run that could not complete. A panic Seagull recovered
// while generating is an internal error, as a panic in the playground is.
//
// Takes err (error) which describes the problem.
// Takes resolved (*project) which is the resolved configuration, or nil when the run
// failed before it was resolved.
//
// Returns Response which has OK false and Error set.
func failure(err error, resolved *project) Response {
	response := emptyResponse
	response.Error = err.Error()
	response.Internal = errors.Is(err, seagull.ErrPanic)
	if resolved != nil {
		response.Config = resolved.view()
	}
	return response
}
