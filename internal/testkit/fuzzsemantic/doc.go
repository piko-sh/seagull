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

// Package fuzzsemantic models semantic relations between generated query methods.
//
// A version 19 input chooses the rows of two small tables, a predicate, its parameter, a
// pivot row and a page size. The helper calls the generated methods of a fixed catalogue
// of queries, and the package checks relations between their results that hold whatever
// the right answer is. The partitions of a predicate add up to the whole table,
// aggregates combine over those partitions, window functions number and total each
// partition, joins and semi-joins agree with their other formulations, compounds agree
// with their branches, pages concatenate to the ordered result, and a builder's condition
// on a query that limits its own rows keeps the rows it returns. Only the pivot check
// consults an independent evaluation of the predicate, never Seagull's own analysis. A
// query the engine refuses is reported as a native error, apart from a relation that
// fails.
package fuzzsemantic
