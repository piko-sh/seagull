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

package catalogue

import "errors"

var (
	// ErrDocumentCount is returned when a manifest is empty or holds more than one YAML
	// document.
	ErrDocumentCount = errors.New("a manifest holds exactly one YAML document")

	// ErrNotEmpty is returned when a directory an export or a check writes into already
	// holds something.
	ErrNotEmpty = errors.New("the directory is not empty")

	// ErrFileTooLarge is returned when a file is larger than the catalogue reads.
	ErrFileTooLarge = errors.New("the file is too large")

	// ErrNotRegular is returned when a path the catalogue reads names something other than a
	// regular file, such as a named pipe.
	ErrNotRegular = errors.New("not a regular file")

	// ErrTooManyFiles is returned when a variant holds more files than an export copies.
	ErrTooManyFiles = errors.New("the variant holds too many files")

	// ErrExportTooLarge is returned when the files an export copies add up to more than it
	// writes.
	ErrExportTooLarge = errors.New("the export is too large")

	// ErrNoCommand is returned when a command runner is given no program to run.
	ErrNoCommand = errors.New("no command to run")

	// ErrOutputTooLarge is returned when a command writes more output than a check keeps.
	ErrOutputTooLarge = errors.New("the command wrote too much output")
)
