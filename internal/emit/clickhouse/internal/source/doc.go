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
// The files provide the real Go source of the helper the native ClickHouse emitter adds
// to a generated package: sglScanNativeComposite, the converting scan adapter that reads
// a Map or Array column holding named tuples, which the native clickhouse-go driver hands
// a sql.Scanner in its own forms, into the generated slices, maps and structs. The
// conversion of the rows a columnar batch appends lives beside the value helper, in
// internal/emit/internal/chvalues/source, whose shape parser it shares.
//
// The file compiles and is tested here as it is; the clickhouse emitter embeds it and
// renders it into a generated package with internal/gogen/helpers.
package source
