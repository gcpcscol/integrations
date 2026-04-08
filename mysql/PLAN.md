# MySQL Integration Plugin for Plakar — Implementation Plan

## Overview

This plugin provides logical backup and restore of MySQL databases via Plakar.
The first version targets **logical dumps only** using `mysqldump` and `mysql` CLI tools.
Physical backups (e.g. via Percona XtraBackup or MySQL Enterprise Backup) are out of scope for v1.

The implementation is inspired by the PostgreSQL integration structure but adapted to MySQL semantics,
tools, and capabilities. Options that are PostgreSQL-specific (pg_basebackup, pg_restore custom format,
tablespaces, pg_authid, WAL settings, etc.) have no equivalent here.

---

## Directory Structure

```
integration-mysql/
├── Makefile                   # Build, package, install, test
├── README.md                  # User-facing documentation
├── mysql.1                    # Man page
├── manifest.yaml              # Plugin metadata & connector registration
├── go.mod / go.sum
├── .gitignore
├── LICENSE
│
├── mysqlconn/                 # Shared connection configuration
│   └── conn.go                # URI parsing, CLI args, env vars, ping
│
├── importer/                  # Logical backup via mysqldump
│   ├── importer.go
│   └── schema.json            # Config schema for mysql://
│
├── exporter/                  # Restore via mysql CLI
│   ├── exporter.go
│   └── schema.json            # Config schema for mysql://
│
├── plugin/                    # Plugin entrypoints (SDK wiring)
│   ├── importer/main.go
│   └── exporter/main.go
│
├── manifest/                  # Metadata collection & serialization
│   ├── manifest.go            # Build and emit /manifest.json
│   └── metadata.go            # SQL queries against INFORMATION_SCHEMA etc.
│
└── tests/
    ├── logical/
    │   ├── single_test.go     # Single-database backup + restore + verify
    │   └── alldbs_test.go     # All-databases backup + restore + verify
    ├── testhelpers/
    │   ├── mysql.go           # MySQL container setup helper
    │   ├── plakar.go          # Plakar container helper
    │   ├── exec.go            # Command execution in containers
    │   ├── network.go         # Docker network helper
    │   └── store.go           # Snapshot/restore helpers
    ├── plakar.Dockerfile      # Test image with plugin pre-installed
    └── .dockerignore
```

---

## Step-by-Step Implementation Plan

### Step 1 — Project scaffolding

- [ ] Create `go.mod` with module `github.com/PlakarKorp/integration-mysql`
- [ ] Add dependencies:
  - `github.com/PlakarKorp/go-kloset-sdk` (same version as postgresql)
  - `github.com/PlakarKorp/kloset` (same version)
  - `github.com/testcontainers/testcontainers-go` (tests only)
- [ ] Create `manifest.yaml` registering two connectors:
  - `mysqlImporter` — protocol `mysql`, type `importer`, class `database`
  - `mysqlExporter` — protocol `mysql`, type `exporter`, class `database`
- [ ] Create `Makefile` with targets: `build`, `package`, `install`, `uninstall`,
  `reinstall`, `integration-test`, `clean`
- [ ] Create `.gitignore` and `LICENSE` (ISC)

### Step 2 — Connection configuration (`mysqlconn/conn.go`)

Implement a shared `ConnConfig` struct and helpers used by all connectors.

**Struct fields**:
```go
type ConnConfig struct {
    Host       string // default: 127.0.0.1
    Port       string // default: 3306
    Username   string
    Password   string
    SSLMode    string // disabled, preferred, required, verify_ca, verify_identity
    SSLCert    string
    SSLKey     string
    SSLCA      string
    BinDir     string // mysql_bin_dir, default: "" (use $PATH)
}
```

**Methods**:
- `ParseConnConfig(config map[string]string) (ConnConfig, error)` — parse `location`
  URI (`mysql://user:pass@host:port/dbname`) plus standalone key overrides
- `Args() []string` — returns `-h host -P port -u user` (no password in args)
- `Env() []string` — returns env slice with `MYSQL_PWD=password`, SSL vars if set
- `Ping(ctx, mysqlBin, database string) error` — runs `mysql -e "SELECT 1"` to check
  connectivity
- `BinPath(binary string) string` — joins `BinDir` with binary name if set, else
  returns binary name for PATH lookup

