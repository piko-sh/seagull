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
	"cmp"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_kit"
)

// namedPrefix is the start of the SQL of a query whose method edits its SQL at run time,
// by which the query-names lookup names any SQL the method sends.
type namedPrefix struct {
	// source is the expression of the prefix: a slice of the query's constant.
	source string

	// label is the name the lookup reports.
	label string

	// length is the prefix's length, which orders the longest first.
	length int
}

// queryNamesFile writes the query-name lookup, including SQL prefixes for queries edited
// at runtime.
//
// Returns emit.GeneratedFile which is the file.
// Returns bool which is false when the style asks for no lookup.
// Returns error when the file does not format.
func (current *generation) queryNamesFile() (emit.GeneratedFile, bool, error) {
	names := current.request.Style.QueryNames
	if !names.Enabled {
		return emit.GeneratedFile{}, false, nil
	}
	var exact []string
	seen := make(map[string]bool)
	var prefixes []namedPrefix
	add := func(constant, sql, label string, edited int) {
		if !seen[sql] {
			seen[sql] = true
			exact = append(exact, constant+": "+strconv.Quote(label))
		}
		if edited > 0 {
			prefixes = append(prefixes, namedPrefix{source: constant + "[:" + strconv.Itoa(edited) + "]", label: label, length: edited})
		}
	}
	for _, rendered := range current.queries {
		add(rendered.naming.SQLConstant, rendered.rendered.SQL, rendered.naming.Label, firstEdit(rendered, &rendered.rendered))
		if rendered.count != nil {
			add(rendered.naming.CountConstant, rendered.count.SQL, rendered.naming.Label, firstEdit(rendered, rendered.count))
		}
	}
	slices.SortStableFunc(prefixes, func(left, right namedPrefix) int { return cmp.Compare(right.length, left.length) })
	entries := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		entries = append(entries, "{"+prefix.source+argumentSeparator+strconv.Quote(prefix.label)+"}")
	}
	file := newSource(&current.request.Naming)
	file.printf("var %s = map[string]string{%s}\n\n", current.helpers.queryNames, strings.Join(exact, argumentSeparator))
	file.printf("var %s = []struct {\nprefix string\nname string\n}{%s}\n\n", current.helpers.queryNamePrefixes, strings.Join(entries, argumentSeparator))
	file.printf("func %s(query string) string {\nif name, ok := %s[query]; ok {\nreturn name\n}\n", names.Function, current.helpers.queryNames)
	file.printf("for _, entry := range %s {\nif %s(query, entry.prefix) {\nreturn entry.name\n}\n}\nreturn \"\"\n}\n",
		current.helpers.queryNamePrefixes, file.qualified(stringsPackage, "HasPrefix"))
	generated, err := file.generated(names.File, current.request)
	return generated, true, err
}

// firstEdit returns the offset of the first edit a query's method may make to a
// rendering's SQL at run time, or zero when it never edits it.
//
// Takes rendered (*query) which is the query.
// Takes rendering (*emit_kit.Rendered) which is the rendering, the query's or its
// count's.
//
// Returns int which is the offset.
func firstEdit(rendered *query, rendering *emit_kit.Rendered) int {
	marks := &rendering.Marks
	var offsets []int
	for _, list := range marks.Lists {
		offsets = append(offsets, list.Operator.Start)
	}
	if rendered.row != nil && rendering == &rendered.rendered {
		offsets = append(offsets, rendered.row.Values.Start)
	}
	if sortableParameter(rendered.analysed) >= 0 && marks.OrderBy != nil {
		offsets = append(offsets, marks.OrderBy.Start)
	}
	if rendered.analysed.DynamicRuntime {
		offsets = append(offsets, builderOffsets(marks)...)
	}
	if len(offsets) == 0 {
		return 0
	}
	return slices.Min(offsets)
}

// builderOffsets returns the offsets a runtime builder edits a rendering at: where its
// filters, orderings and pagination go.
//
// Takes marks (*emit_kit.Marks) which are the rendering's marks.
//
// Returns []int which holds the offsets.
func builderOffsets(marks *emit_kit.Marks) []int {
	var offsets []int
	if filter := marks.Filter; filter != nil {
		offsets = append(offsets, filter.InsertAt)
		if filter.Present {
			offsets = append(offsets, filter.Condition.Start)
		}
	}
	if order := marks.OrderBy; order != nil {
		offsets = append(offsets, order.Start)
	}
	if place := marks.Pagination; place != nil {
		offsets = append(offsets, place.Start)
	}
	return offsets
}
