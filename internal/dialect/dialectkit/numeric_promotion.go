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

package dialectkit

import "seagull.sh/seagull/internal/sqlmodel"

// IsNumericCategory reports whether a type is an integer, a binary float or an exact
// decimal.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns bool which is true for a numeric type.
func IsNumericCategory(sqlType sqlmodel.SQLType) bool {
	switch sqlType.Category {
	case sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal:
		return true
	default:
		return false
	}
}

// PromoteAcrossNumericCategories returns the type two numbers of different categories
// meet in, as in a UNION or CASE. A float beats a decimal and a decimal beats an integer,
// and the winner keeps its own type.
//
// Takes left (sqlmodel.SQLType) which is one operand's type.
// Takes right (sqlmodel.SQLType) which is the other's.
//
// Returns sqlmodel.SQLType which is the type they meet in.
// Returns bool which is false unless both are numbers of different categories.
func PromoteAcrossNumericCategories(left, right sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	if !IsNumericCategory(left) || !IsNumericCategory(right) || left.Category == right.Category {
		return sqlmodel.SQLType{}, false
	}
	for _, winner := range []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal} {
		if left.Category == winner {
			return left, true
		}
		if right.Category == winner {
			return right, true
		}
	}
	return left, true
}
