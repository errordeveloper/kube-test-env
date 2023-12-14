package kind

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"k8s.io/apimachinery/pkg/runtime"
	clientgo "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	klog "k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/kind/pkg/cluster"

	configv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"github.com/errordeveloper/kube-test-env/provider/kind/log"
)

const (
	Name              = "kind"
	ClusterNamePrefix = "kte-"

	EnvForceIsolated    = "KTE_FORCE_ISOLATED"
	EnvForceIsolatedAll = "all"

	EnvForcePreexisting       = "KTE_FORCE_PREEXISTING"
	EnvForcePreexistingAll    = "all"
	EnvForcePreexistingShared = "shared"

	EnvPreexitstingKubeconfig = "KTE_PREEXISTING_KUBECONFIG"
)

type Kind struct {
	UUID uuid.UUID
	*cluster.Provider

	prexistingKubeconfig *string

	NodeImage string
	Retain    bool

	ArtifactDir string
	Logger      klog.Logger
}

type (
	Cluster    = configv1alpha4.Cluster
	Node       = configv1alpha4.Node
	Networking = configv1alpha4.Networking
)

const (
	ControlPlaneRole = configv1alpha4.ControlPlaneRole
	WorkerRole       = configv1alpha4.WorkerRole
)

var shared = struct {
	once *sync.Once
	k    *Kind
}{
	once: &sync.Once{},
}

var Log = klog.NewKlogr()

func init() {
	ctrl.SetLogger(Log.WithName("kte-controller-runtime"))
}

func Shared(logger klog.Logger) (*Kind, error) {
	var initErr error
	shared.once.Do(func() {
		if preexsting := newPreexistingFromEnv(logger, false); preexsting != nil {
			shared.k = preexsting
			return
		}
		artifactDir, err := os.MkdirTemp("", "kte-kind-shared-provider-")
		if err != nil {
			initErr = err
			return
		}
		shared.k = New(artifactDir, logger.WithName("kind-shared-provider"))
	})
	if initErr != nil {
		return nil, initErr
	}

	if v, ok := os.LookupEnv(EnvForceIsolated); ok && v != EnvForceIsolatedAll {
		logger.Info("using isolated provider as '" + EnvForceIsolated + "=" + EnvForceIsolatedAll + "' was set")
		artifactDir, err := os.MkdirTemp("", "kte-kind-shared-provider-")
		if err != nil {
			return nil, err
		}
		return New(artifactDir, logger), nil
	}

	logger.Info("using shared provider", "kind-provider-uuid", shared.k.UUID.String())

	if shared.k == nil {
		return nil, fmt.Errorf("shared provider '%s' not initialized", Name)
	}
	return shared.k, nil
}

func New(artifactDir string, logger klog.Logger) *Kind {
	if preexsting := newPreexistingFromEnv(logger, false); preexsting != nil {
		return preexsting
	}

	logAdapter := &log.Adapter{logger.WithName("kind")}
	uuid := uuid.New()
	return &Kind{
		UUID:        uuid,
		ArtifactDir: artifactDir,
		Logger:      logger.WithName("kind-provider").WithValues("kind-provider-uuid", uuid.String()),
		Provider:    cluster.NewProvider(cluster.ProviderWithLogger(logAdapter)),
	}
}

func newPreexisting(logger klog.Logger, preexistingKubeconfig string) *Kind {
	return &Kind{
		UUID:                 uuid.New(),
		prexistingKubeconfig: &preexistingKubeconfig,
		Logger:               logger,
	}
}

