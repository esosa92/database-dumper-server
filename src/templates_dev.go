//go:build dev

package main

import (
	"html/template"
	"strings"
)

func initTemplates() (*template.Template, error) {
	return template.New("").Funcs(templateFuncs()).ParseGlob("templates/**/*.html")
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"join": strings.Join,
	}
}
