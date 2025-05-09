#export AZURE_SUBSCRIPTION_ID="""
#export AZURE_RESOURCE_GROUP="h100ResourceGroup"               
#export AZURE_CLUSTER_NAME="h100Cluster"        
#export AZURE_LOCATION="centraluseuap"
#export all the variables above

export AZURE_RESOURCE_GROUP_MC="MC_${AZURE_RESOURCE_GROUP}_${AZURE_CLUSTER_NAME}_${AZURE_LOCATION}"
export GPU_PROVISIONER_NAMESPACE="gpu-provisioner"
export KAITO_NAMESPACE="kaito-workspace"
export KAITO_RAGENGINE_NAMESPACE="kube-system"
export TEST_SUITE="gpuprovisioner" 
export AI_MODELS_REGISTRY="aimodelsregistrytest.azurecr.io"
export E2E_ACR_REGISTRY="aimodelsregistrytest.azurecr.io"

#export GINKGO_LABEL="FastCheck" 
#export Faskcheck if needed
export RUN_LLAMA_13B="false"
export ACR_NAME=$(echo ${AZURE_CLUSTER_NAME} | tr '[:upper:]' '[:lower:]')

az aks get-credentials --name ${AZURE_CLUSTER_NAME} --resource-group ${AZURE_RESOURCE_GROUP} --overwrite-existing

ACR_EXISTS=$(az acr check-name --name ${ACR_NAME} --query "nameAvailable" -o tsv)

if [ "$ACR_EXISTS" = "true" ]; then
    echo "Creating ACR named ${ACR_NAME}..."
    az acr create --resource-group ${AZURE_RESOURCE_GROUP} \
                 --name ${ACR_NAME} \
                 --sku Basic \
                 --admin-enabled true
else
    echo "ACR named ${ACR_NAME} already exists or name is invalid"
fi

export AI_MODELS_REGISTRY_SECRET="dummy-models-secret"
export E2E_ACR_REGISTRY_SECRET="dummy-acr-secret"

az acr login --name ${ACR_NAME}

ACR_USERNAME=${ACR_NAME}
ACR_PASSWORD=$(az acr credential show --name ${ACR_NAME} \
                                     --resource-group ${AZURE_RESOURCE_GROUP} \
                                     --query "passwords[0].value" -o tsv)



kubectl delete secret ${AI_MODELS_REGISTRY_SECRET} --ignore-not-found
kubectl create secret docker-registry ${AI_MODELS_REGISTRY_SECRET} \
  --docker-server=mcr.microsoft.com \
  --docker-username=dummy \
  --docker-password=dummy
kubectl delete secret ${E2E_ACR_REGISTRY_SECRET} --ignore-not-found
kubectl create secret docker-registry ${E2E_ACR_REGISTRY_SECRET} \
  --docker-server=${ACR_NAME}.azurecr.io \
  --docker-username=${ACR_NAME} \
  --docker-password=${ACR_PASSWORD}


export GINKGO_NODES=1
mkdir -p 

make kaito-workspace-e2e-test