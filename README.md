# Dron8s

Yet another Kubernetes plugin for Drone using [dynamic](https://pkg.go.dev/k8s.io/client-go@v0.19.2/dynamic) [Server Side Apply](https://kubernetes.io/docs/reference/using-api/api-concepts/#server-side-apply) to achieve `kubectl apply -f` parity for your CI-CD pipelines.

## Features
* Create resources if they do not exist/update if they do
* Can handle multiple yaml configs in one file
* Can handle most resource types<sup>1</sup>
* In-cluster/Out-of-cluster use
* Easy set up, simple usage

_<sup>1</sup>Using client-go@v0.19.2. While common Kubernetes API will work with your cluster, some features will not. For more information check the [compatibility matrix](https://github.com/kubernetes/client-go#compatibility-matrix)._

# [in-cluster](https://github.com/kubernetes/client-go/tree/master/examples/in-cluster-client-configuration) use

In-cluster use is intented to only work along [Kubernetes Runner](https://docs.drone.io/runner/kubernetes/overview/) with in-cluster deployment scope. That is your pipelines can only create/patch resources within the cluster Kubernetes Runner is running.

## Prerequisites 
You need to manually create a `clusterrolebinding` to allow cluster resource manipulation from Drone server.

Assuming you installed Drone/Kubernetes Runner using [Drone provided Helm charts](https://github.com/drone/charts/tree/master/charts) run:
```bash
$ kubectl create clusterrolebinding dron8s --clusterrole=cluster-admin --serviceaccount=drone:default
```
_If you opted for manual installation you have to replace the `--serviceaccount` flag with the correct service name you used (ie. `--serviceaccount=drone-ci:default`)._


## Example 
```yaml
kind: pipeline
type: kubernetes
name: dron8s-in-cluster-example


steps:
- name: dron8s
  image: bh90210/dron8s:latest
  settings:
    yaml: ./config.yaml
```

# [out-of-cluster](https://github.com/kubernetes/client-go/tree/master/examples/out-of-cluster-client-configuration) use

For out-of-cluster use you can choose whichever [runner](https://docs.drone.io/runner/overview/) you prefer but you need to provide you cluster's `kubeconfig` via a secret.

## Prerequisites 
Create a secret with the contents of kubeconfig

1. gui
2. kubenrnetes secrets
3. encrypted

## Example 
```yaml
kind: pipeline
type: docker
name: dron8s-out-of-cluster-example


steps:
- name: dron8s
  image: bh90210/dron8s:latest
  settings:
    yaml: ./config.yaml
    kubeconfig:
        from_secret:
            secret
```

# In-cluster Uninstall

The only thing you need to manually delete is the `clusterrolebinding`. Run:

```bash
$ kubectl delete clusterrolebinding dron8s
```

# Developing

If you wish you can directly edit `.drone.yaml` as everything you need for the build is right there.

Otherwise:

```bash
$ git clone github.com/bh90210/dron8s

$ GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dron8s

$ docker build -t {username}/dron8s .

$ docker push {username}/dron8s
```
And to use your own repo inside Drone just change the `image` field to your `{username}/dron8s`
```yaml
kind: pipeline
type: docker
name: default

steps:
- name: dron8s
  image: {username}/dron8s
  settings:
    yaml: ./config
```
_Replace `{username}` with your actual Docker Hub (or other registry) username._

_For more information see Drone's [Plugin Documentation](https://docs.drone.io/plugins/tutorials/golang/)._

