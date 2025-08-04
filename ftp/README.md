# FTP Integration

## What is FTP?

**FTP (File Transfer Protocol)** is a standard network protocol used to transfer files between a client and server over a TCP/IP connection. It’s widely used for accessing and managing files on remote servers.

This integrations allows:

- Seamless backup of files hosted on FTP servers into a Kloset repository
- Direct restoration of snapshots to remote FTP destinations
- Compatibility with legacy systems and tools that use FTP


## Installation

If a pre-built package exists for your system and architecture,
you can simply install it using:

```sh
$ plakar pkg add ftp
```

Otherwise,
you can first build it:

```sh
$ plakar pkg build ftp
```

This should produce `ftp-vX.Y.Z.ptar` that can be installed with:

```bash
$ plakar pkg add ./ftp-v0.1.0.ptar
```

## Configuration

The configuration parameters are as follow:

- `location` (required): The URL of the FTP server in the form ftp://&lt;host&gt;[:&lt;port&gt;]
- `username` (optional): The username to authenticate as (defaults to anonymous)
- `password` (optional): The password to authenticate with (defaults to anonymous)

## Example Usage

```bash
# configure an FTP source
$ plakar source add myFTPsrc ftp://ftp.example.org/pub/somedirectory

# backup the source
$ plakar backup @myFTPsrc

# configure an FTP destination
$ plakar destination add myFTPdst ftp://ftp.example.org/upload

# restore the snapshot to the destination
$ plakar restore -to @myFTPdst <snapid>
```
