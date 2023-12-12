package kind

import (
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	klog "k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

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

	NodeImage   string
	ArtifactDir string
	Logger      klog.Logger
}

func New(artifactDir string, logger klog.Logger) *Kind {

	logAdapter := &log.Adapter{logger.WithName("kind")}
	uuid := uuid.New()
	return &Kind{
		UUID:        uuid,
		ArtifactDir: artifactDir,
		Logger:      logger.WithName("kind-provider").WithValues("kind-provider-uuid", uuid.String()),
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
	options := []cluster.CreateOption{
		cluster.CreateWithKubeconfigPath(k.KubeConfigPath()),
		cluster.CreateWithDisplayUsage(false),
		cluster.CreateWithDisplaySalutation(false),
		cluster.CreateWithWaitForReady(timeout),
	}
	if config != nil {
		options = append(options, cluster.CreateWithV1Alpha4Config(config))
	}
	if k.NodeImage != "" {
		options = append(options, cluster.CreateWithNodeImage(k.NodeImage))
	}
	return k.Provider.Create(k.ClusterName(), options...)
}

func (k *Kind) CollectLogs() error {
	return k.Provider.CollectLogs(k.ClusterName(), k.LogsDir())
}

func (k *Kind) NewControllerManager() (ctrl.Manager, error) {
	clientConfig, err := k.NewClientConfig()
	if err != nil {
		return nil, err
	}
	logger := k.Logger.WithValues("kind-provider-uuid", k.UUID.String())
	ctrl.SetLogger(logger.WithName("kte-controller-runtime"))
	options := ctrl.Options{
		Logger: logger.WithName("kte-controller-manager"),
	}
	return ctrl.NewManager(clientConfig, options)
}

func (k *Kind) NewClientConfig() (*rest.Config, error) {
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: k.KubeConfigPath(),
		},
		&clientcmd.ConfigOverrides{
			CurrentContext: Name + "-" + k.ClusterName(),
		})

	return loader.ClientConfig()
}

func (k *Kind) Delete() error {
	return k.Provider.Delete(k.ClusterName(), k.KubeConfigPath())
}
