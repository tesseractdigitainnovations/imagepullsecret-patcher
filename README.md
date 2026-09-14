# imagepullsecret-patcher

[![CI](https://github.com/tesseractdigitainnovations/imagepullsecret-patcher/actions/workflows/ci.yml/badge.svg)](https://github.com/tesseractdigitainnovations/imagepullsecret-patcher/actions/workflows/ci.yml)
[![Publish image](https://github.com/tesseractdigitainnovations/imagepullsecret-patcher/actions/workflows/release.yml/badge.svg)](https://github.com/tesseractdigitainnovations/imagepullsecret-patcher/actions/workflows/release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/titansoft-pte-ltd/imagepullsecret-patcher)](https://goreportcard.com/report/github.com/titansoft-pte-ltd/imagepullsecret-patcher)
![Go version](https://img.shields.io/github/go-mod/go-version/tesseractdigitainnovations/imagepullsecret-patcher)
![GitHub tag (latest SemVer)](https://img.shields.io/github/v/tag/tesseractdigitainnovations/imagepullsecret-patcher)

A small Kubernetes controller that creates an image pull secret in **every**
namespace and patches it onto service accounts, so that a whole cluster can pull
from a private container registry without every pod spec naming a secret.

![screenshot](doc/screenshot.png)

Background reading:
[Kubernetes cluster-wide access to a private container registry](https://medium.com/titansoft-engineering/kubernetes-cluster-wide-access-to-private-container-registry-with-imagepullsecret-patcher-b8b8fb79f7e5).

## How it works

Every `-loop-duration` (10 seconds by default) the patcher:

1. lists the namespaces, skipping the excluded ones;
2. makes sure the managed secret exists in each namespace and holds the current
   credential — creating it, updating it in place, or replacing it if its type
   is wrong;
3. lists the service accounts and patches the secret into their
   `imagePullSecrets`, keeping any entries that are already there.

The credential is re-read at the start of every loop, so rotating a mounted
secret is picked up without a restart. A namespace that fails is logged and
retried on the next loop; it does not stop the other namespaces.

## Install

The image is published to GitHub Container Registry for **linux/amd64**,
**linux/arm64** and **linux/arm/v7**:

```
ghcr.io/tesseractdigitainnovations/imagepullsecret-patcher:latest          # default branch
ghcr.io/tesseractdigitainnovations/imagepullsecret-patcher:<commit-sha>    # every build
ghcr.io/tesseractdigitainnovations/imagepullsecret-patcher:v1.2.3          # release tags
```

Prefer a commit sha or a release tag in production. Images carry a signed
[build provenance attestation](https://docs.github.com/actions/security-for-github-actions/using-artifact-attestations/using-artifact-attestations-to-establish-provenance-for-builds),
which you can verify with:

```shell
gh attestation verify oci://ghcr.io/tesseractdigitainnovations/imagepullsecret-patcher:latest \
  --owner tesseractdigitainnovations
```

To deploy, start from [deploy-example](deploy-example), which has the namespace,
RBAC and a hardened Deployment:

```shell
kubectl apply -k deploy-example/kubernetes-manifest/
```

## Configuration

Every setting can be given as a command-line flag or an environment variable;
the flag wins, then the environment variable, then the default.

| Flag                    | Environment variable          | Default             | Description                                                                        |
| ----------------------- | ----------------------------- | ------------------- | ---------------------------------------------------------------------------------- |
| `-force`                | `CONFIG_FORCE`                | `true`              | overwrite the managed secret when its content does not match                        |
| `-debug`                | `CONFIG_DEBUG`                | `false`             | show debug logs                                                                     |
| `-log-format`           | `CONFIG_LOG_FORMAT`           | `text`              | `text` or `json` structured output                                                  |
| `-managedonly`          | `CONFIG_MANAGEDONLY`          | `false`             | never touch a secret that this application did not create                           |
| `-runonce`              | `CONFIG_RUNONCE`              | `false`             | reconcile once and exit, for a `CronJob`                                             |
| `-serviceaccounts`      | `CONFIG_SERVICEACCOUNTS`      | `default`           | comma-separated service accounts to patch                                           |
| `-allserviceaccount`    | `CONFIG_ALLSERVICEACCOUNT`    | `false`             | patch every service account, ignoring `-serviceaccounts`                            |
| `-dockerconfigjson`     | `CONFIG_DOCKERCONFIGJSON`     | `""`                | the credential itself, exclusive with `-dockerconfigjsonpath`                        |
| `-dockerconfigjsonpath` | `CONFIG_DOCKERCONFIGJSONPATH` | `""`                | file holding the credential, re-read every loop — the recommended option            |
| `-secretname`           | `CONFIG_SECRETNAME`           | `image-pull-secret` | name of the managed secret                                                          |
| `-excluded-namespaces`  | `CONFIG_EXCLUDED_NAMESPACES`  | `""`                | comma-separated namespaces to leave alone                                           |
| `-loop-duration`        | `CONFIG_LOOP_DURATION`        | `10s`               | interval between reconciliations, any [Go duration](https://pkg.go.dev/time#ParseDuration) |
| `-kubeconfig`           | `KUBECONFIG`                  | `""`                | kubeconfig to use when running outside a cluster                                     |
| `-version`              |                               |                     | print the version and exit                                                          |

Annotations understood by the patcher:

| Annotation                                          | Object    | Effect                                                        |
| --------------------------------------------------- | --------- | ------------------------------------------------------------- |
| `k8s.titansoft.com/imagepullsecret-patcher-exclude` | namespace | set to `"true"` to skip the namespace                          |
| `app.kubernetes.io/managed-by`                      | secret    | set to `imagepullsecret-patcher` on the secrets it creates, and what `-managedonly` looks for |

### Providing the credential

Mount a secret and point `-dockerconfigjsonpath` at it, as
[deploy-example](deploy-example) does. Compared with `-dockerconfigjson`, a
mounted credential is not visible in the pod spec and can be rotated in place:
the next loop picks up the new value and updates every namespace.

### RBAC

The patcher needs, cluster-wide:

- `namespaces`: `get`, `list`
- `serviceaccounts`: `get`, `list`, `patch`
- `secrets`: `get`, `create`, `update`, `delete`

`delete` is only used to replace a secret of the wrong type, which cannot be
updated in place because a secret's type is immutable.

## Development

Requirements: Go 1.26 and, for images, Docker with buildx.

```shell
make test      # go test -race with a coverage profile
make lint      # golangci-lint
make check     # fmt, vet, lint and test — what CI runs
make build     # binary into dist/
make docker    # image for the host platform, named after your git remote
make help      # every target
```

Run it against the cluster in your current kubectl context — it falls back to
your kubeconfig when it is not running inside a cluster:

```shell
go run ./cmd/imagepullsecret-patcher -runonce -debug -dockerconfigjson '{"auths":{}}'
```

Layout:

```
cmd/imagepullsecret-patcher/   flag parsing, logging, client setup, signal handling
internal/config/               configuration from flags and the environment
internal/patcher/              the reconciliation loop, secrets and service accounts
deploy-example/                a working set of manifests
```

CI runs the tests, `golangci-lint` and a build of all three published platforms
on every pull request. Pushes to the default branch and `v*` tags publish the
multi-arch image; see [.github/workflows](.github/workflows).

## Why

To pull private images, Kubernetes needs the registry credential either in
[every pod spec](https://kubernetes.io/docs/concepts/containers/images/#specifying-imagepullsecrets-on-a-pod)
or on the
[service account](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#add-imagepullsecrets-to-a-service-account)
that the pod uses. The second option is invisible to developers — a pod
inherits the credential from its namespace — but it means an administrator
running this in every namespace, forever:

```shell
kubectl create secret docker-registry image-pull-secret \
  -n <namespace> \
  --docker-server=<your-registry-server> \
  --docker-username=<your-name> \
  --docker-password=<your-password> \
  --docker-email=<your-email>

kubectl patch serviceaccount default \
  -n <namespace> \
  -p '{"imagePullSecrets":[{"name":"image-pull-secret"}]}'
```

imagepullsecret-patcher does exactly that, on a loop, including for namespaces
created later.

## License

MIT — see [LICENSE](LICENSE).
