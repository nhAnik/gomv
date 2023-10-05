# gomv

Move function from one package to another.

## Usage
```
Move function from one package to another.
Usage:
    gomv [flags] <PackageName.FunctionName> <DestFilePath>

Flags:
    -dir <location of the project directory>
    -no-preview
```
## Examples

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
By default, a preview will be shown before applying changes and users will
be prompted whether they want to apply the changes.

To disable preview and blindly accept changes (which is not recommended),
`-no-preview` flag can be used.
```sh
gomv -dir /path/to/go/project -no-preview util.fun foo/bar.go
```
