package gomv

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

var ErrNo = errors.New("no from user")

type funcMoveInfo struct {
	srcName, dstName  string
	srcPkg, dstPkg    *packages.Package
	srcAst, dstAst    *ast.File
	funcDecl          *ast.FuncDecl
	funcObj           types.Object
	refInfos          []nodeInfo
	refactoredFileMap map[string]*astInfo
	showPreview       bool
}

func (fm funcMoveInfo) isValid() bool {
	return fm.srcPkg != nil && fm.dstPkg != nil &&
		fm.srcAst != nil && fm.dstAst != nil &&
		fm.funcDecl != nil && fm.funcObj != nil
}

func MoveFunc(pkgs []*packages.Package, funcName, srcPkgName, dstFileName string, showPreview bool) error {
	dstPkgName, err := getPackageName(dstFileName)
	if err != nil {
		return err
	}
	fm := funcMoveInfo{}

	var srcFileName string
	srcPkgFound := false
	for _, pkg := range pkgs {
		if pkg.Name == srcPkgName {
			srcPkgFound = true
			fm.srcPkg = pkg
			fm.funcDecl, fm.srcAst = searchFunc(pkg, funcName)
			if fm.funcDecl == nil {
				return fmt.Errorf("no function named %s in package %s", funcName, pkg)
			}
			if fm.funcDecl.Recv != nil {
				return errors.New("method can not be moved")
			}
			srcFileName = pkg.Fset.Position(fm.srcAst.Package).Filename
			fm.funcObj = pkg.TypesInfo.ObjectOf(fm.funcDecl.Name)
		}
		if pkg.Name == dstPkgName {
			fm.dstPkg = pkg
			for _, f := range pkg.Syntax {
				if pkg.Fset.Position(f.Package).Filename == dstFileName {
					fm.dstAst = f
				}
			}
		}
	}
	if !srcPkgFound {
		return fmt.Errorf("package %s does not exist", srcPkgName)
	}
	fm.refInfos = searchFuncRefs(pkgs, fm.funcObj)

	if !fm.isValid() {
		return errors.New("invalid case")
	}
	if srcFileName == dstFileName {
		return fmt.Errorf("function %s already exists in file %s", funcName, dstFileName)
	}
	fm.srcName, fm.dstName = srcFileName, dstFileName
	fm.refactoredFileMap = make(map[string]*astInfo)
	srcText, _ := getFileText(fm.srcPkg.Fset, fm.srcAst)
	dstText, _ := getFileText(fm.dstPkg.Fset, fm.dstAst)
	fm.refactoredFileMap[srcFileName] = &astInfo{oldText: string(srcText), fset: fm.srcPkg.Fset}
	fm.refactoredFileMap[dstFileName] = &astInfo{oldText: string(dstText), fset: fm.dstPkg.Fset}
	for _, ref := range fm.refInfos {
		fileName := ref.fset.Position(ref.file.Package).Filename
		text, _ := getFileText(ref.fset, ref.file)
		fm.refactoredFileMap[fileName] = &astInfo{oldText: string(text), fset: ref.fset}
	}
	fm.showPreview = showPreview

	if err := fm.move(); err != nil {
		return err
	}
	return nil
}

