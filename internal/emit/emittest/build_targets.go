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

package emittest

var (
	// knownOperatingSystems lists the operating systems the go command reads from the last
	// underscore-separated element of a file name, as go/build's list of them does.
	knownOperatingSystems = map[string]bool{
		"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true, "hurd": true,
		"illumos": true, "ios": true, "js": true, "linux": true, "nacl": true, "netbsd": true,
		"openbsd": true, "plan9": true, "solaris": true, "wasip1": true, "windows": true, "zos": true,
	}

	// knownArchitectures lists the architectures the go command reads from the last
	// underscore-separated element of a file name, as go/build's list of them does.
	knownArchitectures = map[string]bool{
		"386": true, "amd64": true, "amd64p32": true, "arm": true, "armbe": true, "arm64": true,
		"arm64be": true, "loong64": true, "mips": true, "mipsle": true, "mips64": true, "mips64le": true,
		"mips64p32": true, "mips64p32le": true, "ppc": true, "ppc64": true, "ppc64le": true, "riscv": true,
		"riscv64": true, "s390": true, "s390x": true, "sparc": true, "sparc64": true, "wasm": true,
	}
)
