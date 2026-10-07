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

// Package source holds Go helper templates used during code generation.
//
// The files provide the real Go source of the helper both ClickHouse emitters add to a
// generated package: sglClickHouseValue, which writes a value bound to a {name:Type}
// parameter as the text ClickHouse parses for the parameter's type, from a shape the
// generated code passes; and of the native emitter's sglAppendNativeRow, which checks the
// IP addresses of a row a columnar batch appends against their types and converts a value
// holding a Map into the only forms the driver's Map columns append, reading the row's
// shape with the value helper's parser.
//
// The files compile and are tested here as they are; the chvalues package embeds them and
// renders each into a generated package with internal/gogen/helpers.
package source
