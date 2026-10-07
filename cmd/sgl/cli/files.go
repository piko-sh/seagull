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
	"errors"
	"fmt"
	"io"
	"io/fs"

	"piko.sh/safedisk"
)

const (
	// maxSourceFileBytes is the largest SQL file sgl reads, the cap the library puts on a
	// migration, seed or query file by default.
	maxSourceFileBytes = 4 << 20

	// maxDirectoryEntries is the most entries sgl lists in one directory, such as a go.out
	// or migration directory.
	maxDirectoryEntries = 10_000
)

// readFileIn reads a regular file of at most limit bytes from a sandbox.
//
// Takes sandbox (safedisk.Sandbox) which holds the file.
// Takes name (string) which is the file's name within the sandbox.
// Takes display (string) which names the file in an error.
// Takes limit (int64) which is the largest size read.
//
// Returns []byte which is the content.
// Returns error which names the file when it cannot be read, is not a regular file, or is
// larger than limit, wrapping errFileTooLarge.
func readFileIn(sandbox safedisk.Sandbox, name, display string, limit int64) ([]byte, error) {
	info, err := sandbox.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", display, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("reading %s: %w", display, errNotRegularFile)
	}
	data, _, err := sandbox.ReadFileLimit(name, limit)
	if errors.Is(err, safedisk.ErrFileExceedsLimit) {
		return nil, fmt.Errorf("reading %s: %w: it is larger than %d bytes", display, errFileTooLarge, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", display, err)
	}
	return data, nil
}

// readPrefix reads at most limit bytes from the start of a file in a sandbox.
//
// Takes sandbox (safedisk.Sandbox) which holds the file.
// Takes name (string) which is the file's name within the sandbox.
// Takes limit (int64) which is the most bytes read.
//
// Returns []byte which holds the first bytes, fewer when the file is shorter.
// Returns error when the file cannot be opened or read.
func readPrefix(sandbox safedisk.Sandbox, name string, limit int64) ([]byte, error) {
	file, err := sandbox.Open(name)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, limit))
	return data, errors.Join(err, file.Close())
}

// listDirectory lists a directory's entries, refusing one that holds more than
// maxDirectoryEntries.
//
// Takes directory (string) which is listed.
//
// Returns []fs.DirEntry which holds the entries, in directory order.
// Returns error which wraps fs.ErrNotExist when the directory does not exist, or names
// the directory when it cannot be listed or holds too many entries.
func listDirectory(directory string) ([]fs.DirEntry, error) {
	sandbox, err := safedisk.NewSandbox(directory, safedisk.ModeReadOnly)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", directory, err)
	}
	entries, err := listSandbox(sandbox, directory)
	return entries, errors.Join(err, sandbox.Close())
}

// listSandbox lists the entries at a sandbox's root, refusing more than
// maxDirectoryEntries.
//
// Takes sandbox (safedisk.Sandbox) which is listed.
// Takes display (string) which names the directory in an error.
//
// Returns []fs.DirEntry which holds the entries, in directory order.
// Returns error when the directory cannot be listed or holds too many entries.
func listSandbox(sandbox safedisk.Sandbox, display string) ([]fs.DirEntry, error) {
	root, err := sandbox.Open(".")
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", display, err)
	}
	entries, err := root.ReadDir(maxDirectoryEntries + 1)
	closeErr := root.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.Join(fmt.Errorf("listing %s: %w", display, err), closeErr)
	}
	if len(entries) > maxDirectoryEntries {
		return nil, errors.Join(fmt.Errorf("listing %s: it holds more than %d entries: %w", display, maxDirectoryEntries, errTooManyEntries), closeErr)
	}
	return entries, closeErr
}
