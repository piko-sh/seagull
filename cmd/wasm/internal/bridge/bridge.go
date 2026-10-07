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

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"

	"seagull.sh/seagull/cmd/wasm/internal/session"
	"seagull.sh/seagull/internal/logging"
)

const (
	// requestOverheadBytes allows for the JSON around a request's files: its keys, its
	// overrides and the escapes in the files' text.
	requestOverheadBytes = 64 << 10

	// maxRequestBytes is the longest generate request read. JSON escapes at most double the
	// size of typical SQL, so a request whose files meet session.MaxTotalBytes fits.
	maxRequestBytes = 2*session.MaxTotalBytes + requestOverheadBytes

	// maxFormatRequestBytes is the longest format request read, sized as maxRequestBytes is
	// for one file of session.MaxFileBytes.
	maxFormatRequestBytes = 2*session.MaxFileBytes + requestOverheadBytes

	// maxNameBytes is the longest plain-text argument read: a diagnostic code or a dialect's
	// name.
	maxNameBytes = 256

	// callOperation names a bridge call in the record of a panic it recovered.
	callOperation = "playground bridge call "
)

// Argument is one argument a page passes to a bridge function.
type Argument interface {
	// IsString reports whether the argument is a JavaScript string.
	//
	// Returns bool which is true for a string.
	IsString() bool

	// Length measures a string argument without copying it.
	//
	// Returns int which is its length in UTF-16 code units, never more than its length in
	// UTF-8 bytes.
	Length() int

	// String copies a string argument into Go.
	//
	// Returns string which is the argument's text as UTF-8.
	String() string
}

// function is one bridge function: it reads the call's arguments and returns the value
// Call encodes as JSON.
type function func(ctx context.Context, arguments []Argument) any

// functions maps the name a page calls each bridge function by to the function.
var functions = map[string]function{
	"version":  version,
	"init":     initialise,
	"dialects": listDialects,
	"generate": generate,
	"explain":  explain,
	"format":   format,
	"starter":  starter,
}

// failureView is what a bridge function returns when it fails.
type failureView struct {
	// Error describes the failure.
	Error string `json:"error"`

	// Internal reports a fault in the playground rather than in the input.
	Internal bool `json:"internal"`
}

// initView is what init returns: everything a page needs before its first run.
type initView struct {
	// Version describes the build.
	Version session.VersionView `json:"version"`

	// DefaultDialect is the dialect of a project without sgl.yaml.
	DefaultDialect string `json:"defaultDialect"`

	// Dialects describes every dialect.
	Dialects []session.DialectView `json:"dialects"`

	// Limits holds the request limits.
	Limits limitsView `json:"limits"`

	// Protocol is the version of the generate request and response.
	Protocol int `json:"protocol"`
}

// limitsView holds the limits a generate request must meet.
type limitsView struct {
	// MaxFiles is the most files a request may hold.
	MaxFiles int `json:"maxFiles"`

	// MaxFileBytes is the largest file a request may hold.
	MaxFileBytes int `json:"maxFileBytes"`

	// MaxTotalBytes is the largest total size of a request's files.
	MaxTotalBytes int `json:"maxTotalBytes"`
}

// starterView is what starter returns.
type starterView struct {
	// Files maps each starter file's path to its content.
	Files map[string]string `json:"files"`
}

// Names lists the functions a page can call, sorted.
//
// Returns []string which holds each function's name.
func Names() []string {
	return slices.Sorted(maps.Keys(functions))
}

// Call runs the named function and encodes what it returns as JSON. It never panics: a
// panic becomes an internal failure that names what panicked, and its stack is logged
// once through the logger ctx carries.
//
// Takes name (string) which names the function, one of Names.
// Takes arguments ([]Argument) which are the call's arguments.
//
// Returns string which is the function's answer as JSON, or a failure object with an
// error field.
func Call(ctx context.Context, name string, arguments []Argument) (result string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logging.LogRecoveredPanic(ctx, callOperation+name, recovered)
			result = encode(failureView{Error: fmt.Errorf("%w: %v", errInternal, recovered).Error(), Internal: true})
		}
	}()
	called, found := functions[name]
	if !found {
		return encode(failureView{Error: fmt.Errorf("%w: %q", errUnknownFunction, name).Error(), Internal: true})
	}
	return encode(called(ctx, arguments))
}

// SelfTest generates the SQLite starter project and writes the response as indented JSON.
//
// Takes stdout (io.Writer) which receives the response.
// Takes stderr (io.Writer) which receives an error that stopped the test.
//
// Returns int which is the exit status: 0 when the run succeeded without errors.
func SelfTest(ctx context.Context, stdout, stderr io.Writer) int {
	files, err := session.Starter(session.DefaultDialect)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	response := session.Run(ctx, session.Request{Files: files, Override: session.Override{}, V: session.ProtocolVersion})
	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := fmt.Fprintln(stdout, string(data)); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if !response.OK || response.Counts.Errors > 0 {
		return 1
	}
	return 0
}

