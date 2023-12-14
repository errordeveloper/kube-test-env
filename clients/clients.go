package clients

import (
	"k8s.io/apimachinery/pkg/runtime"
	clientgo "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
)

type ClientMaker struct {
	*rest.Config
	logger klog.Logger
}

func NewClientMaker(config *rest.Config, logger klog.Logger) *ClientMaker {
	return &ClientMaker{
		Config: rest.CopyConfig(config),
		logger: logger,
	}
}

func (m *ClientMaker) NewControllerRuntimeClient() (ctrlClient.Client, error) {
	clientConfig := rest.CopyConfig(m.Config)

	options := ctrlClient.Options{
		Scheme: runtime.NewScheme(),
	}

	if err := clientgoscheme.AddToScheme(options.Scheme); err != nil {
		return nil, err
	}

	return ctrlClient.New(clientConfig, options)
}

func (m *ClientMaker) NewClientSet() (clientgo.Interface, error) {
	clientConfig := rest.CopyConfig(m.Config)

	clientConfig.WarningHandler = ctrlLog.NewKubeAPIWarningLogger(
		m.logger.WithName("KubeAPIWarningLogger"),
		ctrlLog.KubeAPIWarningLoggerOptions{
			Deduplicate: true,
		},
	)
	return clientgo.NewForConfig(clientConfig)
}
