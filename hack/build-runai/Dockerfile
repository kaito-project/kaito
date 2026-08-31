# syntax=docker/dockerfile:1.7

ARG PYTHON_IMAGE=python:3.12.11-slim-bookworm

FROM ${PYTHON_IMAGE} AS wheelhouse

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /build
COPY requirements.txt .
RUN python -m pip download \
        --dest /wheelhouse \
        --no-deps \
        --index-url https://download.pytorch.org/whl/cpu/ \
        torch==2.6.0+cpu \
    && python -m pip download \
        --dest /wheelhouse \
        --find-links=/wheelhouse \
        --requirement requirements.txt

FROM ${PYTHON_IMAGE}

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates libgomp1 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=wheelhouse /wheelhouse /wheelhouse
COPY requirements.txt /tmp/requirements.txt
COPY wheels/ /tmp/runai-wheels/
RUN python -m pip install \
        --no-index \
        --find-links=/wheelhouse \
        --requirement /tmp/requirements.txt \
    && if find /tmp/runai-wheels -maxdepth 1 -type f -name 'runai_model_streamer*.whl' | grep --quiet .; then \
        python -m pip install \
            --no-deps \
            --force-reinstall \
            /tmp/runai-wheels/runai_model_streamer*.whl; \
    fi \
    && rm -rf /wheelhouse /tmp/requirements.txt /tmp/runai-wheels

COPY verify.py /usr/local/bin/verify-runai.py
COPY model_warmer.py /usr/local/bin/dacs-model-warmer.py
RUN python -m py_compile /usr/local/bin/dacs-model-warmer.py \
    && rm -rf /usr/local/bin/__pycache__

USER 65532:65532
ENTRYPOINT ["python", "/usr/local/bin/verify-runai.py"]
