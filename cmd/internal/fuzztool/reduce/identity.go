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

//go:build linux || darwin

package reduce

import (
	"os"
	"regexp"
	"strings"

	"seagull.sh/seagull/cmd/internal/fuzztool/fuzztext"
)

const (
	// locationPanic is the location of a failure that panicked.
	locationPanic = "panic"

	// locationUnknown is the location of a failure no source line names.
	locationUnknown = "unlocated"

	// continuationLines is how many lines an empty failure message borrows from below.
	continuationLines = 3

	// fileGroup is the failing file in a failureLine match.
	fileGroup = 1

	// lineGroup is the failing line number.
	lineGroup = 2

	// messageGroup is the failure message.
	messageGroup = 3
)

// mask replaces one run-specific detail of a failure message.
type mask struct {
	// pattern matches the detail.
	pattern *regexp.Regexp

	// replacement stands in for it.
	replacement string
}

var (
	// failureLine matches a test failure's source location and message.
	failureLine = regexp.MustCompile(`^[[:space:]]+(\S+\.go):(\d+): (.*)$`)

	// panicLine matches a panic's message.
	panicLine = regexp.MustCompile(`^panic: (.*)$`)

	// masks replace the parts of a failure message that change between runs, in order.
	masks = []mask{
		{pattern: regexp.MustCompile(`sgl_[a-z]+_[0-9a-f]{8,}_\d+_\d+`), replacement: "<schema>"},
		{pattern: regexp.MustCompile(`/tmp/[^\s:'"]+`), replacement: "<temp>"},
		{pattern: regexp.MustCompile(regexp.QuoteMeta(os.TempDir()) + `/[^\s:'"]+`), replacement: "<temp>"},
		{pattern: regexp.MustCompile(`0x[0-9a-fA-F]+`), replacement: "<address>"},
		{pattern: regexp.MustCompile(`\d+(\.\d+)?(ns|µs|ms|s)\b`), replacement: "<duration>"},
		{pattern: regexp.MustCompile(`\d+`), replacement: "<n>"},
	}
)

// Identity names a failure by its test, its first failing line and its masked message.
type Identity struct {
	// Function is the fuzz target.
	Function string

	// Location is the failing file and line, panic or unlocated.
	Location string

	// Message is the masked failure message.
	Message string
}

// list returns the identity as the three-element list the summary records.
//
// Returns []string which holds the function, location and message.
func (identity Identity) list() []string {
	return []string{identity.Function, identity.Location, identity.Message}
}

// identify reads a failing replay's identity from its output.
//
// Takes function (string) which is the fuzz target.
// Takes output (string) which is the replay's combined output.
//
// Returns Identity which identifies the failure.
// Returns bool which is false when the output shows no failure of the target.
func identify(function string, output string) (Identity, bool) {
	if !strings.Contains(output, "--- FAIL: "+function) && !strings.Contains(output, "panic: ") {
		return Identity{}, false
	}
	lines := fuzztext.SplitLines(output)
	for index, line := range lines {
		if match := panicLine.FindStringSubmatch(line); match != nil {
			return Identity{Function: function, Location: locationPanic, Message: maskMessage(match[1])}, true
		}
		match := failureLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		message := match[messageGroup]
		if fuzztext.Strip(message) == "" && index+1 < len(lines) {
			following := lines[index+1 : min(len(lines), index+1+continuationLines)]
			parts := make([]string, 0, len(following))
			for _, part := range following {
				parts = append(parts, fuzztext.Strip(part))
			}
			message = strings.Join(parts, " ")
		}
		return Identity{Function: function, Location: match[fileGroup] + ":" + match[lineGroup], Message: maskMessage(message)}, true
	}
	return Identity{Function: function, Location: locationUnknown, Message: ""}, true
}

// maskMessage replaces the parts of a failure message that change between runs.
//
// Takes message (string) which is the raw message.
//
// Returns string which is the masked, stripped message.
func maskMessage(message string) string {
	for _, replacement := range masks {
		message = replacement.pattern.ReplaceAllLiteralString(message, replacement.replacement)
	}
	return fuzztext.Strip(message)
}
