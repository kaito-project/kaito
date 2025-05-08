export AZURE_SUBSCRIPTION_ID="mySubscriptionID"
export AZURE_RESOURCE_GROUP="myResourceGroup"
export AZURE_LOCATION="myLocation"
export CLUSTER_NAME="myClusterName"
az group create --name $AZURE_RESOURCE_GROUP --location $AZURE_LOCATION
az aks create --location $AZURE_LOCATION --resource-group $AZURE_RESOURCE_GROUP --name $CLUSTER_NAME --enable-oidc-issuer --enable-ai-toolchain-operator --generate-ssh-keys
az aks get-credentials --resource-group $AZURE_RESOURCE_GROUP --name $CLUSTER_NAME
kubectl get nodes
export MC_RESOURCE_GROUP=$(az aks show --resource-group $AZURE_RESOURCE_GROUP --name $CLUSTER_NAME --query nodeResourceGroup -o tsv)
export PRINCIPAL_ID=$(az identity show --name "ai-toolchain-operator-${CLUSTER_NAME}" --resource-group $MC_RESOURCE_GROUP --query 'principalId' -o tsv)
export KAITO_IDENTITY_NAME="ai-toolchain-operator-${CLUSTER_NAME}"
export AKS_OIDC_ISSUER=$(az aks show --resource-group $AZURE_RESOURCE_GROUP --name $CLUSTER_NAME --query "oidcIssuerProfile.issuerUrl" -o tsv)
az role assignment create --role "Contributor" --assignee $PRINCIPAL_ID --scope "/subscriptions/$AZURE_SUBSCRIPTION_ID/resourcegroups/$AZURE_RESOURCE_GROUP"
az identity federated-credential create --name "kaito-federated-identity" --identity-name $KAITO_IDENTITY_NAME -g $MC_RESOURCE_GROUP --issuer $AKS_OIDC_ISSUER --subject system:serviceaccount:"kube-system:kaito-gpu-provisioner" --audience api://AzureADTokenExchange
kubectl rollout restart deployment/kaito-gpu-provisioner -n kube-system
wait 30s
kubectl get deployment -n kube-system | grep kaito