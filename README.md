# `kube-test-env` (KTE)

KTE is a Go library that makes it very easy to create a Kubernetes cluster from testing, so all you need do is run `go test`.
It avoids having to have shell scripts that manage kind clusters. Presently it implements only one provider - kind,
it may include other options in the future.

KTE does not require `kind` CLI installed, it just needs Docker.

It's common for test infra to be configured before tests run, that approach suffers from the following:
- setup contract is not explicit, it's often done via shell scripts and not an API
    - lack of abstraction makes it harder to migrate between infra providers
    - incidental features easy to add in a shell script
- the lifecycle of the infra is not bound to test runs
    - left-over state
    - test scenarios cannot define infra scale or isolation requirements
- configuration of the infra may not match expectations of the tests
    - leads to uncertainty and poor reliability
    - it's hard to evolve infra needs as tests evolve
