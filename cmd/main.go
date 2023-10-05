package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nhAnik/gomv"
	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

func getModulePath(src string) (string, error) {
	goModPath := filepath.Join(src, "go.mod")
	bytes, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}
	goModFile, err := modfile.Parse(goModPath, bytes, nil)
	if err != nil {
		return "", err
	}
	return goModFile.Module.Mod.Path, nil
}

const doc = `Move function from one package to another.
Usage:
    gomv [flags] <PackageName.FunctionName> <DestFilePath>

Flags:
    -dir <location of the project directory>
    -no-preview
`

func usage() {
	fmt.Fprint(os.Stderr, doc)
	os.Exit(2)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

var errPkg = errors.New("packages contain errors")

func main() {
	dir := flag.String("dir", "", "location of the go project")
	noPreview := flag.Bool("no-preview", false, "preview of the change")
	flag.Usage = usage
	flag.Parse()
	sourceDir := *dir

	if sourceDir == "" {
		var err error
		sourceDir, err = os.Getwd()
		if err != nil {
			die(err)
		}
	}

	paths := flag.Args()
	if len(paths) != 2 {
		usage()
	}
	split := strings.Split(paths[0], ".")
	if len(split) != 2 {
		usage()
	}
	srcPkgName := split[0]
	funcName := split[1]
	dstFileName := paths[1]
	cfg := &packages.Config{
		Mode: packages.NeedSyntax | packages.NeedFiles | packages.NeedName |
			packages.NeedTypesInfo | packages.NeedTypes,
		Dir: sourceDir,
	}
	modPath, err := getModulePath(sourceDir)
	if err != nil {
		die(err)
	}
	pkgs, err := packages.Load(cfg, modPath+"/...")
	if err != nil || packages.PrintErrors(pkgs) > 0 {
		die(errPkg)
	}
	// Check if dstFileName is absolute path or relative path
	var dstFileAbsPath string
	if strings.HasPrefix(dstFileName, sourceDir) {
		dstFileAbsPath = dstFileName
	} else {
		dstFileAbsPath = filepath.Join(sourceDir, dstFileName)
	}
	if _, err := os.Stat(dstFileAbsPath); errors.Is(err, os.ErrNotExist) {
		die(fmt.Errorf("file %s does not exist", dstFileAbsPath))
	}
	showPreview := !*noPreview
	if err := gomv.MoveFunc(pkgs, funcName, srcPkgName, dstFileAbsPath, showPreview); err != nil {
		if errors.Is(err, gomv.ErrNo) {
			fmt.Println("No change applied")
			return
		} else {
			die(err)
		}
	}

	fmt.Println("Moved")
}
