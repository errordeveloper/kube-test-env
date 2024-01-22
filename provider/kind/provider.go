package kind

import (
	"context"
	"crypto"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fluxcd/pkg/ssa"
	"github.com/google/uuid"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	klog "k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/kind/pkg/cluster"

	configv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	addonsflux "github.com/errordeveloper/kube-test-env/addons/flux"
	"github.com/errordeveloper/kube-test-env/clients"
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

type KindProvider interface {
	ClusterName() string
	KubeConfigPath() string
	NewClientConfig() (*rest.Config, error)
	NewClientMaker() (*clients.ClientMaker, error)
}

type KindLifecycle interface {
	KindProvider

	Create(config *Cluster, timeout time.Duration) error
	CollectLogs() error
	LogsDir() string
	Delete() error
}

type Managed struct {
	Common[KindProvider]

	UUID uuid.UUID

	*cluster.Provider

	NodeImage string
	Retain    bool

	ArtifactDir string
	Logger      klog.Logger
}

type Unmanaged struct {
	Common[KindProvider]

	Logger klog.Logger

	importedKubeconfigPath string
}

type Common[T KindProvider] struct{ k T }

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
	k    KindLifecycle
}{
	once: &sync.Once{},
}

var (
	SharedConfig  *Cluster
	SharedTimeout = time.Minute * 10
)

var Log = klog.NewKlogr()

func init() {
	ctrl.SetLogger(Log.WithName("kte-controller-runtime"))
}

func Shared(logger klog.Logger) (KindProvider, error) {
	var initErr error
	shared.once.Do(func() {
		logger.Info("initializing shared provider")
		if preexsting := newUnamanagedFromEnv(logger, false); preexsting != nil {
			shared.k = preexsting
			return
		}
		artifactDir, err := os.MkdirTemp("", "kte-kind-shared-provider-")
		if err != nil {
			initErr = err
			return
		}
		shared.k = New(artifactDir, logger.WithName("kind-shared-provider"))

		logger.Info("creating cluster with shared provider")
		if err := shared.k.Create(SharedConfig, SharedTimeout); err != nil {
			initErr = err
			return
		}
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
		k := New(artifactDir, logger)
		if err := k.Create(SharedConfig, SharedTimeout); err != nil {
			return nil, err
		}
		return k, nil
	}

	logger.Info("using shared provider", "kind-cluster-name", shared.k.ClusterName())

	if shared.k == nil {
		return nil, fmt.Errorf("shared provider '%s' not initialized", Name)
	}
	return shared.k, nil
}

func SharedCollectLogs() error {
	if shared.k == nil {
		return nil
	}
	return shared.k.CollectLogs()
}

func SharedLogsDir() string {
	if shared.k == nil {
		return ""
	}
	return shared.k.LogsDir()
}

func SharedDelete() error {
	if shared.k == nil {
		return nil
	}
	if err := shared.k.Delete(); err != nil {
		return err
	}
	shared.k = nil
	return nil
}

func New(artifactDir string, logger klog.Logger) KindLifecycle {
	if preexisting := newUnamanagedFromEnv(logger, false); preexisting != nil {
		return preexisting
	}

	logAdapter := &log.Adapter{logger.WithName("kind")}
	uuid := uuid.New()
	k := &Managed{
		UUID:        uuid,
		ArtifactDir: artifactDir,
		Logger:      logger.WithName("kind-provider").WithValues("kind-provider-uuid", uuid.String()),
		Provider:    cluster.NewProvider(cluster.ProviderWithLogger(logAdapter)),
	}
	k.Common = Common[KindProvider]{k: k}
	return k
}

func NewUnmanaged(logger klog.Logger, importKubeconfigPath string) *Unmanaged {
	k := &Unmanaged{
		Logger:                 logger,
		importedKubeconfigPath: importKubeconfigPath,
	}
	k.Common = Common[KindProvider]{k: k}
	return k
}

func newUnamanagedFromEnv(logger klog.Logger, shared bool) *Unmanaged {
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
			return NewUnmanaged(logger.WithName("kind-prexisting-all"), preexistingKubeconfig)
		case EnvForcePreexistingShared:
			if shared {
				logger.Info("not using pre-exising shared cluster as '" + EnvForcePreexisting + "=" + EnvForcePreexistingShared + "' was set, it needs to be explicitly set to '" + EnvForcePreexistingAll + "'")
				return nil
			}
			return NewUnmanaged(logger.WithName("kind-prexisting-shared"), preexistingKubeconfig)
		default:
			logger.Info("not using pre-exising cluster as '" + EnvForcePreexisting + "=" + forcePrexisting + "' was set and it's unsupported")
			return nil
		}
	}
}

