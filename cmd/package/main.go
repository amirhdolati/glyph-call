// Package creates explicit-allowlist release archives without local signaling,
// Python environments, dependency caches or user media.
package main

import (
	"archive/zip"
	"debug/macho"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func archive(path string, files []string) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	writer := zip.NewWriter(out)
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = "glyph-call/" + filepath.ToSlash(path)
		header.Method = zip.Deflate
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(entry, file)
		file.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return writer.Close()
}
func run() error {
	if err := os.MkdirAll("dist", 0755); err != nil {
		return err
	}
	common := []string{"README.md", "docs/media/call-demo.gif", "docs/media/animated-demo.gif", "docs/media/donation-tron.png", "Start.command", "Install.command"}
	binary := append(append([]string{}, common...), "bin/glyph-call")
	architecture := runtime.GOARCH
	if binary, err := macho.OpenFat("bin/glyph-call"); err == nil {
		if len(binary.Arches) > 1 {
			architecture = "universal"
		}
		binary.Close()
	}
	release := fmt.Sprintf("dist/glyph-call-%s-%s.zip", runtime.GOOS, architecture)
	if err := archive(release, binary); err != nil {
		return err
	}
	if err := archive("dist/glyph-call-share.zip", binary); err != nil {
		return err
	}
	source := append(append([]string{}, common...), "go.mod", "go.sum", "build.sh", "CONTRIBUTING.md")
	for _, root := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			switch filepath.Ext(path) {
			case ".go", ".m", ".h", ".plist":
				source = append(source, path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	sort.Strings(source)
	if err := archive("dist/glyph-call-go-source.zip", source); err != nil {
		return err
	}
	for _, path := range []string{release, "dist/glyph-call-share.zip", "dist/glyph-call-go-source.zip"} {
		fmt.Println(strings.TrimSpace(path))
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