func newPreexistingFromEnv(logger klog.Logger, shared bool) *Kind {
	forcePrexisting, haveForcePrexisting := os.LookupEnv(EnvForcePreexisting)
	preexistingKubeconfig, havePreexistingKubeconfig := os.LookupEnv(EnvPreexitstingKubeconfig)

	switch {
	case !haveForcePrexisting && !havePreexistingKubeconfig:
		return nil

	case haveForcePrexisting && !havePreexistingKubeconfig:
		logger.Info("cannot use pre-exising cluster" +
			" as only'" + EnvForcePreexisting + "=" + forcePrexisting + "' was set" +
			" but '" + EnvPreexitstingKubeconfig + "' wasn't")
		return nil

	case !haveForcePrexisting && havePreexistingKubeconfig:
		logger.Info("cannot use pre-exising cluster" +
			" as only'" + EnvPreexitstingKubeconfig + "=" + preexistingKubeconfig + "' was set" +
			" but '" + EnvForcePreexisting + "' wasn't")
		return nil

	default:
		switch forcePrexisting {
		case EnvForcePreexistingAll:
			return newPreexisting(logger.WithName("kind-prexisting-all"), preexistingKubeconfig)
		case EnvForcePreexistingShared:
			if shared {
				logger.Info("not using pre-exising shared cluster as '" + EnvForcePreexisting + "=" + EnvForcePreexistingShared + "' was set, it needs to be explicitly set to '" + EnvForcePreexistingAll + "'")
				return nil
			}
			return newPreexisting(logger.WithName("kind-prexisting-shared"), preexistingKubeconfig)
		default:
			logger.Info("not using pre-exising cluster as '" + EnvForcePreexisting + "=" + forcePrexisting + "' was set and it's unsupported")
			return nil
		}
	}
}

func (k *Kind) ClusterName() string {
	return ClusterNamePrefix + k.UUID.String()
}

func (k *Kind) KubeConfigPath() string {
	if k.prexistingKubeconfig != nil {
		return *k.prexistingKubeconfig
	}
	return filepath.Join(k.ArtifactDir, k.ClusterName(), "kubeconfig")
}

func (k *Kind) LogsDir() string {
	return filepath.Join(k.ArtifactDir, k.ClusterName(), "logs")
}

func (k *Kind) Create(config *Cluster, timeout time.Duration) error {
	if k.shouldBypass() {
		return nil
	}
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
	if k.Retain {
		options = append(options, cluster.CreateWithRetain(true))
	}
	return k.Provider.Create(k.ClusterName(), options...)
}

func (k *Kind) CollectLogs() error {
	if k.shouldBypass() {
		return nil
	}
	return k.Provider.CollectLogs(k.ClusterName(), k.LogsDir())
}

func (k *Kind) NewControllerRuntimeClient() (ctrlClient.Client, error) {
	options := ctrlClient.Options{
		Scheme: runtime.NewScheme(),
	}

	if err := clientgoscheme.AddToScheme(options.Scheme); err != nil {
		return nil, err
	}

	clientConfig, err := k.NewClientConfig()
	if err != nil {
		return nil, err
	}

	return ctrlClient.New(clientConfig, options)
}

func (k *Kind) NewClientSet() (clientgo.Interface, error) {
	clientConfig, err := k.NewClientConfig()
	if err != nil {
		return nil, err
	}
	clientConfig.WarningHandler = ctrlLog.NewKubeAPIWarningLogger(
		Log.WithName("KubeAPIWarningLogger"),
		ctrlLog.KubeAPIWarningLoggerOptions{
			Deduplicate: true,
		},
	)
	return clientgo.NewForConfig(clientConfig)
}

func (k *Kind) NewClientConfig() (*rest.Config, error) {
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: k.KubeConfigPath(),
		},
		&clientcmd.ConfigOverrides{
			// ClusterInfo: clientcmdapi.Cluster{
			// 	Server: "",
			// },
			CurrentContext: Name + "-" + k.ClusterName(),
		})

	return loader.ClientConfig()
}

func (k *Kind) Delete() error {
	if k.shouldBypass() {
		return nil
	}
	return k.Provider.Delete(k.ClusterName(), k.KubeConfigPath())
}

func (k *Kind) shouldBypass() bool {
	if k.prexistingKubeconfig != nil {
		k.Logger.V(2).Info("bypassing kind provider as pre-existing cluster credentials were specified via '" + EnvForcePreexisting + "' and '" + EnvPreexitstingKubeconfig + "'")
		return true
	}
	return false
}
