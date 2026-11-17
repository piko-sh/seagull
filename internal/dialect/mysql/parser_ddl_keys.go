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

package mysql

import (
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

// attachDeclaredIndexes adds the FULLTEXT and other non-unique indexes the statement
// declared to its mutation, as index mutations applied after it, and clears the queue.
//
// Takes mutation (*ast.CatalogueMutation) which is the statement's mutation.
func (p *parser) attachDeclaredIndexes(mutation *ast.CatalogueMutation) {
	for _, index := range p.declaredIndexes {
		created := ast.NewCatalogueMutation(
			ast.MutationCreateIndex,
			mutation.SchemaName,
			mutation.TableName,
			ast.WithIndexDefinition(index.name, index.columns, false),
			ast.WithIndexScope(ast.IndexNamesInTable),
		)
		if index.fullText {
			ast.WithFullTextIndex()(created)
		}
		mutation.AdditionalMutations = append(mutation.AdditionalMutations, created)
	}
	p.declaredIndexes = nil
}

// nameKey records the name of a key or index a CREATE TABLE declares, naming an unnamed
// one after its first column as MySQL does, numbered from _2 when an earlier key of the
// statement has that name.
//
// Takes name (string) which is the declared name, or empty.
// Takes columns ([]string) which are the key's columns.
//
// Returns string which is the key's name, or name unchanged outside a CREATE TABLE.
func (p *parser) nameKey(name string, columns []string) string {
	if p.keyNames == nil {
		return name
	}
	taken := func(candidate string) bool {
		return strings.EqualFold(candidate, primaryKeyName) ||
			slices.ContainsFunc(*p.keyNames, func(existing string) bool { return strings.EqualFold(existing, candidate) })
	}
	if name == "" && len(columns) > 0 {
		name = columns[0]
		for number := 2; taken(name) && number < maxKeyNameSuffixes; number++ {
			name = columns[0] + "_" + strconv.Itoa(number)
		}
	}
	*p.keyNames = append(*p.keyNames, name)
	return name
}
