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

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"seagull.sh/seagull"
)

// formatMarkdown renders explanations as Markdown.
const formatMarkdown = "markdown"

// runExplain explains a diagnostic code, or lists every code.
//
// Takes arguments ([]string) which holds the flags and an optional code.
//
// Returns int which is the exit status.
func (inv *invocation) runExplain(_ context.Context, arguments []string) int {
	flags := inv.newFlags("explain", "explain [--format text|markdown] [code]")
	format := flags.String("format", formatText, "output format: text or markdown")
	positional, status, proceed := flags.parse(arguments)
	if !proceed {
		return status
	}
	if *format != formatText && *format != formatMarkdown {
		return inv.fail(usageError("unknown --format %q: want text or markdown", *format))
	}
	if len(positional) > 1 {
		return inv.fail(usageError("explain takes at most one diagnostic code"))
	}

	var buffer bytes.Buffer
	if len(positional) == 0 {
		if *format == formatMarkdown {
			writeMarkdownReference(&buffer, seagull.Codes())
		} else {
			writeCodeList(&buffer, seagull.Codes())
		}
		_, _ = inv.streams.Stdout.Write(buffer.Bytes())
		return exitOK
	}

	info, found := seagull.LookupCode(positional[0])
	if !found {
		return inv.fail(usageError("unknown diagnostic code %q; run `%s explain` to list them", positional[0], inv.program))
	}
	if *format == formatMarkdown {
		writeMarkdownCode(&buffer, info)
	} else {
		fmt.Fprintf(&buffer, "%s: %s (%s)\n\n%s\n", info.Code, info.Title, info.Severity, info.Explanation)
	}
	_, _ = inv.streams.Stdout.Write(buffer.Bytes())
	return exitOK
}

// writeCodeList writes one line per code: the code, its severity and its title.
//
// Takes writer (io.Writer) which receives the list.
// Takes codes ([]seagull.CodeInfo) which are listed.
func writeCodeList(writer io.Writer, codes []seagull.CodeInfo) {
	table := newTable(writer)
	fmt.Fprintln(table, "CODE\tSEVERITY\tTITLE")
	for _, info := range codes {
		fmt.Fprintf(table, "%s\t%s\t%s\n", info.Code, info.Severity, info.Title)
	}
	_ = table.Flush()
}

// writeMarkdownReference writes the body of the diagnostic reference: a summary table,
// then one section per code.
//
// Takes writer (io.Writer) which receives the Markdown.
// Takes codes ([]seagull.CodeInfo) which are documented.
func writeMarkdownReference(writer io.Writer, codes []seagull.CodeInfo) {
	fmt.Fprintln(writer, "| Code | Severity | Title |")
	fmt.Fprintln(writer, "|---|---|---|")
	for _, info := range codes {
		fmt.Fprintf(writer, "| [%s](#%s) | %s | %s |\n", info.Code, lowerCode(info.Code), info.Severity, info.Title)
	}
	for _, info := range codes {
		fmt.Fprintln(writer)
		writeMarkdownCode(writer, info)
	}
}

// writeMarkdownCode writes one code's section.
//
// Takes writer (io.Writer) which receives the Markdown.
// Takes info (seagull.CodeInfo) which is documented.
func writeMarkdownCode(writer io.Writer, info seagull.CodeInfo) {
	fmt.Fprintf(writer, "## %s\n\n**%s** (%s)\n\n%s\n", info.Code, info.Title, info.Severity, strings.TrimSpace(info.Explanation))
}

// lowerCode returns a diagnostic code in lower case, as used in documentation anchors.
//
// Takes code (string) which is the code.
//
// Returns string which is lower case.
func lowerCode(code string) string {
	return strings.ToLower(code)
}
