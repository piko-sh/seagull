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
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"piko.sh/safedisk"

	"seagull.sh/seagull/sdk/emit"
)

const (
	// changeAdded marks a generated file that does not exist yet.
	changeAdded = "add"

	// changeModified marks a generated file whose content changes.
	changeModified = "modify"

	// changeRemoved marks a stale generated file that would be pruned.
	changeRemoved = "remove"

	// diffContextLines is the number of unchanged lines around each diff hunk.
	diffContextLines = 3
)

// fileChange is one difference between the generated files and the output directory.
type fileChange struct {
	// path is the file's absolute path.
	path string

	// kind is add, modify or remove.
	kind string

	// before is the file's current content, empty when it is added or when only whether it
	// changed was asked for.
	before []byte

	// after is the file's new content, empty when it is removed.
	after []byte
}

// planChanges compares generated files with an output directory: which would be added,
// which modified, and which stale generated files would be pruned.
//
// The directory is read through a read-only sandbox. An existing file is read only as far
// as needed to tell whether it changed, one byte past the new content, unless keepContent
// asks for the whole of each changed file, as a diff needs, up to the new content's
// length plus maxSourceFileBytes.
//
// Takes directory (string) which is the output directory; it need not exist.
// Takes files ([]emit.GeneratedFile) which are the generated files.
// Takes headers ([]string) which mark a file as generated: the current header and any
// legacy ones.
// Takes keepContent (bool) which keeps the current content of each changed file.
//
// Returns []fileChange which holds the differences, sorted by path.
// Returns error when the directory or a file cannot be read.
func planChanges(directory string, files []emit.GeneratedFile, headers []string, keepContent bool) ([]fileChange, error) {
	sandbox, err := safedisk.NewSandbox(directory, safedisk.ModeReadOnly)
	if errors.Is(err, fs.ErrNotExist) {
		return addedFiles(directory, files), nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", directory, err)
	}
	changes, err := planInSandbox(sandbox, directory, files, headers, keepContent)
	return changes, errors.Join(err, sandbox.Close())
}

// addedFiles lists every generated file as added, for an output directory that does not
// exist yet.
//
// Takes directory (string) which is the output directory.
// Takes files ([]emit.GeneratedFile) which are the generated files.
//
// Returns []fileChange which holds one addition per non-empty file, sorted by path.
func addedFiles(directory string, files []emit.GeneratedFile) []fileChange {
	var changes []fileChange
	for _, file := range files {
		if len(file.Content) > 0 {
			changes = append(changes, fileChange{path: filepath.Join(directory, file.Name), kind: changeAdded, before: nil, after: file.Content})
		}
	}
	slices.SortFunc(changes, func(left, right fileChange) int { return strings.Compare(left.path, right.path) })
	return changes
}

// planInSandbox compares generated files with the output directory a sandbox holds.
//
// Takes sandbox (safedisk.Sandbox) which is rooted at the output directory.
// Takes directory (string) which is the output directory, for the paths of changes.
// Takes files ([]emit.GeneratedFile) which are the generated files.
// Takes headers ([]string) which mark a file as generated.
// Takes keepContent (bool) which keeps the current content of each changed file.
//
// Returns []fileChange which holds the differences, sorted by path.
// Returns error when the directory or a file cannot be read.
func planInSandbox(sandbox safedisk.Sandbox, directory string, files []emit.GeneratedFile, headers []string, keepContent bool) ([]fileChange, error) {
	var changes []fileChange
	generated := map[string]bool{}
	for _, file := range files {
		if len(file.Content) == 0 {
			continue
		}
		generated[file.Name] = true
		change, changed, err := compareFile(sandbox, directory, file, keepContent)
		if err != nil {
			return nil, err
		}
		if changed {
			changes = append(changes, change)
		}
	}
	stale, err := staleFiles(sandbox, directory, generated, headers, keepContent)
	if err != nil {
		return nil, err
	}
	changes = append(changes, stale...)
	slices.SortFunc(changes, func(left, right fileChange) int { return strings.Compare(left.path, right.path) })
	return changes, nil
}

