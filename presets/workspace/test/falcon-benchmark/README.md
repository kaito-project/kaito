# Falcon Inference Benchmarking

Benchmark and analyze the performance of Falcon's model inference with our utilities.

## 📂 Contents

- **benchmark-inference.py**: Runs the inference benchmarking and outputs a CSV containing performance metrics.
- **plot.ipynb**: A Jupyter Notebook that visualizes the results of the benchmarking from the CSV.

## 🚀 Getting Started

### Assumptions
1. Starting Point: This guide assumes you are starting with a fresh AKS (Azure Kubernetes Service) cluster.
2. Model Download: If the required model is not already present, it will be downloaded automatically when the benchmarking script is executed.

### Prerequisites

Ensure your `accelerate` configuration aligns with the values provided during benchmarking. To check or set up your configuration, you can run the command: `accelerate config`.

### Setting up the Environment
1. Creating a GPU Node:
   - Before you can run the benchmarking on a GPU, ensure you have a GPU node set up in your AKS cluster.
   - If you haven't already, you can use the Azure CLI or the Azure Portal to create and configure a GPU node pool in your AKS cluster.
<!-- markdown-link-check-disable -->
2. Using or Building the Docker Image:
    - If the image is already hosted on MCR (Microsoft Container Registry), you can access it directly. Use the following format: `mcr.microsoft.com/aks/kaito/kaito-<MODEL_NAME>:<MODEL_VERSION>`
      - Example: `mcr.microsoft.com/aks/kaito/kaito-falcon-40b-instruct:0.0.7`
    - If you are using a private or custom image, you will need to build and push it to your own container registry. Use the following commands:
        ```
        docker build -t <PRIVATE_IMAGE> --file docker/presets/models/tfs/Dockerfile \
        --build-arg WEIGHTS_PATH=<PATH_TO_MODEL_WEIGHTS> \
        --build-arg MODEL_TYPE=text-generation \
        --build-arg VERSION=0.0.1
      
        docker push <PRIVATE_IMAGE>
        ```
3. Deploying a Pod with the Docker Image:
    - Deploy a pod in your AKS cluster using the image you just pushed.
    - Create a YAML file for the pod (e.g., falcon-gpu-pod.yaml). Here is an example YAML:

    ```YAML
    apiVersion: v1
    kind: Pod
    metadata:
      name: falcon-gpu-pod
    spec:
      containers:
      - name: falcon-gpu-container
        image: your_registry/falcon-gpu:latest
        resources:
          limits:
            nvidia.com/gpu: 1
    ```

    - Apply this configuration to deploy the pod:
    ```bash
    kubectl apply -f falcon-gpu-pod.yaml
    ```
4. Accessing the Pod:
    - Once the pod is up and running, you can use kubectl to SSH into it:
    ```bash
    kubectl exec -it falcon-gpu-pod -- /bin/bash
    ```
    - Inside the pod, you will have access to python, pip, and the necessary packages (accelerate, torch, and transformers) for your benchmarking tasks.

### Running the Benchmark
1. Copy the Benchmarking Script to the Pod: Before you run the benchmarking, ensure that the inference_benchmark.py script is present inside your pod. Use the following command to copy the script into your running pod:
  ```
  kubectl cp inference_benchmark.py falcon-gpu-pod:/path/in/pod/
  ```

2. Execute the Benchmarking Script
     - Before you run the benchmark, ensure that your runtime configuration is correctly set using the `accelerate config` command. The configuration set here will determine the behavior of the `accelerate launch` command.
        - **Important:** Parameters given to inference-benchmark.py during benchmarking are solely for logging and will be noted in the CSV for visualization. These parameters don't affect runtime but should match settings made with `accelerate config` for accurate benchmark tracking.
    - Once the script is inside the pod and your configuration is set, you can proceed with the benchmarking:
      - Navigate to the directory where you copied the script inside the pod
      - Run the benchmarking using the provided script and parameters. 
      ```bash
      accelerate launch inference-benchmark.py --model MODEL_NAME --num_nodes NODE_COUNT --num_processes PROCESS_COUNT --num_gpus GPU_COUNT --num_prompts PROMPT_COUNT --model_parallelism PARALLELISM_TYPE --data_parallelism DATA_PARALLELISM_TYPE --quantization QUANTIZATION_TYPE --machine MACHINE_TYPE
      ```
      
      For example:
      ```bash
      accelerate launch inference-benchmark.py --model falcon-40b --num_nodes 1 --num_processes 1 --num_gpus 1 --num_prompts 1 --model_parallelism deepspeed --data_parallelism none --quantization bf16 --machine Standard_NC96ads_A100_v4
      ```

### 📊 Visualizing the Results
- Open the `plot.ipynb` notebook in Jupyter. Ensure you have the necessary libraries installed. The notebook will guide you through loading the benchmark data and visualizing the results.
- Note: The notebook supports the visualization of results from multiple experiments, as shown in the provided snippets. Make sure to update the data source if you have benchmark results from other experiments or configurations.

### 📤 Output
- The benchmarking script (inference-benchmark.py) will produce a results.csv file containing the performance statistics of each inference request.
- The visualization notebook (plot.ipynb) provides plots that offer insights into the inference performance across various configurations and parameters.

### 📌 Notes
- Ensure that you have a file named requests.csv in the directory before running the benchmark. This file should contain the prompts for which you'd like to benchmark inference.
- The script and notebook are set up to work with models hosted on HuggingFace, specifically "tiiuae/falcon-7b-instruct" and "tiiuae/falcon-40b-instruct". Modify the model_id in the script if you want to benchmark a different model.

