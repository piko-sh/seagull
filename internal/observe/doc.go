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

// Package observe lets a host watch the stages of a generation, migration or seed run.
//
// Seagull carries no telemetry dependency of its own. Each stage it runs calls the
// ObserverPort attached to the context, which a host can back with tracing spans, metrics
// or timing output. Without an attached ObserverPort every call goes to Discard(), which
// does nothing. A panic in an attached ObserverPort is recovered and logged once at
// warning level, with its stack, through the logger attached to the context, and the
// stage runs on unobserved, so telemetry never fails the run it observes.
package observe
