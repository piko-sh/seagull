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

// Package source holds the real Go sources of the fixed helpers generated packages share,
// whatever their driver: the bind limit, the slice expander, the runtime builder's
// helpers, the inline JSON type, the query-names lookup, the sort direction type and the
// conversion of enum slices to and from Go's own strings.
//
// Each file compiles and is tested here as it is; internal/gogen/helpers embeds it and
// renders it into a generated package, naming its canonical sgl identifiers after the
// package's vocabulary and evaluating its //sgl: directives.
package source
