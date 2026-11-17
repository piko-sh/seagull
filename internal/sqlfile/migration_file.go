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

// MigrationFile represents a parsed migration file with version, direction, and content.
// Files follow the {version}_{name}.{up|down}.sql naming convention.
type MigrationFile struct {
	// Name is the descriptive segment extracted from the filename (e.g. "create_users" from
	// "0001_create_users.up.sql").
	Name string

	// Filename is the original filename for error reporting.
	Filename string

	// Checksum is the SHA-256 hex digest of Content with a byte order mark removed and CRLF
	// line endings written as LF, computed at read time.
	Checksum string

	// LegacyChecksum is the SHA-256 hex digest of the content as written, which earlier
	// releases recorded, when it differs from Checksum because the content has a byte order
	// mark or CRLF line endings. It is empty otherwise.
	LegacyChecksum string

	// Content is the raw SQL content of the migration file.
	Content []byte

	// Version is the numeric prefix extracted from the filename. Stored as int64 to support
	// both sequential (0001) and timestamp-based (20260324120000) versioning.
	Version int64

	// Down reports whether this is a down migration, which reverts its version, rather than
	// an up migration, which applies it.
	Down bool
}

// MatchesChecksum reports whether a recorded checksum is this file's, as recorded now or
// by an earlier release that hashed the bytes as written.
//
// Takes recorded (string) which is the checksum the history holds.
//
// Returns bool which is true when the file is the one recorded.
func (file *MigrationFile) MatchesChecksum(recorded string) bool {
	return matchesChecksum(recorded, file.Checksum, file.LegacyChecksum)
}

// matchesChecksum reports whether a recorded checksum is a file's current or legacy one.
//
// Takes recorded (string) which is the checksum the history holds.
// Takes checksum (string) which is the file's checksum.
// Takes legacy (string) which is the file's legacy checksum, or empty.
//
// Returns bool which is true when the recorded checksum is either of them.
func matchesChecksum(recorded, checksum, legacy string) bool {
	if recorded == checksum {
		return true
	}
	return legacy != "" && recorded == legacy
}
