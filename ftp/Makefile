GO=go

all: build

build:
	${GO} build -v -o ftpImporter ./plugin/importer
	${GO} build -v -o ftpExporter ./plugin/exporter

clean:
	rm -f ftpImporter ftpExporter ftp-*.ptar
