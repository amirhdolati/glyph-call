package main

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/amirhdolati/glyph-call/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		message := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) && r != '\n' && r != '\t' {
				return -1
			}
			return r
		}, err.Error())
		fmt.Fprintln(os.Stderr, "Error:", message)
		os.Exit(1)
	}
}
