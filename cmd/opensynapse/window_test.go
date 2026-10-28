package main

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestWindowSize(t *testing.T) {
	for _, c := range []struct {
		name string
		work mygo.Rectangle // in DIPs: pixels divided by the scale
		want winSize
	}{
		{"2560x1440 at 100%", mygo.Rectangle{Width: 2560, Height: 1392}, winSize{1320, 900, 960, 620}},
		{"1920x1080 at 100%", mygo.Rectangle{Width: 1920, Height: 1032}, winSize{1152, 774, 960, 620}},
		{"1920x1080 at 150%", mygo.Rectangle{Width: 1280, Height: 688}, winSize{960, 620, 960, 620}},
		{"1366x768 at 100%", mygo.Rectangle{Width: 1366, Height: 728}, winSize{960, 620, 960, 620}},
		{"1280x800 at 150%", mygo.Rectangle{Width: 853, Height: 501}, winSize{853, 501, 853, 501}},
	} {
		if got := windowSize(c.work); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}
