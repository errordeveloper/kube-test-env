package kind_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"

	"github.com/errordeveloper/kube-test-env/provider/kind"
)

func TestKindCreateAccessDelete(t *testing.T) {
	g := NewWithT(t)

	log := klog.FromContext(context.Background())
	k := kind.New(t.TempDir(), log)

	g.Expect(k.Create(nil, time.Minute*10)).To(Succeed())

	clusters, err := k.Provider.List()
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

	mgr, err := k.NewControllerManager()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(mgr).NotTo(BeNil())

	nodes := &corev1.NodeList{}
	ctx := context.Background()
	go func() {
		err := mgr.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()

	g.Expect(mgr.GetClient().List(ctx, nodes)).To(Succeed())
	g.Expect(nodes.Items).To(HaveLen(1))

	g.Expect(k.Delete()).To(Succeed())

	clusters, err = k.Provider.List()
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(clusters).ToNot(ContainElement(k.ClusterName()))

	t.Logf("Deleted cluster name=%q", k.ClusterName())
}
