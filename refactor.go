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

	for _, pkg := range pkgs {
		if pkg.Name == srcPkgName {
			fm.srcPkg = pkg
			fm.funcDecl, fm.srcAst = searchFunc(pkg, funcName)
			srcFileName = pkg.Fset.Position(fm.srcAst.Package).Filename
			if fm.funcDecl != nil {
				fm.funcObj = pkg.TypesInfo.ObjectOf(fm.funcDecl.Name)
			}
		} else if pkg.Name == dstPkgName {
			fm.dstPkg = pkg
			for _, f := range pkg.Syntax {
				if pkg.Fset.Position(f.Package).Filename == dstFileName {
					fm.dstAst = f
				}
			}
		}
	}
	fm.refInfos = searchFuncRefs(pkgs, fm.funcObj)

	if !fm.isValid() {
		return errors.New("invalid case")
	}
	if srcFileName == dstFileName {
		return errors.New("no op")
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
	expFuncName := strings.ToUpper(funcName[:1]) + funcName[1:]

	// Remove the function declaration from source AST
	for idx, decl := range fm.srcAst.Decls {
		if fDecl, ok := decl.(*ast.FuncDecl); ok {
			if fDecl == fm.funcDecl {
				fm.srcAst.Decls = append(fm.srcAst.Decls[:idx], fm.srcAst.Decls[idx+1:]...)
				break
			}
		}
	}

	// Add the function declaration to destination AST
	fm.funcDecl.Name = &ast.Ident{Name: expFuncName}
	fm.dstAst.Decls = append(fm.dstAst.Decls, fm.funcDecl)

	// Add comments within function
	if fm.funcDecl.Doc != nil {
		fm.dstAst.Comments = append(fm.dstAst.Comments, fm.funcDecl.Doc)
	}
	firstIdx, lastIdx := -1, -1
	for idx, commentGroup := range fm.srcAst.Comments {
		if commentGroup == fm.funcDecl.Doc {
			firstIdx = idx
		}
		if commentGroup.Pos() > fm.funcDecl.Pos() && commentGroup.End() < fm.funcDecl.End() {
			if firstIdx == -1 {
				firstIdx = idx
			}
			fm.dstAst.Comments = append(fm.dstAst.Comments, commentGroup)
			lastIdx = idx
		}
	}

	if firstIdx != -1 && lastIdx != -1 {
		// Remove comments of source AST
		fm.srcAst.Comments = append(fm.srcAst.Comments[:firstIdx], fm.srcAst.Comments[lastIdx+1:]...)
	}

	// Find the used import of the function and add those imports
	// in the destination AST
	dstFset := fm.dstPkg.Fset
	usedImports := getUsedImports(fm.srcAst, fm.funcDecl)
	for _, importSpec := range usedImports {
		path, _ := strconv.Unquote(importSpec.Path.Value)
		if importSpec.Name == nil {
			astutil.AddImport(dstFset, fm.dstAst, path)
		} else {
			astutil.AddNamedImport(dstFset, fm.dstAst, importSpec.Name.Name, path)
		}
	}

	if srcInfo, ok := fm.refactoredFileMap[fm.srcName]; ok {
		srcInfo.new = fm.srcAst
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

	if dstInfo, ok := fm.refactoredFileMap[fm.dstName]; ok {
		dstInfo.new = fm.dstAst
	}

	if err := fm.commit(); err != nil {
		return err
	}

	return nil
}

func (fm funcMoveInfo) commit() error {
	fileToTextMap, fileToDiffMap := make(map[string][]byte), make(map[string]string)

	for fileName, info := range fm.refactoredFileMap {
		newText, err := getFileText(info.fset, info.new)
		if err != nil {
			return errors.New("failed due to invalid AST")
		}
		fileToTextMap[fileName] = newText

		if fm.showPreview {
			edits := myers.ComputeEdits(span.URIFromPath(fileName), info.oldText, string(newText))
			diff := fmt.Sprint(gotextdiff.ToUnified(fileName, fileName, info.oldText, edits))
			fileToDiffMap[fileName] = diff
		}
	}

	shouldCommit := !fm.showPreview
	if fm.showPreview {
		for _, diff := range fileToDiffMap {
			fmt.Println(diff)
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
