# Kaito RAG Engine Installation 

> Be sure you've cloned this repo and followed [kaito workspace installation](../installation.md)
```bash
helm install ragengine ./charts/kaito/ragengine --namespace kaito-ragengine --create-namespace
```

## Verify installation
You can run the following commands to verify the installation of the controllers were successful.

Check status of the Helm chart installations.

```bash
helm list -n kaito-ragengine
```

Check status of the `ragengine`.

```bash
kubectl describe deploy ragengine -n kaito-ragengine
```

## Clean up

```bash
helm uninstall kaito-ragengine
```