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

// Package intake admits SQL from an external test suite as fuzz seeds, recording where
// each came from.
//
// The manifest names the suite, its licence and upstream revision, its file format and
// the dialects its SQL is written for. Each admitted file is copied unchanged beside the
// seeds it yields, with its path, size and SHA-256 in the intake manifest. A seed pairs
// the schema statements read before a query with that query under a directive, as
// FuzzGenerate takes them. Links, paths leaving the source, files that are not regular,
// files over the bounds and repeated seeds are refused and counted.
package intake
