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
)

// writeGrouped writes the body of a grouped command: many method: it scans each row into
// locals, starts a group the first time it meets the row's key, and collects each detail
// embed's columns into the group's slice, returning the groups in the order their keys
// first appear.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
// Takes run (statement) which says how to pass the SQL and the arguments.
func (current *generation) writeGrouped(file *source, rendered *query, run statement) {
	failure := failureCode(rendered)
	naming := rendered.naming
	columns := rendered.analysed.OutputColumns
	key := groupKeyColumn(rendered.analysed)
	keyType := file.typeOf(columnType(current.request, &columns[key]))
	file.printf("rows, err := "+queryCall+"\n", run.caller, connection, run.query, run.arguments)
	file.printf(failWithError, failure)
	file.printf(closeRows)
	file.printf("groups := make(map[%s]*%s)\nvar ordered %s%s\n", keyType, naming.Row, slicePrefix, keyType)
	file.printf(openRowLoop)
	targets := make([]string, 0, len(columns))
	for index := range columns {
		file.printf("var %s %s\n", naming.ColumnLocals[index], file.typeOf(columnType(current.request, &columns[index])))
		targets = append(targets, referenceOpen+naming.ColumnLocals[index])
	}
	file.printf(failScan, strings.Join(targets, argumentSeparator), failure)
	keyLocal := naming.ColumnLocals[key]
	file.printf("group, found := groups[%s]\nif !found {\ngroup = &%s{}\n", keyLocal, naming.Row)
	for index := range columns {
		if field := groupField(rendered, index); field != "" {
			file.printf("group.%s = %s\n", field, naming.ColumnLocals[index])
		}
	}
	file.printf("groups[%s] = group\nordered = append(ordered, %s)\n%s", keyLocal, keyLocal, closeBlock)
	for _, embed := range naming.Embeds {
		if detailEmbed(rendered.analysed, embed.Group) {
			file.printf("group.%s = append(group.%s, %s{%s})\n", embed.Field, embed.Field, embed.Type, detailFields(rendered, embed.Group))
		}
	}
	file.printf(closeBlock)
	file.printf(failRows, failure)
	file.printf("results := make(%s%s, 0, len(ordered))\n", slicePrefix, naming.Row)
	file.printf("for _, key := range ordered {\nresults = append(results, *groups[key])\n}\nreturn results, nil\n")
}

// groupField returns the field of a group a column fills when the group starts: a field
// of the row, or of an embed the group holds once, or nothing for a column of a detail
// embed, which each row adds to.
//
// Takes rendered (*query) which is the query.
// Takes index (int) which is the column's index.
//
// Returns string which is the field, or empty.
func groupField(rendered *query, index int) string {
	column := &rendered.analysed.OutputColumns[index]
	field := rendered.naming.ColumnFields[index]
	if !column.IsEmbedded {
		return field
	}
	group := embedGroup(column)
	if detailEmbed(rendered.analysed, group) {
		return ""
	}
	return embedField(rendered, group) + fieldSeparator + field
}

// detailFields returns the fields of a detail embed's struct, each set from its column's
// local.
//
// Takes rendered (*query) which is the query.
// Takes group (string) which is the embed group.
//
// Returns string which is the comma-separated field values.
func detailFields(rendered *query, group string) string {
	columns := rendered.analysed.OutputColumns
	var fields []string
	for index := range columns {
		if columns[index].IsEmbedded && embedGroup(&columns[index]) == group {
			fields = append(fields, rendered.naming.ColumnFields[index]+": "+rendered.naming.ColumnLocals[index])
		}
	}
	return strings.Join(fields, argumentSeparator)
}