**MySQL vs PostgreSQL differences**:
- Password passed via `MYSQL_PWD` env var (equivalent to `PGPASSWORD`)
- Port flag is `-P` (uppercase), user flag is `-u` (no space required but consistent)
- SSL: `--ssl-cert`, `--ssl-key`, `--ssl-ca`, `--ssl-mode` (MySQL 5.7.11+)
- No concept of `maintenance_db` — connect to any database or without specifying one

**schema.json keys** (shared across connectors):
```
location        mysql:// URI
host            override
port            override (default 3306)
username        override
password        override
ssl_mode        disabled | preferred | required | verify_ca | verify_identity
ssl_cert
ssl_key
ssl_ca
mysql_bin_dir
```

### Step 3 — Manifest metadata (`manifest/`)

#### `manifest/metadata.go`

Collect server metadata via SQL queries against `INFORMATION_SCHEMA`,
`PERFORMANCE_SCHEMA`, and the `mysql` system schema.

**Metadata to collect** (best-effort, query failures are non-fatal):

- **Server info**: `SELECT VERSION()`, `SELECT @@hostname`, `@@port`, `@@datadir`,
  `@@character_set_server`, `@@collation_server`, `@@innodb_buffer_pool_size`,
  `@@max_connections`, `@@sql_mode`, `@@time_zone`
- **Databases**: `INFORMATION_SCHEMA.SCHEMATA` — name, charset, collation
- **Tables**: `INFORMATION_SCHEMA.TABLES` — name, engine, row_format, row_estimate,
  data_length, index_length, auto_increment, charset, collation, create_time,
  update_time, table_type
- **Columns**: `INFORMATION_SCHEMA.COLUMNS` — position, name, type, nullable, default,
  extra (auto_increment), charset, collation
- **Indexes**: `INFORMATION_SCHEMA.STATISTICS` — index_name, column_name, non_unique,
  index_type, seq_in_index
- **Constraints**: `INFORMATION_SCHEMA.TABLE_CONSTRAINTS` +
  `INFORMATION_SCHEMA.KEY_COLUMN_USAGE`
- **Views**: `INFORMATION_SCHEMA.VIEWS` — view_definition, check_option, is_updatable,
  definer, security_type
- **Routines**: `INFORMATION_SCHEMA.ROUTINES` — name, type (FUNCTION/PROCEDURE),
  definer, created, modified, sql_mode, is_deterministic, security_type
- **Triggers**: `INFORMATION_SCHEMA.TRIGGERS` — name, event (INSERT/UPDATE/DELETE),
  timing (BEFORE/AFTER), table, action_statement, definer
- **Events**: `INFORMATION_SCHEMA.EVENTS` — name, definer, status, execute_at,
  interval_value, interval_field, starts, ends
- **Users**: `mysql.user` (if readable) — user, host, authentication_plugin,
  password_expired, account_locked
- **Grants**: `SHOW GRANTS FOR ...` (best-effort, per-user)

#### `manifest/manifest.go`

Build and emit `/manifest.json`:

```json
{
  "version": 1,
  "created_at": "...",
  "connector": "mysql",
  "host": "...",
  "port": "3306",
  "server_version": "8.0.36",
  "mysqldump_version": "mysqldump  Ver 8.0.36 ...",
  "database": "mydb",
  "dump_format": "sql",
  "options": {
    "schema_only": false,
    "data_only": false,
    "single_transaction": true,
    "routines": true,
    "events": true,
    "triggers": true
  },
  "server_config": { ... },
  "databases": [ ... ]
}
```

Connect to MySQL using `database/sql` + `github.com/go-sql-driver/mysql` driver
to collect metadata. This avoids shelling out for metadata and gives structured access.

### Step 4 — Logical importer (`importer/`)

#### `importer/schema.json`

```json
{
  "database":            "single database to back up (empty = all databases)",
  "schema_only":         false,
  "data_only":           false,
  "single_transaction":  true,
  "routines":            true,
  "events":              true,
  "triggers":            true,
  "compress":            false,
  "hex_blob":            false,
  "set_gtid_purged":     "AUTO"
}
```

**Option notes**:
- `single_transaction` (default `true`): Uses `--single-transaction` for InnoDB
  consistency without locking. Critical for production use. If disabled, tables
  are locked during dump.
