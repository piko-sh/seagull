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

// Package directive parses and validates the SQL comment directives that annotate
// queries.
//
// The parser turns a query block's comment header and parameter bindings into a
// DirectiveBlock: the query name and command, parameter directives, metadata, embeds and
// column overrides. Each call is validated against the role-keyed directive specification
// (which directives exist, where they may appear, and which positional and keyword
// arguments they take), and every problem becomes a positioned diagnostic. The lexer, the
// call-argument parser and the argument checks it builds on live in the syntax
// subpackage, with the specifications of the column and migration directives, which a
// migration file holds too.
package directive
