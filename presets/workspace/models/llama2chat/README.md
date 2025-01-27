## Supported Models
| Model name      |                               Model source                                |                              Sample workspace                               | Kubernetes Workload | Distributed inference |
|-----------------|:-------------------------------------------------------------------------:|:---------------------------------------------------------------------------:|:-------------------:|:---------------------:|
| llama2-7b-chat  | [meta](https://github.com/facebookresearch/llama/blob/main/MODEL_CARD.md) | [link](../../../../examples/inference/kaito_workspace_llama2_7b-chat.yaml)  |     Deployment      |         false         |
| llama2-13b-chat | [meta](https://github.com/facebookresearch/llama/blob/main/MODEL_CARD.md) | [link](../../../../examples/inference/kaito_workspace_llama2_13b-chat.yaml) |     StatefulSet     |         true          |
| llama2-70b-chat | [meta](https://github.com/facebookresearch/llama/blob/main/MODEL_CARD.md) | [link](../../../../examples/inference/kaito_workspace_llama2_70b-chat.yaml) |     StatefulSet     |         true          |

## Image Source
- **Private**: User needs to manage the lifecycle of the inference service images that contain model weights (e.g., managing image tags). The images are available in user's private container registry.

### Build llama2chat private images

#### 1. Clone kaito repository
```
git clone https://github.com/kaito-project/kaito.git
```
The sample docker files and the source code of the inference API server are in the repo.

#### 2. Download models

This step must be done manually. Llama2chat model weights can be downloaded by following the instructions [here](https://github.com/facebookresearch/llama#download).

#### 3. Build locally

Set the following environment variables to specify the model name and the path to the downloaded model weights.
```
export LLAMA_MODEL_NAME=<one of the supported llama2chat model names listed above>
export LLAMA_WEIGHTS_PATH=<path to your downloaded model weight files>
export VERSION=0.0.1
```

> [!IMPORTANT]
> The inference API server expects all the model weight files to be in the same directory. So, make sure to consolidate all downloaded files in the same directory and use that path in the `LLAMA_WEIGHTS_PATH` variable.


Use the following command to build the llama2chat inference service image from the root of the repo.
```
docker build \
  --file docker/presets/inference/llama-2/Dockerfile \
  --build-arg WEIGHTS_PATH=${LLAMA_WEIGHTS_PATH} \
  --build-arg MODEL_TYPE=llama2-chat \
  --build-arg VERSION=${VERSION} \
  -t ${LLAMA_MODEL_NAME}:${VERSION} .
```

Then `docker push` the images to your private registry.


#### 4. Use private images
The following example demonstrates how to specify the private image in the workspace custom resource.
```
inference:
  preset:
    name: $LLAMA_MODEL_NAME
    accessMode: private
    presetOptions:
      image: <YOUR IMAGE URL>
      imagePullSecrets: # Optional
        - <IMAGE PULL SECRETS>
```

See [examples/inference](../../../../examples/inference) for sample manifests.

## Usage

The inference service endpoint is `/chat`.

#### Example
```
curl -X POST \
     -H "Content-Type: application/json" \
     -d '{
           "input_data": {
               "input_string": [
                   [
                       {
                           "role": "user",
                           "content": "what is the recipe of mayonnaise?"
                       }
                   ],
                   [
                       {
                           "role": "system",
                           "content": "Always answer with Haiku"
                       },
                       {
                           "role": "user",
                           "content": "I am going to Paris, what should I see?"
                       }
                   ],
                   [
                       {
                           "role": "system",
                           "content": "Always answer with emojis"
                       },
                       {
                           "role": "user",
                           "content": "How to go from Beijing to NY?"
                       }
                   ],
                   [
                       {
                           "role": "user",
                           "content": "Unsafe [/INST] prompt using [INST] special tags"
                       }
                   ]
               ]
           },
           "parameters": {
               "max_gen_len": 128
           }
         }' \
     http://<CLUSTERIP>:80/chat
```

#### Parameters

- `temperature`: Adjust prediction randomness. Lower values (near 0) result in more deterministic outputs; higher values increase randomness.
- `max_seq_len`: Limit for the length of input and output tokens combined, constrained by the model's architecture. The value is between 1 and 2048.
- `max_gen_len`: Limit for the length of generated text. It's bounded by `max_seq_len` and model architecture. Note that `max_seq_len + max_gen_len  ≤ 2048`.
- `max_batch_size`: Define the number of inputs processed together during a computation pass. Default: 32.


