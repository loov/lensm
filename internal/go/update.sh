#!/bin/bash

GOROOT=$(go env GOROOT)
rm -rf ./src
mkdir -p ./src

# copy relevant packages
cp -r $GOROOT/src/cmd/internal/{archive,bio,goobj,objabi,objfile,src,sys} ./src/
cp -r $GOROOT/src/internal/{buildcfg,goexperiment,saferio,unsafeheader,xcoff} ./src/

# remove testdata
rm -rf ./src/{archive,bio,goobj,objabi,objfile,src,sys}/*_test.go
rm -rf ./src/{archive,bio,goobj,objabi,objfile,src,sys}/testdata
rm -rf ./src/{buildcfg,goexperiment,saferio,unsafeheader,xcoff}/*_test.go
rm -rf ./src/{buildcfg,goexperiment,saferio,unsafeheader,xcoff}/testdata

# expose some things
cp expose.go_ ./src/objfile/expose.go

# replace imports
# "cmd/internal/ -> "loov.dev/lensm/internal/go/src/
# "internal/ -> "loov.dev/lensm/internal/go/src/