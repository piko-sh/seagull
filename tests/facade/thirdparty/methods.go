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

package thirdparty

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"seagull.sh/seagull/sdk/emit"
)

const (
	// commentMarker opens a line of a doc comment.
	commentMarker = "//"

	// byteOrderMark is the rune Go source may hold only at its very start.
	byteOrderMark = '\uFEFF'

	// contextPackage is the import path of the context package.
	contextPackage = "context"

	// sqlPackage is the import path of database/sql.
	sqlPackage = "database/sql"

	// errorsPackage is the import path of the errors package.
	errorsPackage = "errors"

	// iterPackage is the import path of the iter package.
	iterPackage = "iter"

	// referenceOpen takes the address of a scan target.
	referenceOpen = "&"

	// rowAccess reads a field of the row being scanned.
	rowAccess = "row."

	// queryCall calls the connection's QueryContext.
	queryCall = "%s.%s.QueryContext(ctx, %s%s)"

	// failWithError returns a method's error.
	failWithError = "if err != nil {\n%s\n}\n"

	// failScan returns the error of a failed scan.
	failScan = "if err := rows.Scan(%s); err != nil {\n%s\n}\n"

	// failRows returns the error a scan loop ended with.
	failRows = "if err := rows.Err(); err != nil {\n%s\n}\n"

	// declareRow declares the row a scan fills.
	declareRow = "var row %s\n"

	// closeRows closes the rows when the generated method returns.
	closeRows = "defer rows.Close()\n"

	// openRowLoop starts the loop over the rows.
	openRowLoop = "for rows.Next() {\n"

	// failNil returns no value and err from a method returning a slice, a map or an
	// interface.
	failNil = "return nil, err"

	// resultsOpen opens a list of results.
	resultsOpen = "("
)

// writeMethod writes the Queries method that runs a query, its SQL edited first where the
// call decides the SQL.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
func (current *generation) writeMethod(file *source, rendered *query) {
	current.writeDescription(file, rendered)
	file.printf("func (%s *Queries) %s(%s) %s {\n", receiver, rendered.naming.Method, current.parameters(file, rendered), results(file, rendered))
	command := rendered.analysed.Command
	if command == emit.QueryCommandStream {
		file.printf("return func(yield func(%s, error) bool) {\n", rendered.naming.Row)
	}
	failure := failureCode(rendered)
	run := current.prepareStatement(file, rendered, failure)
	switch {
	case command == emit.QueryCommandOne:
		writeOne(file, rendered, run)
	case command == emit.QueryCommandStream:
		writeStream(file, rendered, run)
	case command == emit.QueryCommandMany && grouped(rendered.analysed) && groupKeyColumn(rendered.analysed) >= 0:
		current.writeGrouped(file, rendered, run)
	case command == emit.QueryCommandMany:
		writeMany(file, rendered, run, current.request.Style.EmptySlices)
	default:
		writeExec(file, rendered, run)
	}
	file.printf(closeBlock + "\n")
}

// writeDescription writes a query's description as its method's doc comment, when the
// style asks for doc comments.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
func (current *generation) writeDescription(file *source, rendered *query) {
	if current.request.Style.DocComments {
		writeComment(file, "", rendered.analysed.Description)
	}
}

// parameters writes a method's parameters: the context, then the Params struct, or the
// lone parameter on its own, or nothing more.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
//
// Returns string which is the parameter list.
func (current *generation) parameters(file *source, rendered *query) string {
	list := "ctx " + file.qualified(contextPackage, "Context")
	switch {
	case rendered.naming.Params != "" && rendered.analysed.Command == emit.QueryCommandBatch:
		return list + argumentSeparator + rendered.naming.ParamsArgument + " " + slicePrefix + rendered.naming.Params
	case rendered.naming.Params != "":
		return list + argumentSeparator + rendered.naming.ParamsArgument + " " + rendered.naming.Params
	case len(rendered.naming.ParameterFields) > 0:
		return list + argumentSeparator + rendered.naming.ParameterFields[0] + " " + current.parameterSource(file, rendered, 0)
	default:
		return list
	}
}

// writeComment writes text as a doc comment, each line after an indent.
//
// Takes file (*source) which is the file.
// Takes indent (string) which starts each line.
// Takes text (string) which is the comment's text.
func writeComment(file *source, indent, text string) {
	for _, line := range commentLines(text) {
		file.write(indent, line, "\n")
	}
}

