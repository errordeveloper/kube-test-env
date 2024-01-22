#!/bin/bash

set -o errexit
set -o pipefail
set -o nounset

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "${script_dir}"

flux_release="${1:-"latest"}"

if [ "${flux_release}" = "latest" ] ; then
  flux_release="$(curl --silent --fail --show-error https://api.github.com/repos/fluxcd/flux2/releases/latest | jq -r .tag_name)"
fi


for component in source-controller kustomize-controller helm-controller ; do
  docker run --rm "ghcr.io/fluxcd/flux-cli:${flux_release}" install \
    --toleration-keys=kubernetes.io/arch \
    --components="${component}" \
    --network-policy=false \
    --export > "${component}.yaml"
done
