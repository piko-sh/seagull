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

//go:build js && wasm

package main

import (
	"context"
	"os"
	"runtime/debug"
	"slices"
	"syscall/js"

	"seagull.sh/seagull/cmd/wasm/internal/bridge"
)

const (
	// globalName is the property of the worker's global object the bridge is set on.
	globalName = "seagull"

	// readyName is the optional global function called once the bridge is set.
	readyName = "seagullReady"

	// selfTestFlag runs the self test instead of registering the bridge.
	selfTestFlag = "-selftest"

	// memoryLimit is the soft limit on the Go heap, so a large project triggers collection
	// well before the browser's limit on WebAssembly memory.
	memoryLimit = 512 << 20
)

// jsArgument is one JavaScript argument of a bridge call, read only as far as the bridge
// asks.
type jsArgument struct {
	// value is the argument.
	value js.Value
}

// IsString reports whether the argument is a JavaScript string.
//
// Returns bool which is true for a string.
func (argument jsArgument) IsString() bool {
	return argument.value.Type() == js.TypeString
}

// Length measures a string argument in JavaScript, without copying it into Go. A string
// primitive has no properties Go can read, so it is measured through its String object.
//
// Returns int which is its length in UTF-16 code units.
func (argument jsArgument) Length() int {
	return js.Global().Get("Object").Invoke(argument.value).Length()
}

// String copies a string argument into Go.
//
// Returns string which is the argument's text as UTF-8.
func (argument jsArgument) String() string {
	return argument.value.String()
}

// main registers the bridge, calls seagullReady when the page defined it, and keeps the
// program alive to serve the bridge; or runs the self test when asked.
func main() {
	debug.SetMemoryLimit(memoryLimit)
	if slices.Contains(os.Args[1:], selfTestFlag) {
		os.Exit(bridge.SelfTest(context.Background(), os.Stdout, os.Stderr))
	}
	register()
	if ready := js.Global().Get(readyName); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}

// register sets the bridge on the global object: one JavaScript function per bridge
// function, each taking and returning strings and never throwing.
func register() {
	functions := map[string]any{}
	for _, name := range bridge.Names() {
		functions[name] = js.FuncOf(func(_ js.Value, arguments []js.Value) any {
			return bridge.Call(context.Background(), name, adapt(arguments))
		})
	}
	js.Global().Set(globalName, js.ValueOf(functions))
}

// adapt wraps a call's JavaScript arguments for the bridge.
//
// Takes arguments ([]js.Value) which are the call's arguments.
//
// Returns []bridge.Argument which holds one entry per argument.
func adapt(arguments []js.Value) []bridge.Argument {
	adapted := make([]bridge.Argument, 0, len(arguments))
	for _, argument := range arguments {
		adapted = append(adapted, jsArgument{value: argument})
	}
	return adapted
}
