# `kube-test-env` (KTE)

KTE is a Go library that makes it very easy to create a Kubernetes cluster from testing, so all you need do is run `go test`.
It avoids having to have shell scripts that manage kind clusters. Presently it implements only one provider - kind,
it may include other options in the future.

KTE does not require `kind` CLI installed, it just needs Docker.