// compareFile compares one generated file with the file of the same name in the output
// directory.
//
// Takes sandbox (safedisk.Sandbox) which is rooted at the output directory.
// Takes directory (string) which is the output directory, for the change's path.
// Takes file (emit.GeneratedFile) which is the generated file.
// Takes keepContent (bool) which keeps the current content when it changed.
//
// Returns fileChange which describes the change.
// Returns bool which is false when the file is unchanged.
// Returns error when the existing file cannot be read.
func compareFile(sandbox safedisk.Sandbox, directory string, file emit.GeneratedFile, keepContent bool) (fileChange, bool, error) {
	path := filepath.Join(directory, file.Name)
	change := fileChange{path: path, kind: changeModified, before: nil, after: file.Content}
	limit := int64(len(file.Content)) + 1
	if keepContent {
		limit = int64(len(file.Content)) + maxSourceFileBytes
	}
	existing, err := readFileIn(sandbox, file.Name, path, limit)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		change.kind = changeAdded
		return change, true, nil
	case errors.Is(err, errFileTooLarge) && !keepContent:
		return change, true, nil
	case err != nil:
		return change, false, err
	case bytes.Equal(existing, file.Content):
		return change, false, nil
	default:
		change.before = existing
		return change, true, nil
	}
}

// staleFiles finds generated files in the output directory that the run no longer
// produces. Only regular files are considered, and only as many of their first bytes as
// the longest header needs are read; a stale file's whole content, up to
// maxSourceFileBytes, is read only when keepContent asks for it.
//
// Takes sandbox (safedisk.Sandbox) which is rooted at the output directory.
// Takes directory (string) which is the output directory, for the paths of changes.
// Takes generated (map[string]bool) which holds the names the run produces.
// Takes headers ([]string) which mark a file as generated.
// Takes keepContent (bool) which keeps the content of each stale file.
//
// Returns []fileChange which holds one removal per stale file.
// Returns error when the directory cannot be listed or a file cannot be read.
func staleFiles(sandbox safedisk.Sandbox, directory string, generated map[string]bool, headers []string, keepContent bool) ([]fileChange, error) {
	entries, err := listSandbox(sandbox, directory)
	if err != nil {
		return nil, err
	}
	headerBytes := int64(0)
	for _, header := range headers {
		headerBytes = max(headerBytes, int64(len(header)+len("\r\n")))
	}
	var changes []fileChange
	for _, entry := range entries {
		if !entry.Type().IsRegular() || generated[entry.Name()] {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		prefix, err := readPrefix(sandbox, entry.Name(), headerBytes)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		if !hasGeneratedHeader(prefix, headers) {
			continue
		}
		var before []byte
		if keepContent {
			if before, err = readFileIn(sandbox, entry.Name(), path, maxSourceFileBytes); err != nil {
				return nil, err
			}
		}
		changes = append(changes, fileChange{path: path, kind: changeRemoved, before: before, after: nil})
	}
	return changes, nil
}

// hasGeneratedHeader reports whether content starts with one of the headers on a line of
// its own, ended by a line feed or by a carriage return and line feed, as the library
// recognises a generated file it prunes.
//
// Takes content ([]byte) which is the start of a file.
// Takes headers ([]string) which are the candidate first lines.
//
// Returns bool which is true when the first line is one of them.
func hasGeneratedHeader(content []byte, headers []string) bool {
	for _, header := range headers {
		if header == "" {
			continue
		}
		rest, found := bytes.CutPrefix(content, []byte(header))
		if found && (bytes.HasPrefix(rest, []byte("\n")) || bytes.HasPrefix(rest, []byte("\r\n"))) {
			return true
		}
	}
	return false
}

// writeDiff writes a unified diff of each change.
//
// Takes writer (io.Writer) which receives the diff.
// Takes changes ([]fileChange) which are diffed.
// Takes display (func(string) string) which shortens a path for printing.
//
// Returns error when the diff cannot be built or written.
func writeDiff(writer io.Writer, changes []fileChange, display func(string) string) error {
	for _, change := range changes {
		name := display(change.path)
		fromFile, toFile := "a/"+name, "b/"+name
		switch change.kind {
		case changeAdded:
			fromFile = "/dev/null"
		case changeRemoved:
			toFile = "/dev/null"
		default:
		}
		text, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        diffLines(change.before),
			B:        diffLines(change.after),
			FromFile: fromFile,
			FromDate: "",
			ToFile:   toFile,
			ToDate:   "",
			Eol:      "",
			Context:  diffContextLines,
		})
		if err != nil {
			return fmt.Errorf("diffing %s: %w", name, err)
		}
		if _, err := io.WriteString(writer, text); err != nil {
			return err
		}
	}
	return nil
}

// diffLines splits content into the lines a unified diff compares, each with its line
// ending, so an absent or empty file has no lines at all and its hunk reads -0,0 or +0,0.
// A last line without a line ending is given one.
//
// Takes content ([]byte) which is the file's content.
//
// Returns []string which holds the lines.
func diffLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	lines := strings.SplitAfter(string(content), "\n")
	if lines[len(lines)-1] == "" {
		return lines[:len(lines)-1]
	}
	lines[len(lines)-1] += "\n"
	return lines
}
