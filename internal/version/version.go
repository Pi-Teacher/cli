// Package version 保存构建期注入的版本号.
package version

// Version 由 Makefile 通过 -ldflags -X 注入, 开发构建时为 dev.
var Version = "dev"
