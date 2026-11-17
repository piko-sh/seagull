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

package fuzzbuilder

import (
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// RegressionMarker ends a saved reproduction of a finding. No finding is open, so the
// decoder only removes it, and an input reads the same with or without it.
const RegressionMarker = "\x00sgl-regression"

// statusExcluded marks an excluded capability.
const statusExcluded = "excluded"

// Capabilities lists the modes an engine's inputs leave out.
//
// Takes engine (string) which names the dialect.
//
// Returns []fuzzcase.Capability which records each exclusion with its reason.
func Capabilities(engine string) []fuzzcase.Capability {
	var result []fuzzcase.Capability
	if !ModeSupported(engine, ModePrepared) {
		result = append(result, capability("mode/prepared", "The ClickHouse database/sql driver prepares INSERT batches, not general queries."))
	}
	if !ModeSupported(engine, ModeNative) {
		result = append(result, capability("mode/native", "This engine has no dedicated generated driver API."))
	}
	return result
}

// capability builds one exclusion record.
//
// Takes feature (string) which names the excluded feature.
// Takes reason (string) which explains it.
//
// Returns fuzzcase.Capability which records the exclusion.
func capability(feature, reason string) fuzzcase.Capability {
	return fuzzcase.Capability{Feature: feature, NativeType: "", CastType: "", Status: statusExcluded, Reason: reason}
}
