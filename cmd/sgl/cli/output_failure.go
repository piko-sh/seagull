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
	"fmt"
	"io"
	"sync"
)

// outputFailure retains the first failed command output write.
type outputFailure struct {
	// err retains the first error with its stream name.
	err error

	// mutex guards err. It is never held while a stream is written.
	mutex sync.Mutex
}

// checkedOutput records failures even when a command ignores its write result.
type checkedOutput struct {
	// writer receives the command's original output bytes.
	writer io.Writer

	// failure keeps the first failure of either command output stream.
	failure *outputFailure

	// name identifies the failed stream without exposing its contents.
	name string
}

// Write forwards output while retaining errors and incomplete writes.
//
// Takes data ([]byte) which contains the command's output.
//
// Returns int which counts bytes accepted by the original writer.
// Returns error when writing fails or the writer accepts an incomplete buffer.
func (output *checkedOutput) Write(data []byte) (int, error) {
	count, err := output.writer.Write(data)
	if count < 0 || count > len(data) {
		count, err = 0, io.ErrShortWrite
	}
	if err == nil && count != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		output.failure.record(fmt.Errorf("writing %s: %w", output.name, err))
	}
	return count, err
}

// Err reports the first failed output write after command execution.
//
// Returns error which is nil when every output write succeeded.
//
// Concurrency: Safe for concurrent use.
func (failure *outputFailure) Err() error {
	failure.mutex.Lock()
	defer failure.mutex.Unlock()
	return failure.err
}

// record keeps err unless an earlier failure is already kept.
//
// Takes err (error) which is the failed write.
//
// Concurrency: Safe for concurrent use.
func (failure *outputFailure) record(err error) {
	failure.mutex.Lock()
	defer failure.mutex.Unlock()
	if failure.err == nil {
		failure.err = err
	}
}
