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

// Command inspect writes, at build time, the JSON the playground computes in the browser,
// so seagull.sh can render examples, starters and the diagnostic reference without
// running WebAssembly. It shares every line of that work with the playground through the
// session package.
//
// Usage, with exactly one mode:
//
//	inspect -batch DIR [-out DIR] [-timings]
//	inspect -starters DIR [-timings]
//	inspect -codes [-out FILE]
//	inspect -dialects [-out FILE]
//
// -batch walks DIR for project directories, those holding sgl.yaml (or another name sgl
// reads) and a db directory, and writes each one's session response as inspect.json in
// that directory, or with -out in the same place under a mirror tree rooted at -out. The
// walk does not descend into a project or into hidden directories. It exits with status 1
// when a project's run did not complete; error diagnostics are content, not failures.
//
// -starters writes DIALECT.json into DIR for every dialect: the files sgl init writes and
// the response the playground gives for them.
//
// -codes prints every diagnostic code with its title, severity and Markdown explanation,
// and, where a short project reproduces it, the diagnostic as the playground presents it
// and the dialect it was reproduced with. A code no short project can raise carries the
// reason instead.
//
// -dialects prints the dialects, their placeholders and the drivers each accepts.
//
// Timings vary between runs, so -batch and -starters leave them out unless -timings is
// given, which keeps the output stable enough to commit.
package main
