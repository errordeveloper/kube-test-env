package flux

import (
	"bytes"
	_ "embed"
	"io"
)

var (
	//go:embed source-controller.yaml
	sourceControllerManifest []byte
	//go:embed helm-controller.yaml
	helmControllerManifest []byte
	//go:embed kustomize-controller.yaml
	kustomizeControllerManifest []byte
)

func SourceControllerManifests() io.Reader {
	return bytes.NewReader(sourceControllerManifest)
}

func HelmControllerManifests() io.Reader {
	return bytes.NewReader(helmControllerManifest)
}

func KustomizeControllerManifests() io.Reader {
	return bytes.NewReader(kustomizeControllerManifest)
}
