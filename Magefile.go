//go:build mage

package main

import build "github.com/grafana/grafana-plugin-sdk-go/build"

func BuildLinux() error {
	return build.Build{}.Linux()
}

func Default() error {
	return BuildLinux()
}
