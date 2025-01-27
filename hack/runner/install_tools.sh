#!/bin/bash

# Script to set up a new runner with necessary tools

# Update and upgrade the package list
sudo apt-get update && sudo apt-get upgrade -y

# Install Azure CLI
curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash

# Install make
sudo apt-get install -y make

# Install git
sudo apt-get install -y git

# Install Docker
sudo apt-get remove -y docker docker-engine docker.io containerd runc
sudo apt-get update
sudo apt-get install -y apt-transport-https ca-certificates curl software-properties-common
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io

# Enable Docker Buildx
sudo docker buildx create --use

# Install yq version 4.20.2
sudo wget https://github.com/mikefarah/yq/releases/download/v4.20.2/yq_linux_amd64 -O /usr/bin/yq
sudo chmod +x /usr/bin/yq

# Install jq-1.7
sudo wget https://github.com/stedolan/jq/releases/download/jq-1.7/jq-linux64 -O /usr/bin/jq
sudo chmod +x /usr/bin/jq

# Install gettext
sudo apt-get install -y gettext

# Install kubectl
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
rm kubectl

# Verify installations
echo "Verifying installations..."
az version
make --version
git --version
docker --version
docker buildx version
yq --version
jq --version
gettext --version
kubectl version --client

echo "Installation completed!"
