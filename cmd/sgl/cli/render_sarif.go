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
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/sgl/internal/buildinfo"
)

const (
	// sarifSchema is the JSON Schema of the SARIF 2.1.0 log format.
	sarifSchema = "https://json.schemastore.org/sarif-2.1.0.json"

	// sarifVersion is the SARIF version written.
	sarifVersion = "2.1.0"

	// diagnosticsURL is the page documenting every diagnostic code.
	diagnosticsURL = "https://seagull.sh/docs/reference/diagnostics"

	// sarifLevelError is the SARIF level of an error.
	sarifLevelError = "error"

	// sarifLevelWarning is the SARIF level of a warning.
	sarifLevelWarning = "warning"

	// sarifLevelNote is the SARIF level of a hint.
	sarifLevelNote = "note"
)

// sarifLog is the root of a SARIF log.
type sarifLog struct {
	// Schema is the log's JSON Schema.
	Schema string `json:"$schema"`

	// Version is the SARIF version.
	Version string `json:"version"`

	// Runs holds the one run of sgl.
	Runs []sarifRun `json:"runs"`
}

// sarifRun is one analysis run.
type sarifRun struct {
	// Tool describes sgl and its rules.
	Tool sarifTool `json:"tool"`

	// Results holds one result per diagnostic.
	Results []sarifResult `json:"results"`
}

// sarifTool wraps the tool's driver.
type sarifTool struct {
	// Driver describes sgl.
	Driver sarifDriver `json:"driver"`
}

// sarifDriver describes the analysis tool.
type sarifDriver struct {
	// Name is the tool's name.
	Name string `json:"name"`

	// Version is the tool's version.
	Version string `json:"version"`

	// InformationURI points at the tool's documentation.
	InformationURI string `json:"informationUri"`

	// Rules holds one rule per diagnostic code reported.
	Rules []sarifRule `json:"rules"`
}

// sarifRule describes one diagnostic code.
type sarifRule struct {
	// ID is the diagnostic code.
	ID string `json:"id"`

	// ShortDescription is the code's title.
	ShortDescription sarifMessage `json:"shortDescription"`

	// FullDescription is the code's explanation.
	FullDescription sarifMessage `json:"fullDescription"`

	// HelpURI points at the code's documentation.
	HelpURI string `json:"helpUri"`

	// DefaultConfiguration holds the code's usual level.
	DefaultConfiguration sarifConfiguration `json:"defaultConfiguration"`
}

// sarifConfiguration holds a rule's default level.
type sarifConfiguration struct {
	// Level is error, warning or note.
	Level string `json:"level"`
}

// sarifMessage is a plain-text message.
type sarifMessage struct {
	// Text is the message.
	Text string `json:"text"`
}

// sarifResult is one diagnostic.
type sarifResult struct {
	// RuleID is the diagnostic code.
	RuleID string `json:"ruleId"`

	// Level is error, warning or note.
	Level string `json:"level"`

	// Message describes the problem.
	Message sarifMessage `json:"message"`

	// Locations holds where the problem is.
	Locations []sarifLocation `json:"locations,omitempty"`
}

// sarifLocation wraps a physical location.
type sarifLocation struct {
	// PhysicalLocation is the file and region.
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

// sarifPhysicalLocation is a file and an optional region.
type sarifPhysicalLocation struct {
	// Region is the span, when the diagnostic has a line.
	Region *sarifRegion `json:"region,omitempty"`

	// ArtifactLocation is the file.
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
}

// sarifArtifact names a file.
type sarifArtifact struct {
	// URI is the file's path relative to the working directory.
	URI string `json:"uri"`
}

// sarifRegion is a span of a file.
type sarifRegion struct {
	// StartLine is one-based.
	StartLine int `json:"startLine"`

	// StartColumn is one-based, or omitted.
	StartColumn int `json:"startColumn,omitempty"`

	// EndLine is one-based, or omitted.
	EndLine int `json:"endLine,omitempty"`

	// EndColumn is one-based, or omitted.
	EndColumn int `json:"endColumn,omitempty"`
}

// writeSARIF renders findings as a SARIF 2.1.0 log.
//
// Takes writer (io.Writer) which receives the log.
// Takes findings ([]finding) which are rendered.
//
// Returns error when encoding fails.
func writeSARIF(writer io.Writer, findings []finding) error {
	results := make([]sarifResult, 0, len(findings))
	var codes []string
	for index := range findings {
		item := &findings[index]
		results = append(results, sarifResultFor(item))
		if !slices.Contains(codes, item.diagnostic.Code) {
			codes = append(codes, item.diagnostic.Code)
		}
	}
	slices.Sort(codes)
	rules := make([]sarifRule, 0, len(codes))
	for _, code := range codes {
		rules = append(rules, sarifRuleFor(code))
	}

	document := sarifLog{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "sgl",
				Version:        buildinfo.Version,
				InformationURI: "https://seagull.sh",
				Rules:          rules,
			}},
			Results: results,
		}},
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the SARIF log: %w", err)
	}
	_, err = writer.Write(append(encoded, '\n'))
	return err
}

// sarifResultFor converts one finding.
//
// Takes item (*finding) which is converted.
//
// Returns sarifResult which describes it.
func sarifResultFor(item *finding) sarifResult {
	diagnostic := item.diagnostic
	presented := item.presentation()
	message := presented.Message
	if presented.Help != "" {
		message += " (help: " + presented.Help + ")"
	}
	physical := sarifPhysicalLocation{Region: nil, ArtifactLocation: sarifArtifact{URI: item.display}}
	if diagnostic.Line > 0 {
		physical.Region = &sarifRegion{
			StartLine:   diagnostic.Line,
			StartColumn: diagnostic.Column,
			EndLine:     diagnostic.EndLine,
			EndColumn:   diagnostic.EndColumn,
		}
	}
	return sarifResult{
		RuleID:    diagnostic.Code,
		Level:     sarifLevel(diagnostic.Severity),
		Message:   sarifMessage{Text: message},
		Locations: []sarifLocation{{PhysicalLocation: physical}},
	}
}

// sarifRuleFor describes a diagnostic code from the code table.
//
// Takes code (string) which is the diagnostic code.
//
// Returns sarifRule which describes it.
func sarifRuleFor(code string) sarifRule {
	info, found := seagull.LookupCode(code)
	if !found {
		info = seagull.CodeInfo{Code: code, Title: code, Explanation: code, Severity: seagull.SeverityError}
	}
	return sarifRule{
		ID:                   code,
		ShortDescription:     sarifMessage{Text: info.Title},
		FullDescription:      sarifMessage{Text: info.Explanation},
		HelpURI:              diagnosticsURL + "#" + lowerCode(code),
		DefaultConfiguration: sarifConfiguration{Level: sarifLevel(info.Severity)},
	}
}

// sarifLevel maps a severity to a SARIF level.
//
// Takes severity (seagull.ErrorSeverity) which is mapped.
//
// Returns string which is error, warning or note.
func sarifLevel(severity seagull.ErrorSeverity) string {
	switch severity {
	case seagull.SeverityWarning:
		return sarifLevelWarning
	case seagull.SeverityHint:
		return sarifLevelNote
	default:
		return sarifLevelError
	}
}
