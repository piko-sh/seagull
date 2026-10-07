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

//go:build formatcheck

package goast

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/imports"
	"piko.sh/goastutil/goimports"
)

// crossCheckFormat compares FormatSource's output with goimports, and fails when they
// differ. It runs only under the formatcheck build tag, so goimports and its package
// loader stay out of every other build.
//
// Two references are checked. The format-only goimports pass over the same printed source
// checks the import regrouping and gofmt steps. The full goimports pass checks the import
// tracker added no unused import and missed none.
//
// Takes fileSet (*token.FileSet) which holds the declaration positions.
// Takes file (*ast.File) which is the file that was formatted.
// Takes printed ([]byte) which is the printed source FormatSource formatted.
// Takes formatted ([]byte) which is FormatSource's output.
//
// Returns []byte which is formatted, unchanged.
// Returns error when either goimports pass produces different bytes.
func crossCheckFormat(fileSet *token.FileSet, file *ast.File, printed []byte, formatted []byte) ([]byte, error) {
	formatOnly, err := imports.Process("", printed, &imports.Options{
		Fragment:   false,
		AllErrors:  false,
		Comments:   true,
		TabIndent:  true,
		TabWidth:   8,
		FormatOnly: true,
	})
	if err != nil {
		return nil, fmt.Errorf("formatcheck: format-only goimports pass failed: %w", err)
	}
	if !bytes.Equal(formatOnly, formatted) {
		return nil, fmt.Errorf("formatcheck: output differs from format-only goimports output:\n--- goimports\n%s\n--- formatted\n%s", formatOnly, formatted)
	}

	reference, err := goimports.FormatAST(fileSet, file)
	if err != nil {
		return nil, fmt.Errorf("formatcheck: full goimports pass failed: %w", err)
	}
	if !bytes.Equal(reference, formatted) {
		return nil, fmt.Errorf("formatcheck: format-only output differs from full goimports output:\n--- full\n%s\n--- format-only\n%s", reference, formatted)
	}
	return formatted, nil
}

// crossCheckTextFormat compares the direct source path against both goimports modes.
//
// Takes source ([]byte) which is the source before import grouping and formatting.
// Takes formatted ([]byte) which is the formatter output to validate.
//
// Returns []byte which is the formatted source.
// Returns error when the enabled goimports cross-check reports a mismatch.
func crossCheckTextFormat(source, formatted []byte) ([]byte, error) {
	for _, formatOnly := range []bool{true, false} {
		reference, err := imports.Process("", source, &imports.Options{Comments: true, TabIndent: true, TabWidth: 8, FormatOnly: formatOnly})
		if err != nil {
			return nil, fmt.Errorf("formatcheck: source goimports pass: %w", err)
		}
		if !bytes.Equal(reference, formatted) {
			return nil, fmt.Errorf("formatcheck: source output differs from goimports (format-only %t):\n--- goimports\n%s\n--- formatted\n%s", formatOnly, reference, formatted)
		}
	}
	return formatted, nil
}
