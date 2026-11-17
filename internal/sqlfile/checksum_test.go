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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentChecksumsIgnoreLineEndingsAndByteOrderMarks(t *testing.T) {
	t.Parallel()
	plain := []byte("CREATE TABLE boats (id int);\nCREATE TABLE ports (id int);\n")
	windows := []byte("CREATE TABLE boats (id int);\r\nCREATE TABLE ports (id int);\r\n")
	marked := append([]byte{0xEF, 0xBB, 0xBF}, plain...)

	plainChecksum, plainLegacy := contentChecksums(plain)
	windowsChecksum, windowsLegacy := contentChecksums(windows)
	markedChecksum, markedLegacy := contentChecksums(marked)

	assert.Equal(t, computeChecksum(plain), plainChecksum, "content without CRLF or a byte order mark hashes as before")
	assert.Empty(t, plainLegacy)
	assert.Equal(t, plainChecksum, windowsChecksum, "CRLF line endings do not change the checksum")
	assert.Equal(t, plainChecksum, markedChecksum, "a byte order mark does not change the checksum")
	assert.Equal(t, computeChecksum(windows), windowsLegacy, "the bytes as written keep the checksum earlier releases recorded")
	assert.Equal(t, computeChecksum(marked), markedLegacy)
}

func TestMatchesChecksumAcceptsTheChecksumEarlierReleasesRecorded(t *testing.T) {
	t.Parallel()
	content := []byte("CREATE TABLE boats (id int);\r\n")
	file := MigrationFile{Content: content}
	file.Checksum, file.LegacyChecksum = contentChecksums(content)

	assert.True(t, file.MatchesChecksum(file.Checksum))
	assert.True(t, file.MatchesChecksum(computeChecksum(content)), "a history recorded from the raw bytes still matches")
	assert.False(t, file.MatchesChecksum(computeChecksum([]byte("CREATE TABLE ports (id int);\n"))))
	seed := SeedFile{Checksum: file.Checksum, LegacyChecksum: file.LegacyChecksum}
	assert.True(t, seed.MatchesChecksum(computeChecksum(content)))
}

func TestMatchesChecksumNeverMatchesAnEmptyLegacyChecksum(t *testing.T) {
	t.Parallel()
	file := MigrationFile{Checksum: computeChecksum([]byte("SELECT 1;\n"))}

	assert.False(t, file.MatchesChecksum(""), "an empty recorded checksum matches no file")
	assert.True(t, file.MatchesChecksum(file.Checksum))
}
