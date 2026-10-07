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

// Package ast holds the vocabulary a dialect speaks to the analyser: parsed statements,
// the raw analysis of a query, typed expression trees and catalogue mutations.
//
// A dialect produces these values from its own grammar and the analyser consumes them,
// resolving names and types against the catalogue to produce the sqlmodel types that
// emitters and embedders read. Nothing outside the dialect contract and the analyser
// needs ast: an emitter or an embedder sees only sqlmodel.
//
// Children, RewriteChildren, Walk and Preorder are the one place that knows which
// expressions each node holds, so a walk built on them reaches every node kind, including
// kinds added later. They never enter a subquery; InnerQuery gives the query an EXISTS or
// a scalar subquery holds.
//
// The package depends only on sqlmodel, for the types both sides share, such as SQLType
// and the catalogue's Column and Function.
package ast
