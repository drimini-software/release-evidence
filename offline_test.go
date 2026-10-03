package releaseevidence_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFirstSliceHasNoNetworkImports(t *testing.T) {
	for _, root := range []string{
		"cmd/release-evidence",
		"internal/detector",
		"internal/model",
		"internal/packet",
		"internal/repository",
		"internal/scanner",
	} {
		err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), filePath, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range parsed.Decls {
				declaration, ok := spec.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, item := range declaration.Specs {
					importSpec, ok := item.(*ast.ImportSpec)
					if !ok {
						continue
					}
					importPath, err := strconv.Unquote(importSpec.Path.Value)
					if err != nil {
						return err
					}
					if importPath == "net" || strings.HasPrefix(importPath, "net/") {
						t.Errorf("production file %s imports %s; the first scan slice must remain offline", filePath, importPath)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
