package main

import "slices"

// Which scene and polling rate the foreground program gets. Bringing the
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

// decideScene: a custom scene listing the program, then the game scene
// (detected games and the programs it lists), then the desktop.
func decideScene(c ScenesConfig, exe string, game bool) (int, string) {
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
