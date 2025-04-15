# fleague

```shell
kubectl create secret generic jpbot-api-secrets --dry-run=client --from-file=config.yml=config.production.yml -o yaml |
  kubeseal \
    --controller-name=sealed-secrets \
    --controller-namespace=kube-system \
    --format yaml >deployment/secret.yaml

```
