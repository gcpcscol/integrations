GO	= go
EXT	=

PLAKAR	= plakar
VERSION	= v1.1.0

all: build

build:
	${GO} build -v -o test-importer${EXT} ./importer
	${GO} build -v -o test-exporter${EXT} ./exporter
	${GO} build -v -o test-storage${EXT} ./storage

package: build
	rm -f example_${VERSION}_*.ptar
	${PLAKAR} pkg create ./manifest.yaml ${VERSION}

uninstall:
	-${PLAKAR} pkg rm example

install: package
	${PLAKAR} pkg add ./webdav_${VERSION}_*.ptar

reinstall: uninstall install

test:
	${GO} test -v ./...

check: test

clean:
	rm -f test-importer${EXT} test-exporter${EXT} test-storage${EXT} test-integration_*.ptar

.PHONY: all build package uninstall install reinstall test check clean
