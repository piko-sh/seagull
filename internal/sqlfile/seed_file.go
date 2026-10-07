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

// SeedFile represents a parsed seed file with version, name, and content.
type SeedFile struct {
	// Name holds the descriptive segment of the filename (e.g. "demo_authors").
	Name string

	// Filename holds the original base name for error reporting.
	Filename string

	// Checksum holds the SHA-256 hex digest of Content.
	Checksum string

	// Content holds the raw SQL bytes of the seed file.
	Content []byte

	// Version holds the numeric prefix parsed from the filename.
	Version int64
}
