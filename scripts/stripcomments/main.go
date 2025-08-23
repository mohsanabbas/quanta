package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func stripFile(path string) error {
	if filepath.Base(path) == "doc.go" {
		return nil
	}
	fset := token.NewFileSet()
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	mode := parser.ParseComments
	file, err := parser.ParseFile(fset, path, src, mode)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	file.Comments = nil

	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GenDecl:
			x.Doc = nil
		case *ast.FuncDecl:
			x.Doc = nil
		case *ast.Field:
			x.Doc, x.Comment = nil, nil
		case *ast.TypeSpec:
			x.Doc, x.Comment = nil, nil
		case *ast.ValueSpec:
			x.Doc, x.Comment = nil, nil
		}
		return true
	})
	var buf bytes.Buffer
	cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, file); err != nil {
		return fmt.Errorf("print %s: %w", path, err)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func main() {
	root := flag.String("root", ".", "root directory")
	flag.Parse()
	var files []string
	err := filepath.WalkDir(*root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {

			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || base == "bin" || base == "tools" {
				return nil
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "walk:", err)
		os.Exit(1)
	}
	for _, p := range files {
		if err := stripFile(p); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
