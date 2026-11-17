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

package duckdb

import (
	"strings"

	"seagull.sh/seagull/internal/sqlmodel"
)

// KeywordValue types the value a bare keyword such as CURRENT_DATE gives where its
// query's FROM clause has no column of its name, as the zero-argument function of that
// name returns it.
//
// Takes name (string) which is the name, in any case.
//
// Returns sqlmodel.SQLType which is the value's type.
// Returns bool which is false for a name that is no such keyword.
func (engine *Dialect) KeywordValue(name string) (sqlmodel.SQLType, bool) {
	if _, isKeyword := implicitFunctionIdentifiers[strings.ToUpper(name)]; !isKeyword || engine.functions == nil {
		return sqlmodel.SQLType{}, false
	}
	for _, function := range engine.functions.Functions[strings.ToLower(name)] {
		if len(function.Arguments) == 0 {
			return function.ReturnType, true
		}
	}
	return sqlmodel.SQLType{}, false
}
