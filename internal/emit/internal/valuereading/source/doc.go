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

// Package source holds the Go source of the reflection readers used by the database/sql
// and native ClickHouse emitters.
//
// Both emitters add these readers to generated packages that read a driver's own values:
// a list into a slice, a map into a map and a scalar into a scalar of its kind. Each
// element passes through the reader of the driver the package uses.
//
// The file compiles and is tested here as it is; the valuereading package embeds it and
// renders it into a generated package with internal/gogen/helpers. The emitters' own
// helper sources call it through stand-ins that read as it does here.
package source
