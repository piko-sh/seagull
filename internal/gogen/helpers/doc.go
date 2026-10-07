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

// Package helpers renders the fixed helper files of generated code, such as the bind
// limit, the slice splicer and the runtime builder's allow-lists, from real Go sources.
//
// Each helper is an ordinary Go file in a source package that compiles, is vetted and has
// unit tests of its behaviour. Its owner embeds it and renders it with File, which
// specialises the source for one generated package: it names the canonical sgl helpers,
// errSgl sentinels, ErrSgl error values and "sgl: " messages after the package's
// vocabulary, sets the package name, and evaluates the source's directives:
//
//	//sgl:when <flag> ...   keeps the declaration below only when every flag holds
//	                        (a flag written !name holds when name does not).
//	//sgl:data <key>        replaces the literal the declaration below holds with a value.
//	//sgl:inline <key>      removes the declaration below and writes its value wherever
//	                        it is used; an if statement on an inlined condition keeps only
//	                        the branch taken, and a range over an inlined list is unrolled.
//
// A directive is written in the doc comment of a top-level declaration, or of a spec of a
// grouped const, var or type declaration, where it acts on that spec alone; a grouped
// declaration left with no spec is removed. A const block that counts with iota or
// repeats its values takes no directive on its specs, since removing one would change the
// values after it.
//
// The source's licence header, package comment and directive and nolint lines are left
// out, and every other line keeps its layout, so the rendered file reads like the source,
// except that top-level declarations follow go/printer's layout without positions: a
// blank line before a declaration with a doc comment or of another kind than the one
// before it, and none otherwise.
package helpers