func (k *Managed) ClusterName() string {
	return ClusterNamePrefix + k.UUID.String()
}

func (k *Managed) KubeConfigPath() string {
	return filepath.Join(k.ArtifactDir, k.ClusterName(), "kubeconfig")
}

func (k *Managed) LogsDir() string {
	return filepath.Join(k.ArtifactDir, k.ClusterName(), "logs")
}

func (k *Managed) Create(config *Cluster, timeout time.Duration) error {
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
	k.Logger.Info("Create(): creating cluster", "kind-cluster-name", k.ClusterName())
	return k.Provider.Create(k.ClusterName(), options...)
}

func (k *Managed) CollectLogs() error {
	k.Logger.Info("CollectLogs(): collecting logs", "kind-cluster-name", k.ClusterName())
	return k.Provider.CollectLogs(k.ClusterName(), k.LogsDir())
}

func (k *Managed) Delete() error {
	k.Logger.Info("Delete(): deleting cluster", "kind-cluster-name", k.ClusterName())
	return k.Provider.Delete(k.ClusterName(), k.KubeConfigPath())
}

func (k *Unmanaged) ClusterName() string {
	hash := crypto.SHA256.New()
	_, _ = hash.Write([]byte(k.importedKubeconfigPath))
	return ClusterNamePrefix + hex.EncodeToString(hash.Sum(nil))
}

func (k *Unmanaged) KubeConfigPath() string { return k.importedKubeconfigPath }
func (k *Unmanaged) LogsDir() string        { return "" }

func (k *Unmanaged) noop(fn string) error {
	k.Logger.Info(fmt.Sprintf("%s(): no-op, cluster was imported", fn), "kubeconfig", k.importedKubeconfigPath)
	return nil
}

func (k *Unmanaged) Create(config *Cluster, timeout time.Duration) error { return k.noop("Create") }
func (k *Unmanaged) CollectLogs() error                                  { return k.noop("CollectLogs") }
func (k *Unmanaged) Delete() error                                       { return k.noop("Delete") }

func (k Common[T]) NewClientConfig() (*rest.Config, error) {
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: k.k.KubeConfigPath(),
		},
		&clientcmd.ConfigOverrides{
			// CurrentContext: Name + "-" + k.k.ClusterName(),
		})

	return loader.ClientConfig()
}

func (k Common[T]) NewClientMaker() (*clients.ClientMaker, error) {
	clientConfig, err := k.NewClientConfig()
	if err != nil {
		return nil, err
	}
	return clients.NewClientMaker(clientConfig, Log), nil
}

func (k Common[T]) ApplyManifest(ctx context.Context, r io.Reader, rm *ssa.ResourceManager) error {
	objs, err := ssa.ReadObjects(r)
	if err != nil {
		return err
	}

	if err := ssa.NormalizeUnstructuredList(objs); err != nil {
		return err
	}

	changeSet, err := rm.ApplyAllStaged(ctx, objs, ssa.DefaultApplyOptions())
	if err != nil {
		return err
	}
	// for _, change := range changeSet.Entries {
	//  TODO: log
	// }
	return rm.WaitForSet(changeSet.ToObjMetadataSet(),
		ssa.WaitOptions{
			Interval: 2 * time.Second,
			Timeout:  time.Minute,
		})

}

func (k Common[T]) ApplyFluxSourceController(ctx context.Context, rm *ssa.ResourceManager) error {
	return k.ApplyManifest(ctx, addonsflux.SourceControllerManifests(), rm)
}
func (k Common[T]) ApplyFluxHelmController(ctx context.Context, rm *ssa.ResourceManager) error {
	return k.ApplyManifest(ctx, addonsflux.HelmControllerManifests(), rm)
}
func (k Common[T]) ApplyFluxKustomizeController(ctx context.Context, rm *ssa.ResourceManager) error {
	return k.ApplyManifest(ctx, addonsflux.KustomizeControllerManifests(), rm)
}
