// SPDX-License-Identifier: MPL-2.0

//go:build !unix

package app

import (
	"context"
	"os"
	"os/signal"
)

func shutdownContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}
