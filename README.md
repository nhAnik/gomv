# gomv

Move function from one package to another.

## Usage
```sh
gomv -dir <DirectoryName> <PackageName.FunctionName> <DestFilePath>
```
For example, to move a function named `fun` inside `util` package
to another file `foo/bar.go`
```sh
gomv -dir /path/to/go/project util.fun foo/bar.go 
```
