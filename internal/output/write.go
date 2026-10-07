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
	"io/fs"
	"strings"

	"piko.sh/safedisk"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// DefaultPermissions is the permission of written files unless Options sets another.
	DefaultPermissions fs.FileMode = 0o644
)

// Options controls one Write.
type Options struct {
	// Header is the first line of every file the run generated; files a previous run
	// generated with it are pruned when absent from the run's output.
	Header string

	// LegacyHeaders lists earlier headers whose files are pruned as well, so a change of
	// header does not strand the files the old one marked.
	LegacyHeaders []string

	// Permissions is the permission of written files; zero selects DefaultPermissions.
	Permissions fs.FileMode

	// Prune removes stale generated files after writing.
	Prune bool
}

// Write writes generated files, creates the output directory when needed and optionally
// prunes obsolete generated files. Empty files are omitted, and files with matching
// content and permissions are left unchanged.
//
// Takes directory (string) which is the output directory.
// Takes files ([]sqlmodel.GeneratedFile) which are the files to write.
// Takes options (Options) which controls the header, pruning and permissions.
//
// Returns error which wraps fault.ErrInvalidConfig for an empty directory or a file name
// that is not a plain base name, or the underlying error when writing or pruning fails.
func Write(directory string, files []sqlmodel.GeneratedFile, options Options) error {
	if directory == "" {
		return fmt.Errorf("output directory is empty: %w", fault.ErrInvalidConfig)
	}
	for _, file := range files {
		if err := validateName(file.Name); err != nil {
			return err
		}
	}

	sandbox, err := safedisk.NewSandbox(directory, safedisk.ModeReadWrite)
	if err != nil {
		return fmt.Errorf("opening output directory %q: %w", directory, err)
	}

	permissions := options.Permissions
	if permissions == 0 {
		permissions = DefaultPermissions
	}

	writeErr := writeAll(sandbox, files, permissions)
	if writeErr == nil && options.Prune {
		headers := append([]string{options.Header}, options.LegacyHeaders...)
		writeErr = pruneStaleGeneratedFiles(sandbox, generatedFileNameSet(files), headers...)
	}
	return errors.Join(writeErr, sandbox.Close())
}

// writeAll writes every file with content into the sandbox atomically, leaving a file
// that already holds the content with the permission alone, so its modification time, and
// every build step keyed on it, stays as it was.
//
// Takes sandbox (safedisk.Sandbox) which is rooted at the output directory.
// Takes files ([]sqlmodel.GeneratedFile) which are the files to write.
// Takes permissions (fs.FileMode) which is the permission of each file.
//
// Returns error when a file cannot be written.
func writeAll(sandbox safedisk.Sandbox, files []sqlmodel.GeneratedFile, permissions fs.FileMode) error {
	for _, file := range files {
		if len(file.Content) == 0 || unchanged(sandbox, file, permissions) {
			continue
		}
		if err := sandbox.WriteFileAtomic(file.Name, file.Content, permissions); err != nil {
			return fmt.Errorf("writing %s: %w", file.Name, err)
		}
	}
	return nil
}

// unchanged reports whether the output directory already holds a file as a write would
// leave it: a regular file, not a link, with the permission and exactly the content.
//
// Takes sandbox (safedisk.Sandbox) which is rooted at the output directory.
// Takes file (sqlmodel.GeneratedFile) which is the file to write.
// Takes permissions (fs.FileMode) which is the permission it is written with.
//
// Returns bool which is false when the file is missing, differs or cannot be read, so it
// is written.
func unchanged(sandbox safedisk.Sandbox, file sqlmodel.GeneratedFile, permissions fs.FileMode) bool {
	info, err := sandbox.Lstat(file.Name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != permissions.Perm() || info.Size() != int64(len(file.Content)) {
		return false
	}
	existing, _, err := sandbox.ReadFileLimit(file.Name, int64(len(file.Content)))
	return err == nil && bytes.Equal(existing, file.Content)
}

// validateName rejects a generated file name that is not a plain base name.
//
// Takes name (string) which is the file name.
//
// Returns error which wraps fault.ErrInvalidConfig when the name is empty, holds a slash
// or backslash, or is . or ..
func validateName(name string) error {
	if name == "." || strings.ContainsAny(name, `/\`) || !fs.ValidPath(name) {
		return fmt.Errorf("generated file name %q is not a plain file name: %w", name, fault.ErrInvalidConfig)
	}
	return nil
}
