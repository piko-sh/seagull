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

package session

import (
	"seagull.sh/seagull/cmd/internal/starter"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

// Starter returns the files sgl init writes for a dialect, keyed by path, so a blank
// playground starts where a new sgl project does.
//
// Takes dialectName (string) which names a built-in dialect.
//
// Returns map[string]string which maps each starter file's path to its content.
// Returns error when the dialect is unknown or its family has no starter files.
func Starter(dialectName string) (map[string]string, error) {
	built, err := dialect_registry.New(dialectName, dialect_registry.Options{})
	if err != nil {
		return nil, err
	}
	files, err := starter.Files(dialectName, built.Info().Family)
	if err != nil {
		return nil, err
	}
	contents := make(map[string]string, len(files))
	for _, file := range files {
		contents[file.Path] = file.Content
	}
	return contents, nil
}
