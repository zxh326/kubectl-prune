# kubectl-prune

## Install

```shell
VERSION=0.0.1
curl -o kubectl-prune.tar.gz -Lf https://github.com/zxh326/kubectl-prune/releases/download/v${VERSION}/kubectl-prune-v${VERSION}-$(go env GOOS)-amd64.tar.gz
tar xf kubectl-prune.tar.gz
cp kubectl-prune-v${VERSION}-$(go env GOOS)-amd64/kubectl-prune $GOPATH/bin/
```

## feature

prune unused k8s resources

support

- [x] configmap
- [x] secrets
- [x] serviceAccount
- [ ] death pods
- [ ] 0 replicas deployment
- [ ] replicaset
- [ ] service
- [ ] custom crds

## usage

(can use --dry-run flag to test before delete)

- ### delete one kind with confirm

```bash
$ k prune cm
delete configmap/unsed-map (y/n) y
delete configMap/usused-map2 (y/m) n

deleted configMap/unsed-map
```

- ### delete suck kind with confirm

```bash
$ k prune cm,secrets

...
```

- ### delete all kind with confirm

```bash
k prune —all
```

- ### delete with no confirm

```bash
$ k prune cm -y

delete cm/test2
delete cm/test
```

- ### delete with all namespaces (should ignore kube-system)

```bash
$ k prune —-all-namespaces
```
