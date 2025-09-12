GO=go
EXT=

all: build

build:
	${GO} build -v -o stdioImporter${EXT} ./plugin/importer
	${GO} build -v -o stdioExporter${EXT} ./plugin/exporter

clean:
	rm -f stdioImporter stdioExporter stdio-*.ptar
