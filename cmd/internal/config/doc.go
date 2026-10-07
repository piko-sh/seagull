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

// Package config reads and validates sgl.yaml, the configuration file for the sgl
// command.
//
// The file is decoded strictly: unknown keys are errors, with a suggestion when a key is
// close to a known one. Files larger than [MaxFileBytes] are refused.
//
// ${env:NAME} references are expanded only in connection strings, so secrets never need
// to live in the file. [Load] expands them from the process environment and leaves a
// reference to a variable that is not set as written, which [RequireExpanded] reports
// where the connection string is used; [Parse] and [ReadFS] read nothing from the process
// and leave them as written until [Config.ExpandEnvironment] is called, so a tool can
// validate a file it did not write without exposing its own environment.
package config
