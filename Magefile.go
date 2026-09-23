//go:build mage
// +build mage

package main

import (
	// mage:import
	build "github.com/grafana/grafana-plugin-sdk-go/build"
)

// Default configures the default target and exposes the Grafana SDK build,
// test, coverage, and cross-platform targets through Mage.
var Default = build.BuildAll
