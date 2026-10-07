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

// Package sqlfile discovers and reads the SQL files Seagull works from: migrations, seeds
// and query files.
//
// Migration files are listed once, as versioned up and down files in version order, and
// both the catalogue replay and the migration run time read that one listing, so they
// select and order the same files; a caller may read each listed file when it needs it.
// File names that do not follow the versioned naming scheme are reported rather than
// silently skipped. Seed files are read in version order. Query files are listed by name
// and split into one block per query under the dialect's lexical rules. Every read goes
// through a FileReader, which can share a byte budget with other readers.
package sqlfile