- `routines`: Include stored procedures and functions via `--routines`
- `events`: Include event scheduler definitions via `--events`
- `triggers`: Include triggers (default true in mysqldump; made explicit)
- `compress`: Compress dump output (via `--compress` in mysqldump, only affects
  client–server protocol, not file size). For file compression, Plakar handles it.
- `hex_blob`: Use `--hex-blob` for BINARY/BLOB columns (hex-encoded, portable)
- `set_gtid_purged`: Controls `SET @@GLOBAL.GTID_PURGED` in dump output
  (`AUTO`, `ON`, `OFF`). Default `AUTO` — include if GTIDs enabled on server.

#### `importer/importer.go`

**Flags**: `FLAG_STREAM` (single-pass streaming)

**Import workflow**:
1. `EmitManifest()` — collect metadata, emit `/manifest.json`
2. If `database` is set:
   - `dumpDatabase()` — run `mysqldump [opts] <database>` → `/<database>.sql`
3. If `database` is empty:
   - `dumpAll()` — run `mysqldump --all-databases [opts]` → `/all.sql`

**Record structure**:
```go
Record{
    Pathname: "/<database>.sql" | "/all.sql",
    FileInfo: {Lname, Lsize: 0, Lmode: 0444},
    ReaderFunc: executes mysqldump stream,
}
```

**`mysqldump` command construction**:
```
mysqldump
  -h <host> -P <port> -u <user>
  [--single-transaction]
  [--routines] [--events] [--triggers / --skip-triggers]
  [--no-data]            (schema_only)
  [--no-create-info]     (data_only)
  [--hex-blob]
  [--set-gtid-purged=AUTO|ON|OFF]
  [--compress]
  <database> | --all-databases
```

**Error handling**:
- Wrap `mysqldump` output in a `cmdReader` (same pattern as postgresql) to capture
  non-zero exit status at EOF
- Detect and surface common errors (access denied, unknown database) with helpful
  messages

**Metadata connection**:
- Use `database/sql` + MySQL driver to connect and run `INFORMATION_SCHEMA` queries
- Reuse the `mysql_bin_dir` config to locate `mysqldump`

### Step 5 — Exporter (`exporter/`)

#### `exporter/schema.json`

```json
{
  "database":    "target database name (inferred from filename if omitted)",
  "create_db":   false,
  "schema_only": false,
  "data_only":   false,
  "force":       false
}
```

**Option notes**:
- `database`: Target database. If omitted, inferred from the `.sql` filename
  (e.g. `mydb.sql` → restore into `mydb`).
- `create_db`: If true, issue `CREATE DATABASE IF NOT EXISTS <db>` before restore.
- `schema_only` / `data_only`: Filter what to restore. Since dumps are plain SQL,
  this is done by pre/post-processing the stream (skip INSERT statements for
  schema_only, skip DDL for data_only) — or document as unsupported in v1.
- `force`: Pass `--force` to `mysql` to continue on SQL errors.

#### `exporter/exporter.go`

**Flags**: `0` (non-streaming, can re-read records)

**Restoration logic**:
- `/manifest.json` → skip
- `*.sql` → pipe to `mysql` CLI:
  ```
  mysql -h <host> -P <port> -u <user> [--force] [<database>]
  ```
  - If `all.sql` (no-database dump): connect without specifying a database
  - If `<dbname>.sql`: connect to `<database>` config value or inferred name
  - If `create_db=true`: run `CREATE DATABASE IF NOT EXISTS <db>` first

**Create DB workflow**:
```go
// Run before piping dump:
mysql -e "CREATE DATABASE IF NOT EXISTS `<db>`"
// Then restore:
mysql <db> < dump.sql
```

### Step 6 — Plugin entrypoints (`plugin/`)

Two thin `main.go` files, each calling the SDK register and start function:

- `plugin/importer/main.go` — registers `mysqlImporter`
- `plugin/exporter/main.go` — registers `mysqlExporter`

### Step 7 — Documentation

#### `README.md`

Cover:
- Prerequisites (MySQL server, `mysqldump`/`mysql` in PATH)
- Installation (`plakar pkg add ...`)
- Quick start: backup and restore a single database
- Quick start: backup and restore all databases
- All configuration options with descriptions and defaults
- SSL/TLS configuration
- `mysqldump` version requirements (MySQL 5.7+ / 8.x)
- MySQL-specific notes: InnoDB consistency (`--single-transaction`), GTIDs,
  routines/events/triggers, binary log coordinates
