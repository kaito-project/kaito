#!/bin/bash

# Check AZURE_LOCATION environment variable
if [ "$AZURE_LOCATION" == "centraluseuap" ]; then
    echo "AZURE_LOCATION is centraluseuap, starting SKU replacement..."
    
    # Try to find the files even if script is run from different directory
    # First check if we're in repo root
    if [ -d "./test/e2e" ]; then
        E2E_DIR="./test/e2e"
    # Check if we're in test directory
    elif [ -d "../e2e" ]; then
        E2E_DIR="../e2e"
    # Check if we're in e2e directory
    elif [ -d "./" ] && [ -f "./preset_test.go" ]; then
        E2E_DIR="./"
    else
        echo "Cannot locate e2e directory, please run from repository root"
        exit 1
    fi
    
    # Define files using detected path
    file1="${E2E_DIR}/preset_vllm_test.go"
    file2="${E2E_DIR}/preset_test.go"
    
    echo "Using e2e directory: $E2E_DIR"
    
    # Process files
    for file in "$file1" "$file2"; do
        if [ -f "$file" ]; then
            echo "Processing file: $file"
            sed -i -E 's/"Standard_[^"]*"/"Standard_NC40ads_H100_v5"/g' "$file"
            echo "File $file processed"
        else
            echo "File not found: $file"
        fi
    done
    
    echo "SKU replacement completed!"
else
    echo "AZURE_LOCATION is not centraluseuap, no SKU replacement needed"
    echo "Current AZURE_LOCATION: $AZURE_LOCATION"
fi
if [ -z "${E2E_DIR}" ]; then
    if [ -d "./test/e2e" ]; then
        E2E_DIR="./test/e2e"
    elif [ -d "../e2e" ]; then
        E2E_DIR="../e2e"
    elif [ -d "./" ] && [ -f "./preset_test.go" ]; then
        E2E_DIR="./"
    else
        echo "Cannot locate e2e directory, please run from repository root"
        exit 1
    fi
    echo "Using e2e directory: $E2E_DIR"
fi

echo "Converting AZURE_CLUSTER_NAME to lowercase in preset_test.go..."
file="${E2E_DIR}/preset_test.go"
if [ -f "$file" ]; then
    grep -q "\"strings\"" "$file" || sed -i '1,20s/import (/import (\n\t"strings"/g' "$file"
    
    sed -i 's/azureClusterName = utils\.GetEnv("AZURE_CLUSTER_NAME")/azureClusterName = strings.ToLower(utils.GetEnv("AZURE_CLUSTER_NAME"))/g' "$file"
    echo "Variable conversion in $file completed"
else
    echo "File not found: $file"
fi

echo "All replacements completed!"