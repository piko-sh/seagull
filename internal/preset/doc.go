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

// Package preset holds the form of a preset the generation stages consume: the directive
// prefixes, the generated-file header, the helper and error prefixes, the type slots and
// the optional query-names file, with the defaults and the rules that decide whether they
// can be used.
//
// The public Preset is defined in the app package, which converts it to Settings; this
// package never names it.
package preset
