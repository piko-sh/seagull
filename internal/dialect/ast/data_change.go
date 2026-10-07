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

package ast

import "strings"

// DataChange is a set of the statements that change a table's rows.
//
// It names one statement, such as the kind of a parsed statement, or several, such as the
// statements a dialect accepts a RETURNING clause on. The zero value is the empty set.
type DataChange uint8

const (
	// DataChangeInsert is an INSERT, or another statement that adds rows, such as an upsert.
	DataChangeInsert DataChange = 1 << iota

	// DataChangeUpdate is an UPDATE.
	DataChangeUpdate

	// DataChangeDelete is a DELETE.
	DataChangeDelete

	// DataChangeMerge is a MERGE, which may add, change and remove rows in one statement.
	DataChangeMerge
)

var (
	// dataChangeKeywords names each statement of a DataChange set, in the order String lists
	// them.
	dataChangeKeywords = []struct {
		keyword string
		change  DataChange
	}{
		{keyword: "INSERT", change: DataChangeInsert},
		{keyword: "UPDATE", change: DataChangeUpdate},
		{keyword: "DELETE", change: DataChangeDelete},
		{keyword: "MERGE", change: DataChangeMerge},
	}
)

// Includes reports whether the set holds every statement of another set. The empty set is
// included in no set, so a statement of unknown kind is never taken as supported.
//
// Takes other (DataChange) which is the set to look for.
//
// Returns bool which is true when other is not empty and every statement of it is in the
// set.
func (change DataChange) Includes(other DataChange) bool {
	return other != 0 && change&other == other
}

// String lists the statements of the set as SQL keywords, such as "INSERT, DELETE", or
// returns "none" for the empty set.
//
// Returns string which is the list.
func (change DataChange) String() string {
	keywords := make([]string, 0, len(dataChangeKeywords))
	for _, entry := range dataChangeKeywords {
		if change&entry.change != 0 {
			keywords = append(keywords, entry.keyword)
		}
	}
	if len(keywords) == 0 {
		return "none"
	}
	return strings.Join(keywords, ", ")
}
