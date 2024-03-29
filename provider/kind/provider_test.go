package kind_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klog "k8s.io/klog/v2"

	"github.com/errordeveloper/kube-test-env/addons"
	"github.com/errordeveloper/kube-test-env/provider/kind"
)

type createAccessDeleteTestCase struct {
	config   *kind.Cluster
	numNodes int
}

func TestKindCreateAccessDelete(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()

	log := klog.FromContext(ctx)

	for _, tc := range []createAccessDeleteTestCase{
		{
			numNodes: 1,
			config:   nil,
		},
		{
			numNodes: 1,
			config:   &kind.Cluster{},
		},
		{
			numNodes: 3,
			config: &kind.Cluster{
				Nodes: []kind.Node{
					{Role: kind.ControlPlaneRole},
					{Role: kind.WorkerRole},
					{Role: kind.WorkerRole},
				},
			},
		},
	} {
		k := kind.New(t.TempDir(), log)

		g.Expect(k.Create(tc.config, time.Minute*10)).To(Succeed())

		g.Expect(k).To(BeAssignableToTypeOf((*kind.Managed)(nil)))

		provider := k.(*kind.Managed).Provider

		clusters, err := provider.List()
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(clusters).To(ContainElement(k.ClusterName()))

		t.Logf("Created cluster name=%q kubeconfig=%q", k.ClusterName(), k.KubeConfigPath())

		g.Expect(k.KubeConfigPath()).To(BeAnExistingFile())

		g.Expect(k.CollectLogs()).To(Succeed())
		g.Expect(k.LogsDir()).To(BeADirectory())

		cpLogs := func(p string) string {
			return filepath.Join(k.LogsDir(), k.ClusterName()+"-control-plane", p)
		}

		g.Expect(filepath.Join(k.LogsDir(), "kind-version.txt")).To(BeAnExistingFile())

		for _, log := range []string{
			"alternatives.log",
			"inspect.json",
			"containerd.log",
			"journal.log",
			"serial.log",
			"kubernetes-version.txt",
			"kubelet.log",
			"images.log",
		} {
			g.Expect(cpLogs(log)).To(BeAnExistingFile())
		}
		g.Expect(cpLogs("pods")).To(BeADirectory())
		g.Expect(cpLogs("containers")).To(BeADirectory())

		clientConfig, err := k.NewClientConfig()
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(clientConfig).NotTo(BeNil())
		g.Expect(clientConfig.Host).To(HavePrefix("https://127.0.0.1:"))
		g.Expect(clientConfig.Username).To(BeEmpty())
		g.Expect(clientConfig.Password).To(BeEmpty())
		g.Expect(clientConfig.BearerToken).To(BeEmpty())
		g.Expect(clientConfig.BearerTokenFile).To(BeEmpty())
		g.Expect(clientConfig.TLSClientConfig).NotTo(BeNil())
		g.Expect(clientConfig.TLSClientConfig.Insecure).To(BeFalse())
		g.Expect(clientConfig.TLSClientConfig.ServerName).To(BeEmpty())
		g.Expect(clientConfig.TLSClientConfig.CertFile).To(BeEmpty())
		g.Expect(clientConfig.TLSClientConfig.KeyFile).To(BeEmpty())
		g.Expect(clientConfig.TLSClientConfig.CAFile).To(BeEmpty())
		g.Expect(clientConfig.TLSClientConfig.CertData).ToNot(BeEmpty())
		g.Expect(clientConfig.TLSClientConfig.KeyData).ToNot(BeEmpty())
		g.Expect(clientConfig.TLSClientConfig.CAData).ToNot(BeEmpty())

		clients, err := k.NewClientMaker()
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(clients).NotTo(BeNil())

		{
			clientSet, err := clients.NewClientSet()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(clientSet).NotTo(BeNil())

			nodes, err := clientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(nodes.Items).To(HaveLen(tc.numNodes))
		}

		{
			client, err := clients.NewControllerRuntimeClient()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(client).NotTo(BeNil())

			nodes := &corev1.NodeList{}

			g.Expect(client.List(ctx, nodes)).To(Succeed())
			g.Expect(nodes.Items).To(HaveLen(tc.numNodes))
		}

		{
			clients, err := clients.NewNamespacedClientMaker(ctx, nil)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(clients).NotTo(BeNil())

			client, err := clients.NewClientSet()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(client).NotTo(BeNil())

			serviceAccounts, err := client.CoreV1().ServiceAccounts(clients.Namespace).List(ctx, metav1.ListOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(serviceAccounts.Items).To(HaveLen(2))

			clients.Cleanup(ctx)
		}

		{
			clients, err := clients.NewNamespacedClientMaker(ctx, nil)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(clients).NotTo(BeNil())

			client, err := clients.NewControllerRuntimeClient()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(client).NotTo(BeNil())

			serviceAccounts := &corev1.ServiceAccountList{}
			g.Expect(client.List(ctx, serviceAccounts, clients.DefaultControllerRuntimeListOptions)).To(Succeed())
			g.Expect(serviceAccounts.Items).To(HaveLen(2))
		}

		{
			rm, err := clients.NewResourceManager()
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(rm).NotTo(BeNil())

			g.Expect(k.ApplyAddons(ctx, addons.Config{
				FluxComponents: addons.FluxComponentsConfig{
					SourceController:    true,
					HelmController:      true,
					KustomizeController: true,
				},
			})).To(Succeed())
		}

		clients.Cleanup(ctx)

		g.Expect(k.Delete()).To(Succeed())

		clusters, err = provider.List()
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(clusters).ToNot(ContainElement(k.ClusterName()))

		t.Logf("Deleted cluster name=%q", k.ClusterName())
	}
}

func TestKindSharedAndImorted(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()

	log := klog.FromContext(ctx)

	k, err := kind.Shared(log)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(k).To(BeAssignableToTypeOf((*kind.Managed)(nil)))

	provider := k.(*kind.Managed).Provider

	clients, err := k.NewClientMaker()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clients).NotTo(BeNil())
	client, err := clients.NewClientSet()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(client).NotTo(BeNil())

	nodes1, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(nodes1.Items).To(HaveLen(1))

	unmanaged := kind.NewUnmanaged(log, k.KubeConfigPath())

	clients, err = unmanaged.NewClientMaker()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clients).NotTo(BeNil())
	client, err = clients.NewClientSet()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(client).NotTo(BeNil())

	nodes2, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(nodes2.Items).To(HaveLen(1))

	g.Expect(nodes1).To(Equal(nodes2))

	g.Expect(kind.SharedDelete()).To(Succeed())

	clusters, err := provider.List()
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(clusters).ToNot(ContainElement(k.ClusterName()))

	t.Logf("Deleted cluster name=%q", k.ClusterName())
}
