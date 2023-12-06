package kind

import (
	"path/filepath"
	"time"

	"github.com/google/uuid"

	klog "k8s.io/klog/v2"

	"sigs.k8s.io/kind/pkg/cluster"

	configv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"github.com/errordeveloper/kube-test-env/provider/kind/log"
)

const (
	Name              = "kind"
	ClusterNamePrefix = "kte-"
)

type Kind struct {
	UUID uuid.UUID
	*cluster.Provider

	ArtifactDir string
	Logger      klog.Logger
}

func New(artifactDir string, logger klog.Logger) *Kind {
	logAdapter := &log.Adapter{logger.WithName("kind")}

	return &Kind{
		UUID:        uuid.New(),
		ArtifactDir: artifactDir,
		Logger:      logger.WithName("kind-provider"),
		Provider:    cluster.NewProvider(cluster.ProviderWithLogger(logAdapter)),
	}
}

func (k *Kind) ClusterName() string {
	return ClusterNamePrefix + k.UUID.String()
}

func (k *Kind) KubeConfigPath() string {
	return filepath.Join(k.ArtifactDir, k.ClusterName(), "kubeconfig")
}

func (k *Kind) LogsDir() string {
	return filepath.Join(k.ArtifactDir, k.ClusterName(), "logs")
}

func (k *Kind) Create(config *configv1alpha4.Cluster, timeout time.Duration) error {
	return k.Provider.Create(
		k.ClusterName(),
		//cluster.CreateWithV1Alpha4Config(config),
		cluster.CreateWithKubeconfigPath(k.KubeConfigPath()),
		cluster.CreateWithDisplayUsage(false),
		cluster.CreateWithDisplaySalutation(false),
		cluster.CreateWithWaitForReady(timeout),
	)
}

func (k *Kind) CollectLogs() error {
	return k.Provider.CollectLogs(k.ClusterName(), k.LogsDir())
}

func (k *Kind) Delete() error {
	return k.Provider.Delete(k.ClusterName(), k.KubeConfigPath())
}
