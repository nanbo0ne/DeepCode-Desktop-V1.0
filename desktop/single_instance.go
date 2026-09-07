package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func singleInstanceLock(app *App) *options.SingleInstanceLock {
	// Allow contributors to run a dev build alongside the installed app.
	// Only an explicit boolean true skips the single-instance lock.
	if devModeEnabled(os.Getenv("ORCA_DEV")) {
		return nil
	}
	return &options.SingleInstanceLock{
		UniqueId: singleInstanceID,
		OnSecondInstanceLaunch: func(options.SecondInstanceData) {
			app.secondInstanceLaunch()
		},
	}
}

func devModeEnabled(raw string) bool {
	enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && enabled
}
