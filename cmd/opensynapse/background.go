package main

import (
	"log"
	"runtime/debug"
)

// guard runs one round of a background job and reports whether it returned.
// A panic in a goroutine ends the whole app, and with it every other job and
// the tray; here it is logged with the job's name and the job goes on with
// its next round. A caller without a wait between rounds must pause after a
// false, or a round that always panics spins.
// shortcut: the page gets no notice of it; Go has no event for one yet.
func guard(job string, round func()) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("%s: panic: %v\n%s", job, r, debug.Stack())
		}
	}()
	round()
	return true
}
