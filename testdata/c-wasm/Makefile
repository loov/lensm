clang  --target=wasm32 -emit-llvm -c -S example.c
llc -march=wasm32 -filetype=obj example.s
wasm-objdump -x example.wasm