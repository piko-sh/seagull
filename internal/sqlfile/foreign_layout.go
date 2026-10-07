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
	"fmt"
	"regexp"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
)

// ForeignDownSection is the down half of a migration written in another tool's
// single-file layout.
type ForeignDownSection struct {
	// Marker is the line that starts the section, as written.
	Marker string

	// Tool names the migration tool whose layout the marker belongs to.
	Tool string

	// Line is the 1-based line of the marker.
	Line int
}

// foreignDownMarker is the comment line with which another migration tool's single-file
// layout starts the down half of a migration.
type foreignDownMarker struct {
	// pattern matches the whole line, ignoring case where the tool does.
	pattern *regexp.Regexp

	// tool names the migration tool whose layout uses the marker.
	tool string
}

// foreignDownMarkers lists the down-section markers of the single-file layouts other
// migration tools use: goose, sql-migrate, dbmate and tern.
var foreignDownMarkers = []foreignDownMarker{
	{tool: "goose", pattern: regexp.MustCompile(`(?i)^\s*--\s*\+goose\s+down\b`)},
	{tool: "sql-migrate", pattern: regexp.MustCompile(`(?i)^\s*--\s*\+migrate\s+down\b`)},
	{tool: "dbmate", pattern: regexp.MustCompile(`(?i)^\s*--\s*migrate:down\b`)},
	{tool: "tern", pattern: regexp.MustCompile(`(?i)^\s*-{3,}\s*create above\s*/\s*drop below\s*-{3,}\s*$`)},
}

// FindForeignDownSection looks for the line with which another migration tool's
// single-file layout starts a migration's down half, such as goose's "-- +goose Down".
// Seagull's runner applies an up migration whole, so in an up migration such a section
// would run straight after the statements it undoes.
//
// Takes content ([]byte) which is the up migration's SQL.
//
// Returns ForeignDownSection which describes the first marker found.
// Returns bool which is false when the migration holds no such marker.
func FindForeignDownSection(content []byte) (ForeignDownSection, bool) {
	line := 0
	for text := range strings.Lines(string(content)) {
		line++
		text = strings.TrimRight(text, "\r\n")
		for _, marker := range foreignDownMarkers {
			if marker.pattern.MatchString(text) {
				return ForeignDownSection{Marker: strings.TrimSpace(text), Tool: marker.tool, Line: line}, true
			}
		}
	}
	return ForeignDownSection{}, false
}

// ForeignDownSectionError reports an up migration that holds another tool's down section,
// which the runner would apply with the rest of the file, undoing it, and which replaying
// the file into the schema would undo in the same way.
//
// Takes filename (string) which is the up migration's file name.
// Takes section (ForeignDownSection) which is the section found.
//
// Returns diag.SourceError which is the Q048 error, at the marker.
func ForeignDownSectionError(filename string, section ForeignDownSection) diag.SourceError {
	downName := strings.TrimSuffix(filename, upMigrationSuffix) + downMigrationSuffix
	return diagbuild.BlockWithSuggestion(filename, section.Line, diag.CodeIgnoredMigrationFile, diag.SeverityError,
		fmt.Sprintf("%s holds a %s down section from line %d (%s); the migration runner applies an up "+
			"migration whole, so the statements after it would undo the migration as soon as it ran",
			filename, section.Tool, section.Line, section.Marker),
		fmt.Sprintf("move the statements from line %d on into %s", section.Line, downName))
}
