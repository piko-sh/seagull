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

package intake

import (
	"regexp"
	"strings"

	"seagull.sh/seagull/cmd/internal/fuzztool/fuzztext"
)

var (
	// conditionLine matches an onlyif or skipif line.
	conditionLine = regexp.MustCompile(`^(onlyif|skipif)[[:space:]]+(\S+)`)

	// schemaStatement matches a statement that changes the schema.
	schemaStatement = regexp.MustCompile(`(?i)^[[:space:]]*(CREATE|ALTER|DROP)\b`)

	// queryStatement matches a statement that reads rows.
	queryStatement = regexp.MustCompile(`(?i)^[[:space:]]*(SELECT|WITH|VALUES)\b`)
)

// condition is one onlyif or skipif line.
type condition struct {
	// engine is the engine name the line gives.
	engine string

	// only is true for onlyif and false for skipif.
	only bool
}

// sqlRecord is one statement or query read from a suite file.
type sqlRecord struct {
	// sql is the record's text.
	sql string

	// conditions are the record's onlyif and skipif lines.
	conditions []condition

	// line is the record's first line, counted from one.
	line int

	// query is true for a query and false for a statement.
	query bool
}

// logicRecords reads the statement and query records of a sqllogictest file.
//
// Takes text (string) which is the file's content.
//
// Returns []sqlRecord which holds the successful statements and every query.
func logicRecords(text string) []sqlRecord {
	lines := fuzztext.SplitLines(text)
	var records []sqlRecord
	for index := 0; index < len(lines); {
		var conditions []condition
		conditions, index = readConditions(lines, index)
		if index >= len(lines) {
			break
		}
		header := fuzztext.Fields(lines[index])
		if len(header) == 0 || header[0] != "statement" && header[0] != "query" {
			index++
			continue
		}
		start := index + 1
		var end int
		end, index = readBody(lines, start)
		if header[0] == "statement" && (len(header) < 2 || header[1] != "ok") {
			continue
		}
		records = append(records, sqlRecord{
			sql:        strings.Join(lines[start:end], "\n"),
			conditions: conditions,
			line:       start + 1,
			query:      header[0] == "query",
		})
	}
	return records
}

// readConditions reads the onlyif and skipif lines that start a record.
//
// Takes lines ([]string) which are the file's lines.
// Takes index (int) which is the first line to read.
//
// Returns []condition which holds each condition read.
// Returns int which is the first line after them.
func readConditions(lines []string, index int) ([]condition, int) {
	var conditions []condition
	for ; index < len(lines); index++ {
		match := conditionLine.FindStringSubmatch(lines[index])
		if match == nil {
			break
		}
		conditions = append(conditions, condition{engine: match[2], only: match[1] == "onlyif"})
	}
	return conditions, index
}

// readBody finds a record's SQL, which ends at a blank line or a ---- separator, and
// skips any expected results up to the next blank line.
//
// Takes lines ([]string) which are the file's lines.
// Takes start (int) which is the record's first SQL line.
//
// Returns int which is the line after the SQL.
// Returns int which is the line after the record.
func readBody(lines []string, start int) (end int, next int) {
	end = start
	for end < len(lines) && fuzztext.Strip(lines[end]) != "" && fuzztext.Strip(lines[end]) != "----" {
		end++
	}
	next = end
	for next < len(lines) && fuzztext.Strip(lines[next]) != "" {
		next++
	}
	return end, next
}

// plainRecords reads each statement of a plain SQL file.
//
// Takes text (string) which is the file's content.
//
// Returns []sqlRecord which holds every non-empty statement.
func plainRecords(text string) []sqlRecord {
	var records []sqlRecord
	line := 1
	for statement := range strings.SplitSeq(text, ";") {
		stripped := fuzztext.Strip(statement)
		if stripped != "" {
			leading := statement[:len(statement)-len(fuzztext.TrimLeft(statement))]
			records = append(records, sqlRecord{
				sql:        stripped,
				conditions: nil,
				line:       line + strings.Count(leading, "\n"),
				query:      queryStatement.MatchString(stripped),
			})
		}
		line += strings.Count(statement, "\n")
	}
	return records
}
