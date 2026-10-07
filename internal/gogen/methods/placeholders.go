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

package methods

import (
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
)

// QuoteIdentifier delimits an identifier as the emission's dialect does.
//
// Takes emission (*gogen.Emission) which supplies the dialect.
// Takes name (string) which is the raw identifier.
//
// Returns string which is the delimited identifier.
func QuoteIdentifier(emission *gogen.Emission, name string) string {
	return emission.Dialect().IdentifierQuote.Quote(name)
}

// MaxBindVariables returns the most parameters one statement of the emission's dialect
// binds, or the conservative default when the dialect sets no limit.
//
// Takes emission (*gogen.Emission) which supplies the dialect.
//
// Returns int which is the limit.
func MaxBindVariables(emission *gogen.Emission) int {
	if limit := emission.Dialect().MaxParameters; limit > 0 {
		return limit
	}
	return goast.DefaultMaxBindVariablesFallback
}
