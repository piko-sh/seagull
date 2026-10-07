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

// Package analyse turns one query block into a fully typed analysed query.
//
// QueryAnalyser parses the block's directives and SQL, asks the dialect for a raw
// analysis, builds the query's scope chain, resolves the types of its output columns and
// parameters, propagates nullability through joins and directives, applies column
// overrides and embeds, runs the diagnostic passes, and assembles the result the emitters
// consume.
package analyse