- Restore workflow: single-db restore, full-server restore
- Known limitations (v1): no physical backup, no per-table filtering,
  `schema_only` / `data_only` not supported for restore yet
- Troubleshooting section

#### `mysql.1` (man page)

Standard Unix man page covering:
- NAME, SYNOPSIS, DESCRIPTION
- OPTIONS (all configuration keys with types and defaults)
- EXAMPLES (backup / restore commands)
- NOTES (MySQL-specific caveats)
- SEE ALSO
- BUGS / LIMITATIONS

**Documentation must be updated whenever**:
- A new configuration option is added/removed/renamed
- Restore or backup behavior changes
- A new MySQL version is tested/supported

### Step 8 — Integration tests (`tests/`)

#### Test strategy

- Use `testcontainers-go` with `mysql:8` Docker image
- Tests are black-box: spin up containers, run `plakar backup`, verify output
- Data verification: seed known rows, restore to fresh container, count/compare rows

#### `tests/testhelpers/mysql.go`

```go
func StartMySQLContainer(ctx, network) (*MySQLContainer, error)
```

- Starts `mysql:8` image
- Sets root password, creates test database and test user
- Seeds test tables:
  ```sql
  CREATE TABLE users (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(255));
  INSERT INTO users (name) VALUES ('alice'), ('bob'), ('charlie');
  CREATE TABLE orders (id INT AUTO_INCREMENT PRIMARY KEY, user_id INT, amount DECIMAL(10,2));
  INSERT INTO orders (user_id, amount) VALUES (1, 99.99), (2, 149.50);
  ```
- Creates stored procedure, trigger, and event for testing `routines`/`events`/`triggers`

#### `tests/logical/single_test.go` — `TestSingleDatabaseBackup`

1. Start MySQL source container (network: `test-net`)
2. Start Plakar container (same network)
3. Initialize Plakar store
4. Run: `plakar backup mysql://root:secret@mysql/testdb`
5. Inspect snapshot:
   - Verify `/manifest.json` exists and is valid JSON
   - Verify `/testdb.sql` exists and is non-empty
   - Verify manifest contains correct `server_version`, `database`, table metadata
6. Start fresh MySQL restore target container (same network)
7. Run: `plakar restore -to mysql://root:secret@mysql-restore/testdb create_db=true <snapid>`
8. Verify row counts match in source and target

#### `tests/logical/alldbs_test.go` — `TestAllDatabasesBackup`

1. Start MySQL source container with two databases (testdb, seconddb)
2. Start Plakar container
3. Run: `plakar backup mysql://root:secret@mysql` (no database = all)
4. Verify `/all.sql` in snapshot
5. Start fresh MySQL restore target
6. Run: `plakar restore -to mysql://root:secret@mysql-restore <snapid>`
7. Verify both databases and their row counts match

#### `tests/plakar.Dockerfile`

```dockerfile
FROM golang:1.24
RUN apt-get update && apt-get install -y default-mysql-client ca-certificates
ARG PLAKAR_SHA
RUN go install github.com/PlakarKorp/plakar@${PLAKAR_SHA}
COPY . /src
RUN cd /src && \
    go build -o /tmp/mysqlpkg/mysqlImporter ./plugin/importer && \
    go build -o /tmp/mysqlpkg/mysqlExporter ./plugin/exporter && \
    cp manifest.yaml /tmp/mysqlpkg/ && \
    cd /tmp/mysqlpkg && \
    plakar pkg create ./manifest.yaml v0.0.1 && \
    plakar pkg add ./mysql_v0.0.1_*.ptar
```

### Step 9 — Makefile

```makefile
PLAKAR  ?= plakar
VERSION ?= v1.0.0
GO       = go

GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

build:
    $(GO) build -o mysqlImporter ./plugin/importer
    $(GO) build -o mysqlExporter ./plugin/exporter

package: build
    mkdir -p dist
    cp mysqlImporter mysqlExporter manifest.yaml dist/
    cd dist && $(PLAKAR) pkg create ./manifest.yaml $(VERSION)

install: package
    $(PLAKAR) pkg add dist/mysql_$(VERSION)_$(GOOS)_$(GOARCH).ptar

uninstall:
    $(PLAKAR) pkg remove mysql

reinstall: uninstall install

integration-test:
    $(GO) test ./tests/... -v -timeout 10m

clean:
    rm -f mysqlImporter mysqlExporter
    rm -rf dist
```

