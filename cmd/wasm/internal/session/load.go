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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	"seagull.sh/seagull/cmd/internal/config"
)

const (
	// sqlExtension ends the name of every migration and query file.
	sqlExtension = ".sql"

	// maxDirectoryEntries is the most entries LoadRequest lists in one directory.
	maxDirectoryEntries = 1024
)

// loader reads a project's files into a request, holding them to the limits Run applies:
// at most MaxFiles files of at most MaxFileBytes each and MaxTotalBytes in all.
type loader struct {
	// filesystem is rooted at the project.
	filesystem fs.FS

	// files maps each file read so far to its content.
	files map[string]string

	// total is the size of the files read so far.
	total int
}

// readSQLFiles adds the SQL files directly inside a directory. A missing directory adds
// nothing.
//
// Takes directory (string) which is a slash-separated directory within the project.
//
// Returns error when the directory or a file cannot be read, or the project holds more
// than a request may.
func (l *loader) readSQLFiles(directory string) error {
	entries, err := l.list(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), sqlExtension) {
			continue
		}
		if _, _, err := l.read(path.Join(directory, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// list lists at most maxDirectoryEntries entries of a directory, sorted by name.
//
// Takes directory (string) which is a slash-separated directory within the project.
//
// Returns []fs.DirEntry which holds the entries, or none when the directory is missing.
// Returns error when the directory cannot be read or holds more entries than the limit.
func (l *loader) list(directory string) (entries []fs.DirEntry, err error) {
	opened, err := l.filesystem.Open(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, opened.Close()) }()
	listing, isDirectory := opened.(fs.ReadDirFile)
	if !isDirectory {
		return nil, fmt.Errorf("%s is not a directory that can be listed", directory)
	}
	entries, err = listing.ReadDir(maxDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > maxDirectoryEntries {
		return nil, fmt.Errorf("%s holds %w: at most %d entries", directory, errOverLimit, maxDirectoryEntries)
	}
	slices.SortFunc(entries, func(left, right fs.DirEntry) int { return strings.Compare(left.Name(), right.Name()) })
	return entries, nil
}

// read adds one regular file to the request, reading at most one byte more than a file
// may hold. A file already read is not read again.
//
// Takes name (string) which is the file's slash-separated path within the project.
//
// Returns content (string) which is the file's content.
// Returns found (bool) which is false, with no error, when the file is missing or is not
// a regular file.
// Returns err (error) when the file cannot be read, or holds more than a request may.
func (l *loader) read(name string) (content string, found bool, err error) {
	if content, seen := l.files[name]; seen {
		return content, true, nil
	}
	info, err := fs.Stat(l.filesystem, name)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, nil
	}
	if len(l.files) >= MaxFiles {
		return "", false, fmt.Errorf("the project holds %w: at most %d files", errOverLimit, MaxFiles)
	}
	file, err := l.filesystem.Open(name)
	if err != nil {
		return "", false, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return "", false, fmt.Errorf("reading %s: %w", name, err)
	}
	if len(data) > MaxFileBytes {
		return "", false, fmt.Errorf("%s is %w: files of at most %d bytes", name, errOverLimit, MaxFileBytes)
	}
	l.total += len(data)
	if l.total > MaxTotalBytes {
		return "", false, fmt.Errorf("the project holds %w: at most %d bytes in all", errOverLimit, MaxTotalBytes)
	}
	l.files[name] = string(data)
	return l.files[name], true, nil
}

// LoadRequest builds a request from a project directory: its configuration file, if it
// has one, and the SQL files in the first database's migration and query directories.
//
// Other files, such as seeds and generated code, are left out, as is anything that is not
// a regular file. Each file is read only as far as the playground's limits allow.
//
// Takes filesystem (fs.FS) which is rooted at the project, such as os.DirFS(dir).
//
// Returns Request which holds the files, with no overrides.
// Returns error when the configuration cannot be parsed, a file or directory cannot be
// read, or the project holds more than a request may.
func LoadRequest(filesystem fs.FS) (Request, error) {
	reader := &loader{filesystem: filesystem, files: map[string]string{}, total: 0}
	migrations, queries := config.DefaultMigrationsDirectory, config.DefaultQueriesDirectory
	for _, name := range config.FileNames() {
		content, found, err := reader.read(name)
		if err != nil {
			return Request{}, err
		}
		if !found {
			continue
		}
		loaded, err := config.Parse([]byte(content), path.Ext(name) == ".json")
		if err != nil {
			return Request{}, fmt.Errorf("%s: %w", name, err)
		}
		migrations, queries = loaded.Databases[0].Migrations, loaded.Databases[0].Queries
		break
	}
	for _, directory := range []string{migrations, queries} {
		if err := reader.readSQLFiles(path.Clean(directory)); err != nil {
			return Request{}, err
		}
	}
	return Request{Files: reader.files, Override: Override{}, V: ProtocolVersion}, nil
}
