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

// Package gogen holds what the code generator's parts share: the Driver an emitter
// describes its driver API with and the optional interfaces it implements for what data
// cannot say, the Emission builders read, the Vocabulary generated names are written
// with, and how a column is read.
//
// The generator itself lives in its subpackages: generate composes a package's files from
// models, methods and dynamic, and helpers renders the fixed helper files from real Go
// sources.
package gogen
