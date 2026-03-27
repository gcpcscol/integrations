# PostgreSQL Integration

## Overview

**PostgreSQL** is a powerful, open-source relational database system.

This integration allows you to:

- **Back up a single PostgreSQL database** into a Kloset repository using `pg_dump` (custom format).
- **Back up all databases** on a PostgreSQL server into a Kloset repository using `pg_dumpall` (plain SQL format).
- **Restore a single database** from a snapshot using `pg_restore`.
- **Restore all databases** from a snapshot using `psql`.

## Prerequisites

The following PostgreSQL client tools must be available in `$PATH` on the machine running Plakar:

- `pg_dump` — for single-database backups
- `pg_dumpall` — for full-server backups
- `pg_restore` — for restoring single-database dumps
- `psql` — for restoring full-server dumps

These are typically installed as part of the `postgresql-client` package on most Linux distributions, or via Homebrew (`brew install postgresql`) on macOS.

## Configuration

Both the source (importer) and destination (exporter) connectors share the same set of configuration parameters.

Connection details can be provided as a PostgreSQL URI via the `location` parameter, as individual fields, or as a combination of both (individual fields override the corresponding URI component).

| Parameter  | Required | Description |
|------------|----------|-------------|
| `location` | No       | PostgreSQL connection URI: `postgresql://[user[:password]@]host[:port][/database]` |
| `host`     | No       | PostgreSQL server hostname (default: `localhost`) |
| `port`     | No       | PostgreSQL server port (default: `5432`) |
| `username` | No       | PostgreSQL username |
| `password` | No       | PostgreSQL password |
| `database` | No       | Database name. **Importer**: if set, only that database is backed up (`pg_dump`); if omitted, all databases are backed up (`pg_dumpall`). **Exporter**: if set, overrides the restore target database for single-database dumps; if omitted, the name is inferred from the dump filename. |

### Single-database vs. full-server backup

- **Single database**: set `database` (or include `/dbname` in the `location` URI).
  The importer runs `pg_dump -Fc` and stores one record named `/<dbname>.dump`.
  The exporter restores it with `pg_restore`.

- **All databases**: leave `database` empty (and omit the database path from the URI).
  The importer runs `pg_dumpall` and stores one record named `/all.sql`.
  The exporter restores it with `psql` connecting to the `postgres` maintenance database.

## Examples

### Back up a single database

```bash
# Configure a source for a single database
$ plakar source add mypg postgresql://postgres:secret@db.example.com/myapp

# Back up to the default store
$ plakar backup @mypg
```

Alternatively, using separate fields:

```bash
$ plakar source add mypg postgresql:// \
    host=db.example.com \
    username=postgres \
    password=secret \
    database=myapp
```

### Back up all databases

```bash
# Configure a source without specifying a database (triggers pg_dumpall)
$ plakar source add mypg postgresql://postgres:secret@db.example.com/

# Back up to the default store
$ plakar backup @mypg
```

### Restore a single database

```bash
# Configure a destination pointing to the target server and database
$ plakar destination add mypgdst postgresql://postgres:secret@db.example.com/myapp

# Restore a snapshot to that destination
$ plakar restore -to @mypgdst <snapid>
```

If the `database` parameter is omitted from the destination, the target database
name is inferred from the dump filename in the snapshot (e.g. `myapp.dump` → `myapp`).

### Restore all databases

```bash
# Configure a destination without a specific database
$ plakar destination add mypgdst postgresql://postgres:secret@db.example.com/

# Restore a full pg_dumpall snapshot
$ plakar restore -to @mypgdst <snapid>
```

`psql` will connect to the `postgres` maintenance database and execute the SQL
script, which recreates all databases, roles, and data.
