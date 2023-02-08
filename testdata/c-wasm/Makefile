PHONY := llc
llc:
	clang --target=wasm32 -emit-llvm -g -c -S example.c
	llc -O0 -march=wasm32 \
		--filetype=obj \
		example.s
	wasm-ld --no-entry --export-all -o example.wasm example.s.o

PHONY := clang
clang:
	clang -g --target=wasm32 --no-standard-libraries -Wl,--no-entry -o example-clang.wasm example.c

PHONY := odump-clang
odump:
	wasm-objdump -x example-clang.wasm
	wasm-objdump -x example.wasm
