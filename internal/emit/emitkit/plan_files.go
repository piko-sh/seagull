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

package emitkit

import (
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// querySourceExtension ends a query source file's name.
	querySourceExtension = ".sql"

	// queryFileExtension ends the name of a generated file holding queries.
	queryFileExtension = ".sql.go"
)

// fileScope is a set of file names taken in one directory, compared without regard to
// case so that no two generated files collide on a file system that folds case.
type fileScope struct {
	// taken holds the lower-cased names already taken.
	taken map[string]struct{}
}

// newFileScope returns a file scope in which the given names are already taken.
//
// Takes names (...string) which are the file names taken from the start.
//
// Returns *fileScope which is the scope.
func newFileScope(names ...string) *fileScope {
	scope := &fileScope{taken: make(map[string]struct{}, len(names))}
	for _, name := range names {
		scope.take(name)
	}
	return scope
}

// take marks a file name taken.
//
// Takes name (string) which is the file name.
//
// Returns bool which is true when the name was free.
func (s *fileScope) take(name string) bool {
	key := strings.ToLower(name)
	if _, taken := s.taken[key]; taken {
		return false
	}
	s.taken[key] = struct{}{}
	return true
}

// planFiles names the generated file of every query source file, by source file name: the
// source's stem made safe by sanitiseStem, numbered when an earlier file or a reserved
// one already has the name, and given the .sql.go extension.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries.
// Takes scope (*fileScope) which holds the file names already taken.
//
// Returns map[string]string which maps each query source file to its generated file.
func planFiles(queries []*sqlmodel.AnalysedQuery, scope *fileScope) map[string]string {
	distinct := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		distinct[query.Filename] = struct{}{}
	}
	sources := slices.Sorted(maps.Keys(distinct))
	files := make(map[string]string, len(sources))
	for _, source := range sources {
		directory, base := path.Split(source)
		stem := strings.TrimSuffix(base, querySourceExtension)
		candidate := sanitiseStem(stem)
		for suffix := 2; !scope.take(directory + candidate + queryFileExtension); suffix++ {
			candidate = sanitiseStem(stem + strconv.Itoa(suffix))
		}
		files[source] = directory + candidate + queryFileExtension
	}
	return files
}
