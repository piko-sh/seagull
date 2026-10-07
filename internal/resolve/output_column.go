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

package resolve

import "seagull.sh/seagull/internal/sqlmodel"

// ArgumentTypesMatch checks whether two function argument lists have the same types by
// comparing category and engine name.
//
// Takes left ([]sqlmodel.FunctionArgument) which holds the first argument list.
// Takes right ([]sqlmodel.FunctionArgument) which holds the second argument list.
//
// Returns bool which is true if the argument lists have the same length and matching
// types.
func ArgumentTypesMatch(left []sqlmodel.FunctionArgument, right []sqlmodel.FunctionArgument) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Type.Category != right[index].Type.Category {
			return false
		}
		if left[index].Type.EngineName != right[index].Type.EngineName {
			return false
		}
	}
	return true
}

// NewOutputColumn returns an output column of the given name, type, and nullability, with
// no source, embed, or Go type attribution.
//
// Takes name (string) which is the column name or alias.
// Takes sqlType (sqlmodel.SQLType) which is the resolved SQL type.
// Takes nullable (bool) which reports whether the column can be NULL.
//
// Returns sqlmodel.OutputColumn which carries only the name, type, and nullability.
func NewOutputColumn(name string, sqlType sqlmodel.SQLType, nullable bool) sqlmodel.OutputColumn {
	return sqlmodel.OutputColumn{
		GoTypeOverride:  nil,
		StructTags:      nil,
		Name:            name,
		SourceTable:     "",
		SourceSchema:    "",
		SourceColumn:    "",
		SourceQualifier: "",
		EmbedTable:      "",
		EmbedName:       "",
		SQLType:         sqlType,
		Nullable:        nullable,
		IsEmbedded:      false,
		EmbedIsOuter:    false,
	}
}
