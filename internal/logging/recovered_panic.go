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

package logging

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
)

// LogRecoveredPanic writes one warning record for a panic the caller has just recovered,
// through the logger attached to ctx.
//
// The record's message is "recovered a panic" and its attributes are the operation, the
// panic value as fmt.Sprint renders it, and the stack of the calling goroutine. Called
// from the deferred function that recovered, the stack still holds the frames that
// panicked, so the stack is reported once here and never needs to travel on an error.
//
// A context that cannot be read, such as one whose Value panics, does not stop the
// record: it is written through the process default logger instead.
//
// Takes operation (string) which names the work that panicked.
// Takes recovered (any) which is the value recover returned.
func LogRecoveredPanic(ctx context.Context, operation string, recovered any) {
	logger, readable := loggerOf(ctx)
	recordContext := ctx
	if !readable {
		recordContext = context.Background()
	}
	logger.WarnContext(recordContext, "recovered a panic",
		slog.String("operation", operation),
		slog.String("panic", fmt.Sprint(recovered)),
		slog.String("stack", string(debug.Stack())),
	)
}
