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

package output

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"piko.sh/safedisk"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// lineFeed ends a header line written with Unix line endings.
	lineFeed = "\n"

	// carriageReturnLineFeed ends a header line written with Windows line endings, as a
	// checkout that converts line endings leaves a generated file.
	carriageReturnLineFeed = "\r\n"
)

// generatedOutputDirectory is the minimal filesystem surface pruneStaleGeneratedFiles
// needs to remove obsolete generated files, satisfied by a safedisk sandbox rooted at the
// output directory.
type generatedOutputDirectory interface {
	// ReadDir lists the entries of the directory named name.
	//
	// Takes name (string) which is the directory path to list.
	//
	// Returns []fs.DirEntry which holds the directory entries.
	// Returns error when the directory cannot be read.
	ReadDir(name string) ([]fs.DirEntry, error)

	// Open opens the file named name for reading.
	//
	// Takes name (string) which is the path of the file to open.
	//
	// Returns safedisk.FileHandle which is the open file, and which the caller must close.
	// Returns error when the file cannot be opened.
	Open(name string) (safedisk.FileHandle, error)

	// Remove deletes the file named name.
	//
	// Takes name (string) which is the path of the file to delete.
	//
	// Returns error when the file cannot be deleted.
	Remove(name string) error
}

// generatedFileNameSet returns the names of the files a generation run will actually
// write.
//
// Only files carrying content are included; it is the keep set to pass to
// pruneStaleGeneratedFiles.
//
// Takes files ([]sqlmodel.GeneratedFile) which are the freshly generated files.
//
// Returns map[string]struct{} which is the set of names that must survive a prune.
func generatedFileNameSet(files []sqlmodel.GeneratedFile) map[string]struct{} {
	names := make(map[string]struct{}, len(files))
	for _, file := range files {
		if len(file.Content) == 0 {
			continue
		}
		names[file.Name] = struct{}{}
	}
	return names
}

// pruneStaleGeneratedFiles removes files left over from a previous generation run.
//
// A renamed or deleted query no longer collides with the fresh output. Only regular files
// whose first line is one of headers, ended by a line feed or a carriage return and line
// feed, and that are absent from keep are removed, so hand-written files sharing the
// directory are never touched. Directories, symbolic links, named pipes, sockets and
// devices are passed over unopened, because a generated file is always a regular file and
// opening a named pipe would wait for a writer. A file that has vanished since the
// directory listing is treated as already pruned.
//
// Takes directory (generatedOutputDirectory) which is the output directory sandbox.
// Takes keep (map[string]struct{}) which is the set of filenames about to be written.
// Takes headers (...string) which are the generated-file header lines that mark a file as
// generated: the current header and any it replaced.
//
// Returns error when the directory cannot be listed or a stale file cannot be inspected
// or removed for an unexpected reason.
func pruneStaleGeneratedFiles(directory generatedOutputDirectory, keep map[string]struct{}, headers ...string) error {
	entries, err := directory.ReadDir(".")
	if err != nil {
		return fmt.Errorf("listing output directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !entry.Type().IsRegular() {
			continue
		}

		name := entry.Name()
		if _, wanted := keep[name]; wanted {
			continue
		}

		generated, inspectErr := fileHasAnyGeneratedHeader(directory, name, headers)
		if inspectErr != nil {
			return fmt.Errorf("inspecting candidate stale file %q: %w", name, inspectErr)
		}
		if !generated {
			continue
		}

		if removeErr := directory.Remove(name); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return fmt.Errorf("removing stale generated file %q: %w", name, removeErr)
		}
	}

	return nil
}

// fileHasAnyGeneratedHeader reports whether name's first line is one of headers, ended by
// a line feed or by a carriage return and line feed.
//
// Only a prefix as long as the longest header and its line ending is read, so an
// unrelated large sibling is never loaded whole, and the file is opened once whatever the
// number of headers.
//
// Takes directory (generatedOutputDirectory) which is the output directory sandbox.
// Takes name (string) which is the candidate file name.
// Takes headers ([]string) which are the generated-file header lines to match; an empty
// header matches nothing.
//
// Returns bool which is true when the file starts with one of the headers.
// Returns error when an unexpected open or read failure occurs.
func fileHasAnyGeneratedHeader(directory generatedOutputDirectory, name string, headers []string) (bool, error) {
	longest := 0
	for _, header := range headers {
		longest = max(longest, len(header))
	}
	if longest == 0 {
		return false, nil
	}
	prefix, err := readPrefix(directory, name, longest+len(carriageReturnLineFeed))
	if err != nil {
		return false, err
	}
	for _, header := range headers {
		if header != "" && startsWithHeaderLine(prefix, header) {
			return true, nil
		}
	}
	return false, nil
}

// readPrefix reads at most size bytes from the start of a file.
//
// Takes directory (generatedOutputDirectory) which is the output directory sandbox.
// Takes name (string) which is the file name.
// Takes size (int) which is the most bytes read.
//
// Returns []byte which holds the bytes read, fewer than size for a shorter file, or none
// for a file already removed.
// Returns error when an unexpected open or read failure occurs.
func readPrefix(directory generatedOutputDirectory, name string, size int) ([]byte, error) {
	handle, openErr := directory.Open(name)
	if openErr != nil {
		if errors.Is(openErr, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, openErr
	}
	defer func() { _ = handle.Close() }()

	prefix := make([]byte, size)
	read, readErr := io.ReadFull(handle, prefix)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return nil, readErr
	}
	return prefix[:read], nil
}

// startsWithHeaderLine reports whether text starts with a header followed by a line feed,
// or by a carriage return and line feed.
//
// Takes text ([]byte) which is the start of a file.
// Takes header (string) which is the header line.
//
// Returns bool which is true when the file's first line is the header.
func startsWithHeaderLine(text []byte, header string) bool {
	rest, found := bytes.CutPrefix(text, []byte(header))
	if !found {
		return false
	}
	return bytes.HasPrefix(rest, []byte(lineFeed)) || bytes.HasPrefix(rest, []byte(carriageReturnLineFeed))
}
