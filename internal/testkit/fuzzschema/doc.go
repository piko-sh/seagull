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

// Package fuzzschema models version 18 migration schema histories.
//
// A version 18 input is a history of one to six migrations over a few tables. Its
// operations add, drop, rename and retype columns, set and drop defaults and NOT NULL,
// add and drop CHECK, UNIQUE, PRIMARY KEY and FOREIGN KEY constraints, drop and recreate
// and rename tables, and create sequences, views, materialised views, generated columns,
// enums, partitions and tables in a second schema, over integer, decimal, text, temporal,
// Boolean, UUID, JSON, array and binary columns. Each history is written only in SQL its
// engine accepts, and the package predicts the final schema itself, without Seagull's
// parser or catalogue. The prediction holds the tables and views, their columns in order
// with a coarse type family, nullability, default and generation, and their constraints.
package fuzzschema