// commentLines writes text as the lines of a Go doc comment: each line without its
// trailing spaces, a bare // between paragraphs, and no empty line at either end. A rune
// Go source cannot hold in a comment is replaced, and carriage returns are dropped.
//
// Takes text (string) which is the text, its lines separated by newlines.
//
// Returns []string which holds the comment's lines, or nil for text that is only
// whitespace.
func commentLines(text string) []string {
	lines := strings.Split(strings.Map(commentRune, text), "\n")
	for index, line := range lines {
		lines[index] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	first, last := 0, len(lines)
	for first < last && lines[first] == "" {
		first++
	}
	for last > first && lines[last-1] == "" {
		last--
	}
	if first == last {
		return nil
	}
	comment := make([]string, 0, last-first)
	for _, line := range lines[first:last] {
		if line == "" {
			comment = append(comment, commentMarker)
			continue
		}
		comment = append(comment, commentMarker+" "+line)
	}
	return comment
}

// commentRune returns the rune a doc comment writes for a rune of its text: none for a
// carriage return, the replacement character for a NUL, a byte order mark or a byte that
// is not UTF-8, and the rune itself otherwise.
//
// Takes character (rune) which is the rune of the text.
//
// Returns rune which is the rune written, or -1 to leave it out.
func commentRune(character rune) rune {
	switch character {
	case '\r':
		return -1
	case 0, byteOrderMark:
		return utf8.RuneError
	default:
		return character
	}
}

// results writes a method's results, as its command and optional: say.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
//
// Returns string which is the result list.
func results(file *source, rendered *query) string {
	row := rendered.naming.Row
	switch rendered.analysed.Command {
	case emit.QueryCommandOne:
		if rendered.analysed.Optional {
			return resultsOpen + row + ", bool, error)"
		}
		return resultsOpen + row + ", error)"
	case emit.QueryCommandMany:
		return resultsOpen + slicePrefix + row + ", error)"
	case emit.QueryCommandStream:
		return file.qualified(iterPackage, "Seq2") + "[" + row + ", error]"
	case emit.QueryCommandExecRows:
		return "(int64, error)"
	case emit.QueryCommandExecResult:
		return resultsOpen + file.qualified(sqlPackage, "Result") + ", error)"
	default:
		return "error"
	}
}

// failureCode returns the code a method runs to report err, which suits its results.
//
// Takes rendered (*query) which is the query.
//
// Returns string which is the code.
func failureCode(rendered *query) string {
	row := rendered.naming.Row
	switch rendered.analysed.Command {
	case emit.QueryCommandOne:
		if rendered.analysed.Optional {
			return "return " + row + "{}, false, err"
		}
		return "return " + row + "{}, err"
	case emit.QueryCommandMany, emit.QueryCommandExecResult:
		return failNil
	case emit.QueryCommandStream:
		return "yield(" + row + "{}, err)\nreturn"
	case emit.QueryCommandExecRows:
		return "return 0, err"
	default:
		return "return err"
	}
}

// scanTargets returns the addresses a row is scanned into: each column's field of the
// row, or of its embed's struct.
//
// Takes rendered (*query) which is the query.
//
// Returns string which is the comma-separated addresses.
func scanTargets(rendered *query) string {
	columns := rendered.analysed.OutputColumns
	targets := make([]string, 0, len(columns))
	for index := range columns {
		field := rendered.naming.ColumnFields[index]
		if columns[index].IsEmbedded {
			field = embedField(rendered, embedGroup(&columns[index])) + fieldSeparator + field
		}
		targets = append(targets, referenceOpen+rowAccess+field)
	}
	return strings.Join(targets, argumentSeparator)
}

// writeOne writes the body of a command: one method: it scans the one row, and for an
// optional query reports a missing row as false rather than as sql.ErrNoRows.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
// Takes run (statement) which says how to pass the SQL and the arguments.
func writeOne(file *source, rendered *query, run statement) {
	row := rendered.naming.Row
	file.printf(declareRow, row)
	file.printf("err := %s.%s.QueryRowContext(ctx, %s%s).Scan(%s)\n", run.caller, connection, run.query, run.arguments, scanTargets(rendered))
	if rendered.analysed.Optional {
		file.printf("if %s(err, %s) {\nreturn %s{}, false, nil\n}\n", file.qualified(errorsPackage, "Is"), file.qualified(sqlPackage, "ErrNoRows"), row)
		file.printf(failWithError, failureCode(rendered))
		file.printf("return row, true, nil\n")
		return
	}
	file.printf(failWithError, failureCode(rendered))
	file.printf("return row, nil\n")
}

// writeMany writes the body of a command: many method: it scans every row into a slice,
// which is nil when no row matches unless the style asks for empty slices.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
// Takes run (statement) which says how to pass the SQL and the arguments.
// Takes emptySlices (bool) which is true when no row gives an empty slice.
func writeMany(file *source, rendered *query, run statement, emptySlices bool) {
	failure := failNil
	file.printf("rows, err := "+queryCall+"\n", run.caller, connection, run.query, run.arguments)
	file.printf(failWithError, failure)
	declaration := "var results " + slicePrefix + rendered.naming.Row
	if emptySlices {
		declaration = "results := " + slicePrefix + rendered.naming.Row + "{}"
	}
	file.write(closeRows, declaration, "\n", openRowLoop)
	file.printf(declareRow, rendered.naming.Row)
	file.printf(failScan, scanTargets(rendered), failure)
	file.printf("results = append(results, row)\n" + closeBlock)
	file.printf(failRows, failure)
	file.printf("return results, nil\n")
}

// writeStream writes the body of a command: stream method's iterator, which yields each
// row as it is scanned and stops when the caller does.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
// Takes run (statement) which says how to pass the SQL and the arguments.
func writeStream(file *source, rendered *query, run statement) {
	failure := failureCode(rendered)
	file.printf("rows, err := "+queryCall+"\n", run.caller, connection, run.query, run.arguments)
	file.printf(failWithError, failure)
	file.printf(closeRows + openRowLoop)
	file.printf(declareRow, rendered.naming.Row)
	file.printf(failScan, scanTargets(rendered), failure)
	file.printf("if !yield(row, nil) {\nreturn\n}\n" + closeBlock)
	file.printf("if err := rows.Err(); err != nil {\nyield(%s{}, err)\n}\n", rendered.naming.Row)
	file.printf(closeBlock)
}

// writeExec writes the body of an exec, execrows or execresult method.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
// Takes run (statement) which says how to pass the SQL and the arguments.
func writeExec(file *source, rendered *query, run statement) {
	call := run.caller + fieldSeparator + connection + ".ExecContext(ctx, " + run.query + run.arguments + callClose
	switch rendered.analysed.Command {
	case emit.QueryCommandExecRows:
		file.printf("result, err := %s\n", call)
		file.printf(failWithError, failureCode(rendered))
		file.printf("return result.RowsAffected()\n")
	case emit.QueryCommandExecResult:
		file.printf("return %s\n", call)
	default:
		file.printf("_, err := %s\nreturn err\n", call)
	}
}