// version describes the build.
//
// Takes []Argument which holds page arguments and is ignored.
//
// Returns any which is a session.VersionView.
func version(context.Context, []Argument) any {
	return session.Version()
}

// initialise describes everything a page needs before its first run.
//
// Takes []Argument which holds page arguments and is ignored.
//
// Returns any which is an initView, or a failureView.
func initialise(context.Context, []Argument) any {
	listed, err := session.Dialects()
	if err != nil {
		return failureView{Error: err.Error(), Internal: true}
	}
	return initView{
		Version:        session.Version(),
		DefaultDialect: session.DefaultDialect,
		Dialects:       listed,
		Limits: limitsView{
			MaxFiles:      session.MaxFiles,
			MaxFileBytes:  session.MaxFileBytes,
			MaxTotalBytes: session.MaxTotalBytes,
		},
		Protocol: session.ProtocolVersion,
	}
}

// listDialects describes every dialect.
//
// Takes []Argument which holds page arguments and is ignored.
//
// Returns any which is a []session.DialectView, or a failureView.
func listDialects(context.Context, []Argument) any {
	listed, err := session.Dialects()
	if err != nil {
		return failureView{Error: err.Error(), Internal: true}
	}
	return listed
}

// generate runs one generation.
//
// Takes arguments ([]Argument) whose first is a session.Request as JSON, of at most
// maxRequestBytes.
//
// Returns any which is a session.Response, or a failureView when the request is too long
// or is not valid JSON.
func generate(ctx context.Context, arguments []Argument) any {
	data, err := text(arguments, 0, maxRequestBytes)
	if err != nil {
		return failureView{Error: fmt.Sprintf("the request is %v", err), Internal: false}
	}
	var request session.Request
	if err := json.Unmarshal([]byte(data), &request); err != nil {
		return failureView{Error: fmt.Sprintf("the request is not valid JSON: %v", err), Internal: false}
	}
	return session.Run(ctx, request)
}

// explain documents a diagnostic code.
//
// Takes arguments ([]Argument) whose first is the code as plain text.
//
// Returns any which is a session.ExplainView, or a failureView.
func explain(_ context.Context, arguments []Argument) any {
	code, err := text(arguments, 0, maxNameBytes)
	if err != nil {
		return failureView{Error: fmt.Sprintf("the code is %v", err), Internal: false}
	}
	explained, err := session.Explain(code)
	if err != nil {
		return failureView{Error: err.Error(), Internal: false}
	}
	return explained
}

// format rewrites a SQL file's directives into canonical form.
//
// Takes arguments ([]Argument) whose first is a session.FormatRequest as JSON, of at most
// maxFormatRequestBytes.
//
// Returns any which is a session.FormatResponse.
func format(_ context.Context, arguments []Argument) any {
	data, err := text(arguments, 0, maxFormatRequestBytes)
	if err != nil {
		return session.FormatResponse{Source: "", Error: fmt.Sprintf("the request is %v", err)}
	}
	var request session.FormatRequest
	if err := json.Unmarshal([]byte(data), &request); err != nil {
		return session.FormatResponse{Source: "", Error: fmt.Sprintf("the request is not valid JSON: %v", err)}
	}
	return session.Format(request)
}

// starter returns the files sgl init writes for a dialect.
//
// Takes arguments ([]Argument) whose first is the dialect's name as plain text.
//
// Returns any which is a starterView, or a failureView.
func starter(_ context.Context, arguments []Argument) any {
	name, err := text(arguments, 0, maxNameBytes)
	if err != nil {
		return failureView{Error: fmt.Sprintf("the dialect is %v", err), Internal: false}
	}
	files, err := session.Starter(name)
	if err != nil {
		return failureView{Error: err.Error(), Internal: false}
	}
	return starterView{Files: files}
}

// text reads one string argument, measuring it before copying it so one longer than limit
// is never copied.
//
// Takes arguments ([]Argument) which are the call's arguments.
// Takes position (int) which is the argument's index.
// Takes limit (int) which is the longest argument read, in UTF-8 bytes.
//
// Returns string which is the argument, or empty when it is missing or not a string.
// Returns error which wraps errTooLong when the argument is longer than limit.
func text(arguments []Argument, position, limit int) (string, error) {
	if position >= len(arguments) || arguments[position] == nil || !arguments[position].IsString() {
		return "", nil
	}
	argument := arguments[position]
	if argument.Length() > limit {
		return "", fmt.Errorf("%w: the playground reads at most %d bytes", errTooLong, limit)
	}
	value := argument.String()
	if len(value) > limit {
		return "", fmt.Errorf("%w: the playground reads at most %d bytes", errTooLong, limit)
	}
	return value, nil
}

// encode renders a value as JSON.
//
// Takes value (any) which is encoded.
//
// Returns string which is the JSON, or a failure object when the value cannot be encoded.
func encode(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"error":%q,"internal":true}`, err.Error())
	}
	return string(data)
}
