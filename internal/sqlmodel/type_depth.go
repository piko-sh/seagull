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

package sqlmodel

const (
	// MaxTypeDepth is how deeply Seagull follows the element, key, field and member types
	// nested in a SQL type.
	//
	// The root is at depth 0, and each nested type adds one level. Traversal stops at
	// MaxTypeDepth to keep deeply nested types from exhausting the stack.
	MaxTypeDepth = 64

	// MaxGoTypeArgumentDepth caps how deeply the type arguments of a go_type override may
	// nest, so a hostile override cannot drive parsing and the recursion over the Go type it
	// names without bound.
	MaxGoTypeArgumentDepth = 16
)

// TypeTooDeep reports whether a SQL type nests element, key, field or member types to
// MaxTypeDepth or beyond, so a walk bounded by MaxTypeDepth stops before reaching the
// innermost part.
//
// Takes sqlType (*SQLType) which is the type; nil is never too deep.
//
// Returns bool which is true when a nested type sits at depth MaxTypeDepth or deeper.
func TypeTooDeep(sqlType *SQLType) bool {
	return typeDepthReaches(sqlType, 0)
}

// typeDepthReaches reports whether a type, or a type nested in it, sits at MaxTypeDepth
// or deeper.
//
// Takes sqlType (*SQLType) which is the type; nil reaches nothing.
// Takes depth (int) which is the type's own depth.
//
// Returns bool which is true when the type or a nested type sits at or past the limit.
func typeDepthReaches(sqlType *SQLType, depth int) bool {
	if sqlType == nil {
		return false
	}
	if depth >= MaxTypeDepth {
		return true
	}
	if typeDepthReaches(sqlType.ElementType, depth+1) || typeDepthReaches(sqlType.KeyType, depth+1) {
		return true
	}
	for index := range sqlType.StructFields {
		if typeDepthReaches(&sqlType.StructFields[index].SQLType, depth+1) {
			return true
		}
	}
	for index := range sqlType.UnionMembers {
		if typeDepthReaches(&sqlType.UnionMembers[index].SQLType, depth+1) {
			return true
		}
	}
	return false
}
