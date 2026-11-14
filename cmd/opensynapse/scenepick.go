package main

import (
	"slices"
	"strings"
)

// Which scene and polling rate the foreground window gets. Bringing the
// devices to them is in scenes.go.

const (
	reasonDefault = "default"
	reasonRule    = "rule"
	reasonGame    = "game"
)

// Scene indexes besides custom scenes (0…).
const (
	desktop   = -1
	gameScene = -2
	noScene   = -3
)

// titleHas reports whether a window title holds a rule's text, without case.
// lowTitle is the title in lower case. An empty text matches nothing: only a
// hand-edited file that normalize refused can hold one.
func titleHas(lowTitle, text string) bool {
	return text != "" && strings.Contains(lowTitle, strings.ToLower(text))
}

// wantTitle reports whether a round needs the foreground window's title: a
// switched-on feature has a title rule. Without one no title is ever read.
func wantTitle(sc ScenesConfig) bool {
	return sc.Enabled && (len(sc.Game.Titles) > 0 || slices.ContainsFunc(sc.Scenes, func(s Scene) bool { return len(s.Titles) > 0 }))
}

// decideScene: a custom scene by window title, the game scene by title, a
// custom scene listing the program, then the game scene (the programs it
// lists and detected games), then the desktop. A title goes first because it
// is the narrower rule: it picks one tab or document of a program.
func decideScene(c ScenesConfig, exe, title string, game bool) (int, string) {
	title = strings.ToLower(title)
	titled := func(s Scene) bool {
		return slices.ContainsFunc(s.Titles, func(t string) bool { return titleHas(title, t) })
	}
	if i := slices.IndexFunc(c.Scenes, titled); i >= 0 {
		return i, reasonRule
	}
	if titled(c.Game) {
		return gameScene, reasonRule
	}
	for i, s := range c.Scenes {
		if slices.Contains(s.Apps, exe) {
			return i, reasonRule
		}
	}
	if slices.Contains(c.Game.Apps, exe) {
		return gameScene, reasonRule
	}
	if game {
		return gameScene, reasonGame
	}
	return desktop, reasonDefault
}

func (c ScenesConfig) scene(idx int) Scene {
	switch idx {
	case desktop:
		return c.Desktop
	case gameScene:
		return c.Game
	}
	return c.Scenes[idx]
}

// decideRate: an exception for the program, then the game rate, then the
// normal rate. 0 means each mouse's highest rate.
func decideRate(c SmartPoll, exe string, game bool) (int, string) {
	for _, r := range c.Exceptions {
		if r.Exe == exe {
			return r.Rate, reasonRule
		}
	}
	if game {
		return c.GameRate, reasonGame
	}
	return c.NormalRate, reasonDefault
}
