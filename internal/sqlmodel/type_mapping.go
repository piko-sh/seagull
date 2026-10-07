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

// GoTypeResolver maps a SQL type to the Go type generated code uses for it.
type GoTypeResolver interface {
	// Resolve returns the Go type for a value of sqlType, for a NOT NULL value or a nullable
	// one. A type it does not cover resolves to any.
	//
	// Takes sqlType (SQLType) which is the SQL type to map.
	// Takes nullable (bool) which selects the nullable Go type.
	//
	// Returns GoType which is the Go type.
	Resolve(sqlType SQLType, nullable bool) GoType
}
