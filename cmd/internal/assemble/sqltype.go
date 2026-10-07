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

package assemble

import (
	"strings"

	"seagull.sh/seagull/sdk/dialect"
)

// unknownTypeName names a SQL type that carries no engine name.
const unknownTypeName = "unknown"

// SQLTypeName names a SQL type for printing: its engine name or, for an array the engine
// does not name, its element's name followed by [] for each level. Nesting past
// dialect.MaxTypeDepth levels is not followed.
//
// Takes sqlType (dialect.SQLType) which is named.
//
// Returns string which is the name, such as integer or text[], or unknown when no level
// carries an engine name.
func SQLTypeName(sqlType dialect.SQLType) string {
	current := &sqlType
	for depth := range dialect.MaxTypeDepth {
		if current.EngineName != "" {
			return current.EngineName + strings.Repeat("[]", depth)
		}
		if current.ElementType == nil {
			return unknownTypeName + strings.Repeat("[]", depth)
		}
		current = current.ElementType
	}
	return unknownTypeName + strings.Repeat("[]", dialect.MaxTypeDepth)
}
