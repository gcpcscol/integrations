GO=go

all: build

build:
	${GO} build -v -o stdioImporter ./plugin/importer
	${GO} build -v -o stdioExporter ./plugin/exporter

clean:
	rm -f stdioImporter stdioExporter stdio-*.ptar
