# example.wasm is checked in because TinyGo pins the Go versions it accepts,
# so a build from this repo's go.mod usually cannot produce it.
#
# Rebuild with a Go toolchain TinyGo supports:
#	GOTOOLCHAIN=local GOROOT=$HOME/sdk/go1.26 PATH=$GOROOT/bin:$PATH \
#		tinygo build -o example.wasm -target=wasi .
#
# Built with TinyGo 0.41.1 (LLVM 20.1.1). Unlike a `GOOS=wasip1` build, it
# carries DWARF in .debug_* custom sections instead of a Go pclntab.
PHONY := build
build:
	tinygo build -o example.wasm -target=wasi .
