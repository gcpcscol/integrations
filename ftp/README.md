# FTP integration

## Overview

The **Plakar FTP integration** enables seamless backup and restoration of FTP content to and from a [Kloset repository](/posts/2025-04-29/kloset-the-immutable-data-store/).

## Installation

First, build the binaries:

```bash
$ go build -o ftpImporter ./plugin/importer
$ go build -o ftpExporter ./plugin/exporter
```

Create the ptar plugin:

```bash
$ plakar pkg create manifest.yaml
```

This should produce `ftp-vX.Y.Z.ptar` that can be installed with:

```bash
$ plakar pkg add ftp-v0.1.0.ptar
```

## Configuration

The configuration parameters are as follow:

- `location`: The URL of the IMAP server in the form ftp://<host>:<port>.

## Example Usage

```bash
# configure an FTP source connector
$ plakar source add myFTPsrc ftp://ftp.example.org

# backup the mailbox
$ plakar backup @myFTPsrc

# configure an FTP destination connector
$ plakar destination add myFTPdst ftp://ftp.example.org

# restore the snapshot to the destination
$ plakar restore -to @myFTPdst <snapid>
```

## Questions, Feedback, and Support

Found a bug? [Open an issue on GitHub](https://github.com/PlakarKorp/plakar/issues/new?title=Bug%20report%20on%20Filesystem%20integration&body=Please%20provide%20a%20detailed%20description%20of%20the%20issue.%0A%0A**Plakar%20version**)

Join our [Discord community](https://discord.gg/uuegtnF2Q5) for real-time help and discussions.
