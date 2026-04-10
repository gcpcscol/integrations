package main

import (
	"context"

	"github.com/PlakarKorp/integration-mysql/exporter"
	"github.com/PlakarKorp/integration-mysql/mysqlconn"
	"github.com/PlakarKorp/kloset/connectors"
	iexporter "github.com/PlakarKorp/kloset/connectors/exporter"
)

func newMySQL(_ context.Context, _ *connectors.Options, proto string, config map[string]string) (iexporter.Exporter, error) {
	conn, err := mysqlconn.ParseConnConfig(config)
	if err != nil {
		return nil, err
	}
	conn.ClientBin = "mysql"
	conn.DumpBin = "mysqldump"
	return exporter.New(proto, conn, config)
}
