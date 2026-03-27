GO=go
EXT=

all: build

build:
	${GO} build -v -o postgresqlImporter${EXT} ./plugin/importer
	${GO} build -v -o postgresqlExporter${EXT} ./plugin/exporter
	${GO} build -v -o postgresqlBinImporter${EXT} ./plugin/binimporter

clean:
	rm -f postgresqlImporter postgresqlExporter postgresqlBinImporter
