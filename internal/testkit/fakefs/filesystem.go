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

package fakefs

import (
	"context"
	"io/fs"
	"os"
	"time"
)

// FileReader serves file contents and directory listings from maps. A path listed in
// ReadFileErrors or ReadDirErrors fails with that error; any other unknown path fails
// with os.ErrNotExist.
type FileReader struct {
	// Files maps paths to file content.
	Files map[string][]byte

	// Directories maps directory paths to their entries.
	Directories map[string][]os.DirEntry

	// ReadFileErrors is returned by ReadFile when set.
	ReadFileErrors map[string]error

	// ReadDirErrors is returned by ReadDir when set.
	ReadDirErrors map[string]error
}

// ReadFile returns the content registered for path.
//
// Takes path (string) which is the file to read.
//
// Returns []byte which is the registered content.
// Returns error which is the injected error for path, or os.ErrNotExist.
func (reader *FileReader) ReadFile(_ context.Context, path string) ([]byte, error) {
	if reader.ReadFileErrors != nil {
		if err, ok := reader.ReadFileErrors[path]; ok {
			return nil, err
		}
	}
	if reader.Files != nil {
		if content, ok := reader.Files[path]; ok {
			return content, nil
		}
	}
	return nil, os.ErrNotExist
}

// ReadDir returns the entries registered for directory.
//
// Takes directory (string) which is the directory to list.
//
// Returns []os.DirEntry which is the registered listing.
// Returns error which is the injected error for directory, or os.ErrNotExist.
func (reader *FileReader) ReadDir(_ context.Context, directory string) ([]os.DirEntry, error) {
	if reader.ReadDirErrors != nil {
		if err, ok := reader.ReadDirErrors[directory]; ok {
			return nil, err
		}
	}
	if reader.Directories != nil {
		if entries, ok := reader.Directories[directory]; ok {
			return entries, nil
		}
	}
	return nil, os.ErrNotExist
}

// DirEntry is a directory entry with a fixed name and kind.
type DirEntry struct {
	// EntryName is the directory entry's name.
	EntryName string

	// Directory reports whether the entry is a directory.
	Directory bool
}

// Name returns the entry name.
//
// Returns string which is the configured entry name.
func (entry *DirEntry) Name() string {
	return entry.EntryName
}

// IsDir reports whether the entry is a directory.
//
// Returns bool which is true for a directory.
func (entry *DirEntry) IsDir() bool {
	return entry.Directory
}

// Type returns os.ModeDir for a directory and zero for a file.
//
// Returns fs.FileMode which is os.ModeDir for directories and zero for files.
func (entry *DirEntry) Type() fs.FileMode {
	if entry.Directory {
		return os.ModeDir
	}
	return 0
}

// Info returns the entry's file information: its name and kind, with no size and a zero
// modification time.
//
// Returns fs.FileInfo which describes the entry.
// Returns error which is always nil.
func (entry *DirEntry) Info() (fs.FileInfo, error) {
	return entryInfo{entry: entry}, nil
}

// entryInfo is the fs.FileInfo of a DirEntry.
type entryInfo struct {
	// entry is the entry described.
	entry *DirEntry
}

// Name returns the entry's name.
//
// Returns string which is the configured entry name.
func (info entryInfo) Name() string {
	return info.entry.EntryName
}

// Size returns zero, since an entry holds no content.
//
// Returns int64 which is always zero.
func (entryInfo) Size() int64 {
	return 0
}

// Mode returns os.ModeDir for a directory and zero for a file.
//
// Returns fs.FileMode which is the entry's kind.
func (info entryInfo) Mode() fs.FileMode {
	return info.entry.Type()
}

// ModTime returns the zero time.
//
// Returns time.Time which is always the zero time.
func (entryInfo) ModTime() time.Time {
	return time.Time{}
}

// IsDir reports whether the entry is a directory.
//
// Returns bool which is true for a directory.
func (info entryInfo) IsDir() bool {
	return info.entry.Directory
}

// Sys returns nil, since an entry has no underlying data source.
//
// Returns any which is always nil.
func (entryInfo) Sys() any {
	return nil
}
