# Kaito Adapter Image Format Specification

This document describes the specification for Kaito-compatible adapter images. It assumes the reader is familiar with the [OCI Image Format Specification](https://github.com/opencontainers/image-spec).

## Image Layout

A Kaito-compatible adapter image consists of any OCI image with the file structure below. Files added to the image but not specified here are simply ignored. The image can use any base and have as many layers as needed.
```
/
└── data
    ├── adapter_config.json
    └── adapter_model.safetensors
```

A minimal and conformant image can be created by building the following Dockerfile:

```dockerfile
FROM scratch
COPY adapter_config.json       /data/
COPY adapter_model.safetensors /data/
```

## Image Platform

The adapter artifacts are not dependent on the operating system or architecture. Therefore, it is recommended that they be held in a platform-independent image to ensure interoperability between systems. Kaito can handle platform-specific images, but they must match the platform of the host system. As a rule of thumb, Kaito will be able to read any image that you can mount in your cluster using [K8s Image Volumes](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/#create-pod).

## Building Platform-Independent Images

Kaito builds platform-independent images in a few steps described at a high level below. The implementation leverages [ORAS](https://github.com/oras-project/oras) to simplify the process, and its source code can be found in [pusher.sh](./pusher.sh). 

1. Create the image layer by archiving `/data/adapter_config.json` and `/data/adapter_model.safetensors`.
2. Compute the diff ID by hashing the image layer.
3. Optionally compress the image layer.
4. Create the image configuration with the diff ID.
5. Optionally create the annotations file.
6. Build the image layout with the image layer, image configuration, and annotations file.

Keep in mind the following caveats when manually building platform-independent images:

- **DO NOT** add an [OCI Image Index](https://github.com/opencontainers/image-spec/blob/main/image-index.md) to the image. This will cause a platform mismatch when using [containerd](https://github.com/containerd/containerd) as the container runtime.
- **DO NOT** specify `archiecture` or `os` in the [OCI Image Configuration](https://github.com/opencontainers/image-spec/blob/main/config.md). Although these values are required by the OCI Specification, not specifying them allows the creation of platform-independent images accepted by [containerd](https://github.com/containerd/containerd), [cri-o](https://github.com/cri-o/cri-o), [ORAS](https://github.com/oras-project/oras), and [Skopeo](https://github.com/containers/skopeo).
- **DO** specify `rootfs.diff_ids` in the [OCI Image Configuration](https://github.com/opencontainers/image-spec/blob/main/config.md). Not specifying these values will prevent [containerd](https://github.com/containerd/containerd) from properly expanding the image.
