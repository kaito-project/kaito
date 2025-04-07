# Use the MCR PyTorch image as a base
FROM mcr.microsoft.com/aifx/acpt/stable-ubuntu2004-cu118-py38-torch211

ARG WEIGHTS_PATH
ARG MODEL_TYPE
ARG VERSION

# Set the working directory
WORKDIR /workspace/tfs

# Write the version to a file
RUN echo $VERSION > /workspace/tfs/version.txt

# First, copy just the requirements.txt file and install dependencies
# This is done before copying the code to utilize Docker's layer caching and
# avoid reinstalling dependencies unless the requirements file changes.
COPY kaito/presets/workspace/inference/${MODEL_TYPE}/requirements.txt /workspace/tfs/requirements.txt
RUN pip install --no-cache-dir -r requirements.txt

COPY kaito/presets/workspace/inference/${MODEL_TYPE}/inference_api.py /workspace/tfs/inference_api.py

# Convert to ONNX Runtime
# RUN python convert_to_onnx.py ${MODEL_NAME} 

# Copy the entire model weights to the weights directory
COPY ${WEIGHTS_PATH} /workspace/tfs/weights