func (fm funcMoveInfo) move() error {
	funcName := fm.funcDecl.Name.Name
	expFuncName := funcName
	if fm.srcPkg.ID != fm.dstPkg.ID {
		expFuncName = strings.ToUpper(funcName[:1]) + funcName[1:]

		// Export function
		fm.funcDecl.Name = &ast.Ident{Name: expFuncName}

		if fm.funcDecl.Doc != nil {
			// Update function doc comments
			for _, comment := range fm.funcDecl.Doc.List {
				if strings.Contains(comment.Text, funcName) {
					comment.Text = strings.ReplaceAll(comment.Text, funcName, expFuncName)
				}
			}
		}
	}

	dstFset := fm.dstPkg.Fset

	// Find the used import of the function and add those imports
	// in the destination AST
	usedImports := getUsedImports(fm.srcAst, fm.funcDecl)
	for _, importSpec := range usedImports {
		path, _ := strconv.Unquote(importSpec.Path.Value)
		if importSpec.Name == nil {
			astutil.AddImport(dstFset, fm.dstAst, path)
		} else {
			astutil.AddNamedImport(dstFset, fm.dstAst, importSpec.Name.Name, path)
		}
	}

	// Update all the call expressions of the moved function
	for _, ref := range fm.refInfos {
		callExp, ok := ref.node.(*ast.CallExpr)
		if !ok {
			continue
		}
		if ref.file.Name.Name == fm.dstPkg.Name {
			// The call expression can be in the destination package.
			// In that case, no need to make selection expression and
			// add import.
			callExp.Fun = &ast.Ident{Name: expFuncName}
		} else {
			callExp.Fun = &ast.SelectorExpr{
				X:   &ast.Ident{Name: fm.dstPkg.Name},
				Sel: &ast.Ident{Name: expFuncName},
			}
			astutil.AddImport(ref.fset, ref.file, fm.dstPkg.ID)
		}

		fileName := ref.fset.Position(ref.file.Package).Filename
		if info, ok := fm.refactoredFileMap[fileName]; ok {
			info.new = ref.file
		}
	}

	// Move function declaration with comments from source AST to
	// destination AST. The move is done using dave/dst package
	// to move comments properly.
	srcDb, dstDb, err := fm.moveFromSrcToDest()
	if err != nil {
		return err
	}

	if srcInfo, ok := fm.refactoredFileMap[fm.srcName]; ok {
		if srcInfo.fset, srcInfo.new, err = srcDb.restore(); err != nil {
			return err
		}
	}

	if dstInfo, ok := fm.refactoredFileMap[fm.dstName]; ok {
		if dstInfo.fset, dstInfo.new, err = dstDb.restore(); err != nil {
			return err
		}
	}

	if err := fm.commit(); err != nil {
		return err
	}

	return nil
}

func (fm funcMoveInfo) moveFromSrcToDest() (*dstBundle, *dstBundle, error) {
	srcDb, err := newDstBundle(fm.srcPkg.Fset, fm.srcAst)
	if err != nil {
		return nil, nil, err
	}

	var decFuncDecl *dst.FuncDecl
	if decNod, ok := srcDb.dec.Map.Dst.Nodes[fm.funcDecl]; ok {
		decFuncDecl, _ = decNod.(*dst.FuncDecl)
	}
	if decFuncDecl == nil {
		return nil, nil, errors.New("decorated function not found")
	}
	for idx, decl := range srcDb.dstFile.Decls {
		if fDecl, ok := decl.(*dst.FuncDecl); ok {
			if fDecl == decFuncDecl {
				rest := srcDb.dstFile.Decls[idx+1:]
				srcDb.dstFile.Decls = append(srcDb.dstFile.Decls[:idx], rest...)
				break
			}
		}
	}

	dstDb, err := newDstBundle(fm.dstPkg.Fset, fm.dstAst)
	if err != nil {
		return nil, nil, err
	}
	dstDb.dstFile.Decls = append(dstDb.dstFile.Decls, decFuncDecl)

	return srcDb, dstDb, nil
}

func (fm funcMoveInfo) commit() error {
	fileToTextMap, fileToDiffMap := make(map[string][]byte), make(map[string]gotextdiff.Unified)

	for fileName, info := range fm.refactoredFileMap {
		newText, err := getFileText(info.fset, info.new)
		if err != nil {
			return errors.New("move failed due to invalid AST")
		}
		fileToTextMap[fileName] = newText

		if fm.showPreview {
			edits := myers.ComputeEdits(span.URIFromPath(fileName), info.oldText, string(newText))
			diff := gotextdiff.ToUnified(fileName, fileName, info.oldText, edits)
			fileToDiffMap[fileName] = diff
		}
	}

	shouldCommit := !fm.showPreview
	if fm.showPreview {
		for _, diff := range fileToDiffMap {
			previewDiff(diff)
		}
		shouldCommit = yesNo()
	}

	if !shouldCommit {
		return ErrNo
	}

	// Try to write the edited AST in the corresponding file
	for fileName, text := range fileToTextMap {
		if err := os.WriteFile(fileName, text, 0755); err != nil {
			// If any write fails, rollback
			fm.rollback()
			return fmt.Errorf("failed to write in file: %s", fileName)
		}
	}

	return nil
}

func (fm funcMoveInfo) rollback() {
	for fileName, info := range fm.refactoredFileMap {
		os.WriteFile(fileName, []byte(info.oldText), 0755)
	}
}

func yesNo() bool {
	r := bufio.NewReader(os.Stdin)
	for i := 0; i < 5; i++ {
		fmt.Println("Do you want to apply changes? [Y/n]")

		ans, err := r.ReadString('\n')
		if err != nil {
			continue
		}
		ans = strings.ToLower(strings.TrimSpace(ans))
		switch ans {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
	}
	// After max tries, return no
	return false
}
