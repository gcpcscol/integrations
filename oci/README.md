# OCI Registry Store Integration

## Overview

**OCI Registries (Open Container Initiative registries)** are the standard distribution mechanism for container images and related artifacts (Helm charts, SBOMs, signatures, WASM modules, generic OCI artifacts). Popular implementations include Docker Hub, Amazon ECR, Google Artifact Registry, Azure Container Registry, GitHub Container Registry, and self-hosted registries such as Harbor.

This integration allows Kloset / Plakar to use an OCI registry as a **native storage backend** for repositories, leveraging existing registry infrastructure, authentication, replication, and immutability guarantees.

With this store, repository data is encoded as OCI artifacts and pushed to a registry, making it easy to host backups alongside container images while benefiting from cloud-native distribution and access controls.

## Capabilities

* **Store a full Kloset repository in an OCI registry**
  Packfiles, states, indexes, and metadata are stored as OCI artifacts under a dedicated repository namespace.

* **Content-addressed, immutable storage**
  Data blobs map naturally to OCI layers, enabling strong integrity guarantees and efficient deduplication.

* **Registry-native distribution and replication**
  Works with registry mirroring, geo-replication, and caching without special handling.

* **Compatible with managed and self-hosted registries**
  Supports Docker Hub, GHCR, ECR, GCR, ACR, Harbor, and any OCI-compliant registry.

* **Air-gapped and firewall-friendly deployments**
  Uses standard HTTPS traffic and registry APIs already allowed in most environments.

## Repository Layout (Conceptual)

The store maps internal repository objects to OCI artifacts:

* **Blobs** → OCI layers (content-addressed)
* **Manifests** → Logical groupings (packfiles, states)
* **Tags** → Repository markers (e.g. `latest`, snapshot heads, GC roots)

The exact layout is an internal detail and may evolve, but the registry always contains valid OCI artifacts.

## Configuration

The configuration parameters are as follows:

* `location` (required): OCI registry reference where the store lives
  (e.g. `oci://ghcr.io/my-org/plakar-store`)

* `username` (optional): Registry username (if required)

* `password` (optional): Registry password or token

* `use_tls` (optional): Whether to use TLS (defaults to `true`)

* `tls_insecure_no_verify` (optional): Disable TLS certificate verification (defaults to `false`)

Optional, registry-specific parameters:

* `repository_prefix`: Prefix used inside the registry repository
* `media_type`: Custom OCI media type for stored artifacts (advanced)

Authentication may also be provided via:

* Docker credential helpers
* Environment variables
* Cloud provider IAM (ECR, GCR, ACR), when supported

## Examples

```bash
# Configure an OCI registry store (GitHub Container Registry)
$ plakar store add myOCIstore \
  oci://ghcr.io/my-org/plakar \
  username=YOUR_USERNAME \
  password=YOUR_TOKEN

# Create the store
$ plakar at @myOCIstore create

# Use the store for backups
$ plakar at @myOCIstore backup /data

# List snapshots
$ plakar at @myOCIstore snapshots

# Restore a snapshot
$ plakar at @myOCIstore restore <snapid> /restore/path
```

Example using a cloud-managed registry (AWS ECR):

```bash
# Authentication handled via AWS IAM
$ plakar store add myOCIstore oci://123456789012.dkr.ecr.us-east-1.amazonaws.com/plakar

$ plakar at @myOCIstore create
```

## Use Cases

* **Cloud-native backup storage** using existing container registries
* **Disaster recovery** with registry replication across regions
* **Backup distribution** through standard OCI tooling
* **Air-gapped environments** where registries are the only allowed artifact store
* **Unified artifact management** (images, SBOMs, backups in one system)

## Notes and Best Practices

* Use **dedicated repositories** for backup data to avoid mixing with container images.
* Enable **immutability / retention policies** on the registry when available.
* Prefer **token-based authentication** over static passwords.
* Monitor registry quotas and storage costs, especially for large repositories.

## Limitations

* Performance depends on registry implementation and blob size limits.
* Garbage collection behavior is registry-specific.
* Some registries enforce strict rate limits on pushes and pulls.

## Compatibility

This store targets the OCI Distribution Specification and should work with any compliant registry implementation.

---

This integration turns OCI registries into first-class, cloud-native storage backends for Kloset repositories.
