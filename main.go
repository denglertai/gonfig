/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"github.com/denglertai/gonfig/cmd"
	"github.com/denglertai/gonfig/internal/plugin"
)

func main() {
	// Initialze the Plugin system
	plugin.InitPlugins()

	cmd.Execute()
}
