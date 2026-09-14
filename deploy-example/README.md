# deploy-example

A minimal, working deployment of imagepullsecret-patcher.

```
0_namespace.yaml    the imagepullsecret-patcher namespace
1_rbac.yaml         service account, ClusterRole and binding
2_deployment.yaml   the source credential secret and the Deployment
kustomization.yaml  optional: apply everything and override the image in one place
```

## 1. Create the source credential

The patcher copies one credential into every namespace. Create it in the
patcher's own namespace, where it is read from a mounted volume:

```shell
kubectl create namespace imagepullsecret-patcher

kubectl create secret docker-registry image-pull-secret-src \
  -n imagepullsecret-patcher \
  --docker-server=<your-registry-server> \
  --docker-username=<your-name> \
  --docker-password=<your-password> \
  --docker-email=<your-email>
```

That replaces the placeholder secret in
[2_deployment.yaml](kubernetes-manifest/2_deployment.yaml), so delete that
`Secret` object from the file if you create the secret with `kubectl`. Otherwise
edit its `.dockerconfigjson` value, which is a base64-encoded
[docker config json](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/#create-a-secret-by-providing-credentials-on-the-command-line).

A mounted secret is the recommended way to supply the credential: it can be
rotated without restarting the patcher, and it never appears in the pod spec.

## 2. Choose the image

The manifests reference `ghcr.io/tesseractdigitainnovations/imagepullsecret-patcher:latest`.
Pin it to a commit sha for a reproducible rollout:

```shell
cd kubernetes-manifest
kustomize edit set image \
  ghcr.io/tesseractdigitainnovations/imagepullsecret-patcher=ghcr.io/<owner>/imagepullsecret-patcher:<commit-sha>
```

## 3. Apply

```shell
kubectl apply -k kubernetes-manifest/   # with kustomize
kubectl apply -f kubernetes-manifest/   # or plain manifests
```

Then check that it is working:

```shell
kubectl -n imagepullsecret-patcher logs -l app.kubernetes.io/name=imagepullsecret-patcher -f

# the secret should now exist in every namespace
kubectl get secret image-pull-secret --all-namespaces

# and be referenced by the service accounts
kubectl get serviceaccount default -o jsonpath='{.imagePullSecrets}'
```

## What the example configures

| Setting                                     | Why                                                                              |
| ------------------------------------------- | -------------------------------------------------------------------------------- |
| `-dockerconfigjsonpath=/secrets/...`        | reads the credential from the mounted secret, so rotation needs no restart        |
| `-allserviceaccount`                        | patches every service account, not just `default`                                 |
| `-loop-duration=1m`                         | reconciles once a minute instead of every 10 seconds                              |
| `-log-format=json`                          | structured logs for a log collector                                               |
| `GOMEMLIMIT=56MiB`                          | keeps the Go heap under the 64Mi limit so the pod is not OOM-killed               |
| `readOnlyRootFilesystem`, `runAsNonRoot`, … | the image needs no writable filesystem, no root and no capabilities               |
| `strategy: Recreate`                        | avoids two patchers writing the same secrets during a rollout                     |

To exclude a namespace, either annotate it or list it on the command line:

```shell
kubectl annotate namespace kube-system k8s.titansoft.com/imagepullsecret-patcher-exclude=true
# or: -excluded-namespaces=kube-system,kube-public
```

## Running it as a CronJob instead

With `-runonce` the patcher reconciles once and exits, which fits a `CronJob`
if you would rather not keep a pod running:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: imagepullsecret-patcher
  namespace: imagepullsecret-patcher
spec:
  schedule: "*/10 * * * *"
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      backoffLimit: 2
      template:
        spec:
          restartPolicy: OnFailure
          serviceAccountName: imagepullsecret-patcher
          containers:
            - name: imagepullsecret-patcher
              image: ghcr.io/tesseractdigitainnovations/imagepullsecret-patcher:latest
              args: ["-runonce", "-allserviceaccount", "-dockerconfigjsonpath=/secrets/.dockerconfigjson"]
              volumeMounts:
                - name: src-dockerconfigjson
                  mountPath: /secrets
                  readOnly: true
          volumes:
            - name: src-dockerconfigjson
              secret:
                secretName: image-pull-secret-src
```

A failed single run exits non-zero, so the job is retried and shows up as
failed in the cluster.
