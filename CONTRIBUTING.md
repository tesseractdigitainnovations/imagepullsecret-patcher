# Contributing

## Getting set up

You need Go 1.26. For building images you also need Docker with the buildx
plugin; everything else is in the standard toolchain.

```shell
git clone https://github.com/tesseractdigitainnovations/imagepullsecret-patcher.git
cd imagepullsecret-patcher
make test
```

`make help` lists every target. The useful ones:

| Target                  | What it does                                                     |
| ----------------------- | ---------------------------------------------------------------- |
| `make test`             | tests with the race detector and a coverage profile               |
| `make cover`            | per-function coverage from that profile                           |
| `make lint`             | `golangci-lint run`, using [.golangci.yml](.golangci.yml)         |
| `make check`            | format, vet, lint and test — the same gates as CI                 |
| `make build`            | build the binary into `dist/`                                     |
| `make docker`           | build the image for your platform, named after your git remote    |
| `make docker-multiarch` | build all published platforms (`PUSH=1` to push)                  |

`golangci-lint` is not vendored; install it from
[the releases](https://github.com/golangci/golangci-lint/releases) or with
`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`.

## Trying a change against a real cluster

The patcher uses the in-cluster config when it finds one, and otherwise falls
back to your kubeconfig, so it can be run straight from a workstation. Use
`-runonce` so it does not sit in a loop, and point it at a scratch cluster
(kind, minikube, k3d) rather than anything you care about — it writes secrets
into every namespace:

```shell
go run ./cmd/imagepullsecret-patcher \
  -runonce -debug \
  -dockerconfigjson '{"auths":{}}' \
  -excluded-namespaces kube-system,kube-public
```

## Code layout

```
cmd/imagepullsecret-patcher/   flags, logging, Kubernetes client, signals
internal/config/               configuration from flags and the environment
internal/patcher/              the reconciliation loop
deploy-example/                example manifests
```

Some conventions worth keeping:

- **Logging** is `log/slog`, with the namespace attached as an attribute rather
  than formatted into the message.
- **Errors** are returned and wrapped with `%w`, aggregated with `errors.Join`
  where one failure should not stop the rest of a loop. Nothing panics: a
  long-running controller should log and retry.
- **The Kubernetes client** is only reached through the `kubernetes.Interface`
  on `Patcher`, which is what lets the tests use `fake.NewClientset`.
- **Every API call takes the context** so that a shutdown signal interrupts an
  in-flight request.

## Tests

Tests are table-driven, use subtests, and drive the fake clientset rather than
mocking. New behaviour needs a case; a bug fix needs a case that fails before
the fix. `make test` must pass with the race detector.

## Pull requests

- Keep `go.mod` tidy: CI fails if `go mod tidy` changes anything.
- `make check` should be clean before you push.
- Kubernetes dependencies (`k8s.io/*`) move together — Dependabot groups them,
  so please do the same when bumping them by hand.

## Releasing

Pushing a `v*` tag builds and publishes the multi-arch image to
`ghcr.io/<owner>/<repo>` and opens a GitHub release with generated notes:

```shell
git tag v0.15
git push origin v0.15
```

Every push to the default branch also publishes `latest` and a commit-sha tag,
so there is no need to tag just to get an image.
