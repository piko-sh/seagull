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

// Package assemble turns one database of an sgl.yaml into what the generator needs: the
// dialect, the emitter, the preset, the type overrides and the custom functions. The sgl
// command and the in-browser playground both build their generators here, so a
// configuration means the same thing to each.
//
// It also reads generated code back: a [GoIndex] finds a query's method, the types it
// uses and the SQL it runs. A [StageTimer] sums the time a generation spends in each
// stage, for sgl generate --timings and the playground.
//
// The package links no database driver. Its errors are plain errors that name the
// database and the setting at fault; the sgl command reports them as usage errors.
package assemble