---

## MySQL-Specific Design Decisions

### Why no `globals.sql` equivalent

PostgreSQL's `pg_dumpall --globals-only` exports roles and tablespaces separately
from data. MySQL has no equivalent granularity in `mysqldump`. In single-database
dumps, users/grants are intentionally excluded (they live in `mysql` system schema).
In all-databases dumps, `--all-databases` includes the `mysql` schema which contains
user accounts, but restoration requires care to not overwrite system tables carelessly.

For v1, we document that user/grant migration must be handled manually for
single-database restores. Future versions may add `--dump-grants` support via
`SHOW GRANTS` iteration.

### Why `single_transaction` defaults to `true`

Without `--single-transaction`, `mysqldump` locks all tables for the duration of the
dump (via `LOCK TABLES`). This is rarely acceptable in production. For InnoDB (the
default engine since MySQL 5.5), `--single-transaction` provides a consistent snapshot
without locks. For MyISAM tables, locking is still required; a warning should be
emitted if the server has MyISAM tables.

### Why `set_gtid_purged` is exposed

When GTIDs are enabled on the MySQL server, `mysqldump` includes `SET @@GLOBAL.GTID_PURGED`
statements in the dump. Restoring to a server that already has GTIDs causes errors.
Exposing `set_gtid_purged=OFF` lets users disable this for cross-server restores.

### Why no custom binary dump format

`mysqldump` only produces plain SQL. There is no equivalent to `pg_dump -Fc` (custom
binary format). All dumps are `.sql` files, which simplifies the exporter: always use
`mysql` CLI to restore, never `pg_restore`.

### Metadata via `database/sql`

Unlike PostgreSQL where metadata is collected via `psql` subprocess, MySQL metadata is
collected using a Go SQL driver (`go-sql-driver/mysql`). This gives structured access
to `INFORMATION_SCHEMA` without parsing text output, and avoids subprocess overhead for
metadata queries.

---

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/PlakarKorp/go-kloset-sdk` | Plugin SDK (importer/exporter registration) |
| `github.com/PlakarKorp/kloset` | Core connector interfaces |
| `github.com/go-sql-driver/mysql` | MySQL driver for metadata collection |
| `github.com/testcontainers/testcontainers-go` | Docker containers in tests |

---

## Implementation Order

1. **Scaffolding** (go.mod, manifest.yaml, Makefile, .gitignore, LICENSE)
2. **`mysqlconn/conn.go`** (connection config, shared by all connectors)
3. **`manifest/metadata.go`** (SQL queries for metadata)
4. **`manifest/manifest.go`** (build and emit manifest.json)
5. **`importer/schema.json`** + **`importer/importer.go`**
6. **`plugin/importer/main.go`**
7. **`exporter/schema.json`** + **`exporter/exporter.go`**
8. **`plugin/exporter/main.go`**
9. **`tests/testhelpers/`** (MySQL + Plakar container helpers)
10. **`tests/logical/single_test.go`**
11. **`tests/logical/alldbs_test.go`**
12. **`tests/plakar.Dockerfile`**
13. **`README.md`**
14. **`mysql.1`** (man page)
15. Final review: ensure README and man page are consistent with implementation

---

## Documentation Checklist (to verify on every update)

- [ ] All config keys in `schema.json` files are documented in README and man page
- [ ] All README examples are tested and accurate
- [ ] Man page OPTIONS section matches schema.json
- [ ] Known limitations section is up to date
- [ ] MySQL version requirements are stated
- [ ] SSL configuration section is accurate

---

## Future Work (out of scope for v1)

- Physical backup via Percona XtraBackup or MySQL Shell's `dump` utility
- Per-table include/exclude filtering
- Dump binary log coordinates for point-in-time recovery setup
- User/grant dump and restore (single-database mode)
- Parallel table dump (mydumper integration)
- Restore-time `schema_only` / `data_only` filtering
- Support for MySQL replicas (read-only source, consistent backup)
