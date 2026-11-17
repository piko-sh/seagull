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

package fuzzcase

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing/fstest"
)

// maxBundleBytes bounds encoded source bundles.
const maxBundleBytes = 64 << 10

// maxBundleFiles bounds files in each source directory.
const maxBundleFiles = 8

// bundleDirectoryMode makes in-memory directories readable.
const bundleDirectoryMode = 0o755

// bundleMigrations is the directory holding a bundle's migration files.
const bundleMigrations = "migrations"

// bundleQueries is the directory holding a bundle's query files.
const bundleQueries = "queries"

// Bundle preserves SQL file boundaries and custom function configuration.
type Bundle struct {
	// Origin names a seed directory for audits and is never part of a fuzz input.
	Origin string `json:"-"`

	// Files holds migration and query files in their original order.
	Files []File `json:"files"`

	// Functions holds the customFunctions array from a case specification.
	Functions json.RawMessage `json:"customFunctions,omitempty"`

	// Version identifies the envelope format.
	Version int `json:"version"`
}

// File is one SQL source in a bundle.
type File struct {
	// Name is a relative path below migrations or queries.
	Name string `json:"name"`

	// SQL is the file's content.
	SQL string `json:"sql"`
}

// FS makes an isolated filesystem for a decoded bundle.
//
// Returns fstest.MapFS which owns the source bytes.
func (bundle Bundle) FS() fstest.MapFS {
	files := fstest.MapFS{
		bundleMigrations: &fstest.MapFile{Mode: fs.ModeDir | bundleDirectoryMode},
		bundleQueries:    &fstest.MapFile{Mode: fs.ModeDir | bundleDirectoryMode},
	}
	for _, file := range bundle.Files {
		files[file.Name] = &fstest.MapFile{Data: []byte(file.SQL)}
	}
	return files
}

// DecodeBundle validates a bounded file envelope.
//
// Takes data ([]byte) which contains JSON.
//
// Returns Bundle which holds the decoded files.
// Returns error when the envelope is invalid.
func DecodeBundle(data []byte) (Bundle, error) {
	if len(data) > maxBundleBytes {
		return Bundle{}, fmt.Errorf("bundle exceeds %d bytes", maxBundleBytes)
	}
	var bundle Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return Bundle{}, fmt.Errorf("decoding bundle: %w", err)
	}
	if bundle.Version != Version {
		return Bundle{}, fmt.Errorf("unsupported bundle version %d", bundle.Version)
	}
	seen := make(map[string]bool)
	counts := make(map[string]int)
	for _, file := range bundle.Files {
		directory := path.Dir(file.Name)
		if !validBundlePath(file.Name) || seen[file.Name] {
			return Bundle{}, fmt.Errorf("invalid or duplicate source path %q", file.Name)
		}
		seen[file.Name] = true
		counts[directory]++
		if counts[directory] > maxBundleFiles {
			return Bundle{}, fmt.Errorf("too many %s files", directory)
		}
	}
	return bundle, nil
}

// validBundlePath accepts a slash-separated SQL file directly below a source directory.
//
// Takes name (string) which is the file's path in the bundle.
//
// Returns bool which is false for unsafe, misplaced or non-SQL paths.
func validBundlePath(name string) bool {
	directory := path.Dir(name)
	return fs.ValidPath(name) && !strings.Contains(name, "\\") && path.Ext(name) == ".sql" &&
		(directory == bundleMigrations || directory == bundleQueries)
}
