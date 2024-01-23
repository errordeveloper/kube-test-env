package addons

import (
	"context"
	"io"

	"github.com/errordeveloper/kube-test-env/addons/flux"
	"github.com/errordeveloper/kube-test-env/clients"
)

type Config struct {
	FluxComponents FluxComponentsConfig
}

type FluxComponentsConfig struct {
	SourceController, HelmController, KustomizeController bool
}

func Apply(ctx context.Context, rm *clients.ResourceManager, config Config) error {
	var manifests []io.Reader

	if config.FluxComponents.SourceController {
		manifests = append(manifests, flux.SourceControllerManifests())
	}
	if config.FluxComponents.HelmController {
		manifests = append(manifests, flux.HelmControllerManifests())
	}
	if config.FluxComponents.KustomizeController {
		manifests = append(manifests, flux.KustomizeControllerManifests())
	}

	return rm.ApplyManifest(ctx, io.MultiReader(manifests...))
}
