package kind_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	klog "k8s.io/klog/v2"

	configv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"github.com/errordeveloper/kube-test-env/provider/kind"
)

func TestKind(t *testing.T) {
	g := NewWithT(t)

	log := klog.FromContext(context.Background())
	k := kind.New(t.TempDir(), log)

	g.Expect(k.Create(&configv1alpha4.Cluster{}, time.Minute*10)).To(Succeed())

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

	g.Expect(k.Delete()).To(Succeed())

	t.Logf("Deleted cluster name=%q", k.ClusterName())

}
