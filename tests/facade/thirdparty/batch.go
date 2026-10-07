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
	"strconv"
	"strings"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit/emit_kit"
)

const (
	// stringsPackage is the import path of the strings package.
	stringsPackage = "strings"

	// strconvPackage is the import path of the strconv package.
	strconvPackage = "strconv"

	// concatenation joins two strings in Go source.
	concatenation = " + "
)

// writeBatch writes a batch method using multi-row INSERT statements within the bind
// limit, or one statement per row when the statement writes a clause after its VALUES
// row, which may act on the rows of one statement, such as an upsert meeting one key
// twice.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query; its row template is set.
func (current *generation) writeBatch(file *source, rendered *query) {
	row := rendered.row
	naming := rendered.naming
	perRow := len(row.Arguments)
	chunk := max(current.maxBinds()/max(perRow, 1), 1)
	if emit_kit.ClausesAfterValues(rendered.analysed, &current.request.Dialect.Lexical) {
		chunk = 1
	}
	items := naming.ParamsArgument
	file.printf("func (%s *Queries) %s(%s) error {\n", receiver, naming.Method, current.parameters(file, rendered))
	file.printf("for offset := 0; offset < len(%s); offset += %d {\n", items, chunk)
	file.printf("end := min(offset+%d, len(%s))\n", chunk, items)
	file.printf("var values %s\n", file.qualified(stringsPackage, "Builder"))
	file.printf("args := make([]any, 0, (end-offset)*%d)\n", perRow)
	file.printf("for index, item := range %s[offset:end] {\n", items)
	file.printf("if index > 0 {\nvalues.WriteString(%s)\n}\n", strconv.Quote(argumentSeparator))
	file.printf("values.WriteString(%s)\n", rowSource(file, row))
	if perRow > 0 {
		arguments := make([]string, 0, perRow)
		for _, parameter := range row.Arguments {
			arguments = append(arguments, rendered.access("item", parameter))
		}
		file.printf("args = append(args, %s)\n", strings.Join(arguments, argumentSeparator))
	}
	file.printf(closeBlock)
	file.printf("if _, err := %s.%s.ExecContext(ctx, %s[:%d]+values.String()+%s[%d:], args...); err != nil {\nreturn err\n}\n",
		receiver, connection, naming.SQLConstant, row.Values.Start, naming.SQLConstant, row.Values.End)
	file.printf(closeBlock + "return nil\n" + closeBlock + "\n")
}

// rowSource returns the expression writing one row of a batch: its parts with each place
// between them, which in a numbered form takes the row's base number plus the place's
// slot, and in an anonymous form is the whole row.
//
// Takes file (*source) which is the query file.
// Takes row (*emit_kit.Row) which is the row template.
//
// Returns string which is the expression.
func rowSource(file *source, row *emit_kit.Row) string {
	if row.Kind != dialect.PlaceholderNumbered {
		return strconv.Quote(strings.Join(row.Parts, ""))
	}
	parts := []string{strconv.Quote(row.Parts[0])}
	for index, slot := range row.Slots {
		number := file.qualified(strconvPackage, "Itoa") + "(index*" + strconv.Itoa(len(row.Arguments)) + "+" + strconv.Itoa(slot+1) + callClose
		parts = append(parts, number, strconv.Quote(row.Parts[index+1]))
	}
	return strings.Join(parts, concatenation)
}
