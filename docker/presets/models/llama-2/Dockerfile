# Build Llama model
# docker build -t $ACR_NAME.azurecr.io/{{IMAGE_NAME}}:$IMAGE_TAG \
#              --build-arg WEIGHTS_PATH=/weights \
#              --build-arg VERSION={{VERSION}} \
#              --build-arg MODEL_TYPE={{MODEL_TYPE}} \

FROM python:3.9-slim@sha256:caaf1af9e23adc6149e5d20662b267ead9505868ff07c7673dc4a7166951cfea
WORKDIR /workspace

# Install git
RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y --no-install-recommends git && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

RUN git clone https://github.com/facebookresearch/llama.git

WORKDIR /workspace/llama

RUN ["/bin/bash", "-c", "sed -i $'/torch.distributed.init_process_group(\"nccl\")/c\\            import datetime\\\n            torch.distributed.init_process_group(\"nccl\", timeout=datetime.timedelta(days=365*100))' /workspace/llama/llama/generation.py"]

RUN pip install -e .
RUN pip install torch==2.1.0 fastapi==0.109.1 pydantic==1.10.9 gputil==1.4.0
RUN pip install 'uvicorn[standard]'

ARG WEIGHTS_PATH
ARG MODEL_TYPE
ARG VERSION
# Write the version to a file
RUN echo $VERSION > /workspace/llama/version.txt

ADD ${WEIGHTS_PATH} /workspace/llama/llama-2/weights
ADD kaito/presets/workspace/inference/${MODEL_TYPE} /workspace/llama/llama-2
