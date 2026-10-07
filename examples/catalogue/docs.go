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

package catalogue

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// docsDirectory is where Seagull's documentation lives, relative to the examples
// directory.
const docsDirectory = "../docs"

// checkDocs checks that every concept's docs references name a page under docs/ and, when
// they carry an anchor, a heading on that page. The website links each concept to these
// pages, so a renamed page or heading must fail here rather than break a link.
//
// Takes examplesDirectory (string) which is the examples directory.
//
// Returns []error which lists the problems found.
func (c *Curriculum) checkDocs(examplesDirectory string) []error {
	root, err := os.OpenRoot(filepath.Join(examplesDirectory, docsDirectory))
	if err != nil {
		return nil
	}
	defer func() { _ = root.Close() }()
	anchors := make(map[string]map[string]bool)
	var problems []error
	for index := range c.Concepts {
		concept := &c.Concepts[index]
		for _, reference := range concept.Docs {
			page, anchor, _ := strings.Cut(reference, "#")
			known, read := anchors[page]
			if !read {
				content, err := root.ReadFile(filepath.FromSlash(page) + ".md")
				if err != nil {
					problems = append(problems, fmt.Errorf("concept %d %s: docs reference %q names no page docs/%s.md", concept.Number, concept.Slug, reference, page))
					anchors[page] = nil
					continue
				}
				known = headingAnchors(string(content))
				anchors[page] = known
			}
			if known != nil && anchor != "" && !known[anchor] {
				problems = append(problems, fmt.Errorf("concept %d %s: docs reference %q names no heading on docs/%s.md", concept.Number, concept.Slug, reference, page))
			}
		}
	}
	return problems
}

// headingAnchors returns the anchors a Markdown page's headings get, the way GitHub and
// goldmark make them, ignoring lines inside fenced code blocks.
//
// Takes markdown (string) which is the page.
//
// Returns map[string]bool which holds every anchor.
func headingAnchors(markdown string) map[string]bool {
	anchors := make(map[string]bool)
	fenced := false
	for line := range strings.SplitSeq(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced || !strings.HasPrefix(trimmed, "#") {
			continue
		}
		text := strings.TrimLeft(trimmed, "#")
		if text == "" || !strings.HasPrefix(text, " ") {
			continue
		}
		anchors[anchorOf(strings.TrimSpace(text))] = true
	}
	return anchors
}

// anchorOf turns heading text into its anchor: lower case, spaces as hyphens, and every
// character other than a letter, digit, hyphen or underscore dropped.
//
// Takes heading (string) which is the heading's text.
//
// Returns string which is the anchor.
func anchorOf(heading string) string {
	anchor := make([]rune, 0, len(heading))
	for _, character := range strings.ToLower(heading) {
		switch {
		case character == ' ':
			anchor = append(anchor, '-')
		case character == '-' || character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character):
			anchor = append(anchor, character)
		}
	}
	return string(anchor)
}
