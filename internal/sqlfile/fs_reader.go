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

package sqlfile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/fault"
)

const (
	// DefaultMaxFileBytes caps the size of a migration, seed or query file a reader loads
	// when no other cap is set, bounding the memory used by a single file read.
	DefaultMaxFileBytes int64 = 4 * 1024 * 1024

	// MaxFileBytesCeiling is the largest cap on the size of one file a reader accepts: 1
	// TiB. A reader given a larger cap uses this one.
	MaxFileBytesCeiling int64 = 1 << 40

	// DefaultMaxFiles caps the entries in a migration, seed or query directory a reader
	// lists when no other cap is set.
	DefaultMaxFiles = 10_000

	// DefaultMaxTotalBytes caps the bytes one generation reads across every migration and
	// query file when no other cap is set: 64 MiB, sixteen times the default cap on one
	// file.
	DefaultMaxTotalBytes int64 = 64 << 20

	// specialFileModes are the type bits of a directory entry that names no file a reader
	// can load: a named pipe, a socket, a device or another irregular file. Opening one can
	// block until another process writes to it.
	specialFileModes = fs.ModeNamedPipe | fs.ModeSocket | fs.ModeDevice | fs.ModeCharDevice | fs.ModeIrregular
)

// FSReader implements FileReader over an io/fs.FS, such as an embed.FS or os.DirFS,
// capping the size of each file and the number of entries in each directory.
type FSReader struct {
	// filesystem is the filesystem files are read from.
	filesystem fs.FS

	// budget, when set, caps the bytes this reader and the readers sharing the budget read
	// in total.
	budget *ByteBudget

	// maxFileBytes caps the size of a file ReadFile returns.
	maxFileBytes int64

	// maxEntries caps the number of entries ReadDir returns.
	maxEntries int
}

var _ FileReader = (*FSReader)(nil)

// NewFSReader creates a reader over a filesystem.
//
// Takes filesystem (fs.FS) which is the filesystem to read from.
// Takes maxFileBytes (int64) which caps the size of each file; a non-positive value
// selects DefaultMaxFileBytes, and a value above MaxFileBytesCeiling selects the ceiling.
// Takes maxEntries (int) which caps the entries in each directory; a non-positive value
// selects DefaultMaxFiles.
//
// Returns *FSReader which reads through the filesystem.
func NewFSReader(filesystem fs.FS, maxFileBytes int64, maxEntries int) *FSReader {
	if maxFileBytes <= 0 {
		maxFileBytes = DefaultMaxFileBytes
	}
	if maxEntries <= 0 {
		maxEntries = DefaultMaxFiles
	}
	return &FSReader{filesystem: filesystem, maxFileBytes: min(maxFileBytes, MaxFileBytesCeiling), budget: nil, maxEntries: maxEntries}
}

// WithBudget returns a copy of the reader that charges every file it reads to a budget,
// which other readers may share.
//
// Takes budget (*ByteBudget) which caps the bytes read in total.
//
// Returns *FSReader which reads as the reader does, within the budget.
func (reader *FSReader) WithBudget(budget *ByteBudget) *FSReader {
	budgeted := *reader
	budgeted.budget = budget
	return &budgeted
}

// ReadFile reads a file, refusing one larger than the size cap.
//
// The path is checked before it is opened, so a named pipe, a device or a directory is
// refused rather than read, and opening it never waits for another process.
//
// Takes filePath (string) which is the slash-separated path within the file system.
//
// Returns []byte which holds the file contents.
// Returns error when ctx is cancelled, the file cannot be read or is not a regular file,
// or it exceeds the cap, in which case the error wraps fault.ErrFileTooLarge, or takes
// the reader's budget past its total, in which case the error wraps fault.ErrLimit.
func (reader *FSReader) ReadFile(ctx context.Context, filePath string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	info, err := fs.Stat(reader.filesystem, filePath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("file %q: %w", filePath, errNotRegularFile)
	}
	if info.Size() > reader.maxFileBytes {
		return nil, fmt.Errorf("file %q is %d bytes (cap %d): %w", filePath, info.Size(), reader.maxFileBytes, fault.ErrFileTooLarge)
	}

	file, err := reader.filesystem.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, readLimit(reader.maxFileBytes)))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(content)) > reader.maxFileBytes {
		return nil, fmt.Errorf("file %q exceeds %d bytes: %w", filePath, reader.maxFileBytes, fault.ErrFileTooLarge)
	}
	if err := reader.budget.spend(filePath, int64(len(content))); err != nil {
		return nil, err
	}
	return content, nil
}

// ReadDir lists a directory's entries sorted by name, refusing a directory with more
// entries than the cap.
//
// Entries are read one more than the cap at a time, so a directory past the cap is
// refused without its every entry being held. Context cancellation is honoured before the
// read; the io/fs.FS interface has no cancellation hook, so an in-flight read runs to
// completion.
//
// Takes directory (string) which is the slash-separated directory path within the file
// system.
//
// Returns []os.DirEntry which holds the entries.
// Returns error when ctx is cancelled, the directory cannot be read, or it holds more
// entries than the cap, in which case the error wraps fault.ErrLimit.
func (reader *FSReader) ReadDir(ctx context.Context, directory string) ([]os.DirEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := readDirectory(reader.filesystem, directory, reader.maxEntries)
	if err != nil {
		return nil, err
	}
	if len(entries) > reader.maxEntries {
		return nil, fmt.Errorf("directory %q holds more than the limit of %d entries: %w",
			directory, reader.maxEntries, fault.ErrLimit)
	}
	slices.SortFunc(entries, func(left, right fs.DirEntry) int {
		return strings.Compare(left.Name(), right.Name())
	})
	return entries, nil
}

// readLimit returns how many bytes to read from a file capped at maxFileBytes: one more
// than the cap, so a file past it is detected, without overflowing at the largest value.
//
// Takes maxFileBytes (int64) which is the cap on the file's size.
//
// Returns int64 which is the number of bytes to read.
func readLimit(maxFileBytes int64) int64 {
	if maxFileBytes < math.MaxInt64 {
		return maxFileBytes + 1
	}
	return maxFileBytes
}

// readDirectory reads at most one more entry than a cap from a directory, unsorted.
//
// A directory that cannot be read a batch at a time is read whole through fs.ReadDir.
//
// Takes filesystem (fs.FS) which holds the directory.
// Takes directory (string) which is the slash-separated directory path.
// Takes maxEntries (int) which is the cap on the entries the caller accepts.
//
// Returns []fs.DirEntry which holds the entries read, more than maxEntries when the
// directory is past the cap.
// Returns error when the directory cannot be opened or read.
func readDirectory(filesystem fs.FS, directory string, maxEntries int) ([]fs.DirEntry, error) {
	file, err := filesystem.Open(directory)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	directoryFile, batched := file.(fs.ReadDirFile)
	if !batched {
		return fs.ReadDir(filesystem, directory)
	}
	count := maxEntries
	if count < math.MaxInt {
		count++
	}
	entries, err := directoryFile.ReadDir(count)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return entries, nil
}

// isSpecialFile reports whether a directory entry names a named pipe, a socket, a device
// or another irregular file, which a listing skips because no reader can load it.
//
// Takes entry (fs.DirEntry) which is the entry.
//
// Returns bool which is true for such an entry.
func isSpecialFile(entry fs.DirEntry) bool {
	return entry.Type()&specialFileModes != 0
}
