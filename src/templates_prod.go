//go:build !dev

package main

import (
	"embed"
	"html/template"
	"strings"
)

//go:embed templates/**/*
var templateFS embed.FS

func initTemplates() (*template.Template, error) {
	return template.New("").Funcs(templateFuncs()).ParseFS(templateFS, "templates/**/*.html")
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"join": strings.Join,
	}
}
