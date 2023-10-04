# gomv

Move function from one package to another.

## Usage
```sh
gomv -dir <DirectoryName> <PackageName.FunctionName> <DestFilePath>
```
Here, `DestFilePath` denotes the path of the destination file.
It can be either absolute path or path relative to project root directory.

For example, to move a function named `fun` inside `util` package
to another file `foo/bar.go`
```sh
gomv -dir /path/to/go/project util.fun foo/bar.go 
```
