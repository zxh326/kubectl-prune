# kubectl-prune

## feature

skip default sa
skip docker pull secrets

## usage

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

need dry-run ?