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

package fakedialect

import "seagull.sh/seagull/internal/dialect"

// overridden is an engine that defers to the engine it embeds, except that its Info
// reports what info makes of the embedded engine's.
type overridden struct {
	dialect.DialectPort

	// info returns the description Info reports, given the embedded engine's.
	info func(info dialect.Info) dialect.Info
}

// Info describes the engine as info makes of the embedded engine's description.
//
// Returns dialect.Info which is the description.
func (engine overridden) Info() dialect.Info {
	return engine.info(engine.DialectPort.Info())
}

// Handle returns the handle on an engine that the generator and the facades take, so a
// test can pass a fake, or an engine it wraps, where a dialect is expected.
//
// Takes port (dialect.DialectPort) which is the engine, or nil for the zero handle.
//
// Returns dialect.Dialect which holds the engine.
func Handle(port dialect.DialectPort) dialect.Dialect {
	return dialect.Wrap(port)
}

// Port returns the engine a handle holds, so a test can read the full description it
// reports or wrap it.
//
// Takes handle (dialect.Dialect) which holds the engine.
//
// Returns dialect.DialectPort which is the engine, or nil for the zero handle.
func Port(handle dialect.Dialect) dialect.DialectPort {
	return dialect.Unwrap(handle)
}

// Renamed returns a handle on the engine a handle holds that reports another name and
// family and defers to that engine for everything else, so a test can stand in for a
// dialect of a family no built-in engine has.
//
// The wrapper implements only the engine's own methods; the optional capabilities the
// analyser finds by type assertion stay with the engine it wraps.
//
// Takes handle (dialect.Dialect) which holds the engine to wrap.
// Takes name (string) which is the name the wrapper reports.
// Takes family (string) which is the family the wrapper reports.
//
// Returns dialect.Dialect which holds the wrapper.
func Renamed(handle dialect.Dialect, name, family string) dialect.Dialect {
	return dialect.Wrap(overridden{DialectPort: dialect.Unwrap(handle), info: func(info dialect.Info) dialect.Info {
		info.Name, info.Family = name, family
		return info
	}})
}

// InfoPanics returns a handle on the engine a handle holds whose Info panics with a
// value, so a test can check that a caller recovers a dialect that breaks the panic
// contract. Every other method defers to the engine it wraps.
//
// Takes handle (dialect.Dialect) which holds the engine to wrap.
// Takes value (any) which is the value Info panics with.
//
// Returns dialect.Dialect which holds the wrapper.
func InfoPanics(handle dialect.Dialect, value any) dialect.Dialect {
	return dialect.Wrap(overridden{DialectPort: dialect.Unwrap(handle), info: func(dialect.Info) dialect.Info {
		panic(value)
	}})
}
