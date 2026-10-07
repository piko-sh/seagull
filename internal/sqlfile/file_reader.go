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

package sqlfile

import (
	"context"
	"os"
)

// FileReader is the file system access the migration, seed and query readers use, so they
// read from any file system, such as an embed.FS or an in-memory one in tests.
type FileReader interface {
	// ReadFile reads the contents of a file at the given path.
	//
	// Takes path (string) which is the file path to read.
	//
	// Returns []byte which contains the file contents.
	// Returns error when the file cannot be read.
	ReadFile(ctx context.Context, path string) ([]byte, error)

	// ReadDir reads the directory entries, sorted by name.
	//
	// Takes directory (string) which is the directory path to read.
	//
	// Returns []os.DirEntry which contains the directory entries sorted by name.
	// Returns error when the directory cannot be read.
	ReadDir(ctx context.Context, directory string) ([]os.DirEntry, error)
}
