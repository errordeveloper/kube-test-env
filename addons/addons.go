package addons

import (
	"context"
	"io"
	"time"

	"github.com/errordeveloper/kube-test-env/addons/flux"
	"github.com/errordeveloper/kube-test-env/clients"
)

type Config struct {
	FluxComponents FluxComponentsConfig
}

type FluxComponentsConfig struct {
	SourceController, HelmController, KustomizeController bool
}

var WaitOptions = clients.WaitOptions{
	Interval: 2 * time.Second,
	Timeout:  time.Minute,
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

	_, err := rm.ApplyManifest(ctx, &WaitOptions, io.MultiReader(manifests...))
	return err
}
