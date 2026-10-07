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

package gotypes

import (
	"strings"

	"seagull.sh/seagull/internal/sqlmodel"
)

// OverrideTooDeep reports whether a type override names an array of more dimensions than
// the mapper follows, so the dimensions past sqlmodel.MaxArrayDimensions map to an
// unknown type.
//
// Takes override (sqlmodel.TypeOverride) which is the override.
//
// Returns bool which is true when the override's SQL type name is too deep.
func OverrideTooDeep(override sqlmodel.TypeOverride) bool {
	return strings.Count(override.SQLTypeName, "[]") > sqlmodel.MaxArrayDimensions
}

// TypeTooDeep reports whether a SQL type nests element, key, field or member types more
// deeply than the structural mapper follows, so the part past the limit maps to any. It
// applies sqlmodel.TypeTooDeep.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
//
// Returns bool which is true when the type is too deep.
func TypeTooDeep(sqlType *sqlmodel.SQLType) bool {
	return sqlmodel.TypeTooDeep(sqlType)
}
