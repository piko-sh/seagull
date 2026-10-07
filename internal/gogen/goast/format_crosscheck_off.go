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

//go:build !formatcheck

package goast

import (
	"go/ast"
	"go/token"
)

// crossCheckFormat returns formatted unchanged; the formatcheck build tag swaps in a
// version that compares it with goimports.
//
// Takes formatted ([]byte) which is FormatSource's output.
//
// Returns []byte which is formatted, unchanged.
// Returns error which is always nil.
func crossCheckFormat(_ *token.FileSet, _ *ast.File, _ []byte, formatted []byte) ([]byte, error) {
	return formatted, nil
}

// crossCheckTextFormat leaves canonical source unchanged without formatcheck.
//
// Takes formatted ([]byte) which is the formatter output to validate.
//
// Returns []byte which is the formatted source.
// Returns error when the enabled goimports cross-check reports a mismatch.
func crossCheckTextFormat(_ []byte, formatted []byte) ([]byte, error) { return formatted, nil }
