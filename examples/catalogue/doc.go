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

// Package catalogue reads and checks the examples curriculum: concepts.yaml, which
// numbers and titles every concept and records what each engine does with it, and the
// example.yaml in each variant directory.
//
// Validate is what CI runs. It fails when the curriculum and the directories disagree: a
// variant cell without a directory, a directory no cell names, an sgl.yaml whose dialect
// or driver does not match its column, an entry query that is not in its file, or a
// hand-written Go file without the licence header. The website reads the same files,
// through its own copy, to build the examples pages.
//
// Two checks need an sgl binary, so they are commands that hack/generate/examples.sh
// --validate runs rather than tests: CheckTwins, behind cmd/twins, proves every
// equivalent cell generates its twin's code, and CheckDiagnostics, behind cmd/vetcheck,
// proves sgl vet reports exactly each variant's expected_diagnostics.
package catalogue
