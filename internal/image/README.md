# KAITO Adapter Image Format Specification

This document describes the specification for KAITO-compatible adapter images. It assumes the reader is familiar with the [OCI Image Format Specification](https://github.com/opencontainers/image-spec).

## Image Layout

A KAITO-compatible adapter image consists of any OCI image with the file structure below. Files added to the image but not specified here are simply ignored. The image can use any base and have as many layers as needed.
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

## Building Platform-Independent Images

To build a platform-independent image compatible with KAITO, you can leverage [pusher.sh](pusher.sh). Simply run `./pusher.sh ${PATH_TO_DATA_DIRECTORY} ${IMAGE_REFERENCE}` to build and push a platform-independent image containing the files in the specified directory.

The following example pushes `~/Documents/kaito/adapter/data` to `ghcr.io/kaito-project/kaito/adapter` with tag `1.2.3`:

```bash
./pusher.sh ~/Documents/kaito/adapter/data ghcr.io/kaito-project/kaito/adapter:1.2.3
```
