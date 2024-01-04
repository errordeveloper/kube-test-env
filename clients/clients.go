package clients

import (
	"context"
	"fmt"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgo "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

type ClientMakerBase struct {
	*rest.Config
	logger klog.Logger
}

type ClientMaker struct {
	*ClientMakerBase

	ResourceMetadataTemplate v1.ObjectMeta

	namespacedClientResources [][]ctrlClient.Object
}

type NamespacedClientMaker struct {
	*ClientMakerBase

	Namespace                           string
	DefaultControllerRuntimeListOptions *ctrlClient.ListOptions
	ResourceMetadataTemplate            v1.ObjectMeta
}

func NewClientMaker(config *rest.Config, logger klog.Logger) *ClientMaker {
	return &ClientMaker{
		ClientMakerBase: &ClientMakerBase{
			Config: rest.CopyConfig(config),
			logger: logger,
		},
		ResourceMetadataTemplate: v1.ObjectMeta{
			GenerateName: "kte-",
		},
	}
}

func (m *ClientMakerBase) NewControllerRuntimeClient() (ctrlClient.Client, error) {
	clientConfig := rest.CopyConfig(m.Config)

	options := ctrlClient.Options{
		Scheme: runtime.NewScheme(),
	}

	if err := clientgoscheme.AddToScheme(options.Scheme); err != nil {
		return nil, err
	}

	return ctrlClient.New(clientConfig, options)
}

func (m *ClientMakerBase) NewClientSet() (clientgo.Interface, error) {
	clientConfig := rest.CopyConfig(m.Config)

	clientConfig.WarningHandler = ctrlLog.NewKubeAPIWarningLogger(
		m.logger.WithName("KubeAPIWarningLogger"),
		ctrlLog.KubeAPIWarningLoggerOptions{
			Deduplicate: true,
		},
	)
	return clientgo.NewForConfig(clientConfig)
}

func (m *ClientMaker) NewNamespacedClientMaker(ctx context.Context, meta *v1.ObjectMeta) (*NamespacedClientMaker, error) {
	clientSet, err := m.NewClientSet()
	if err != nil {
		return nil, err
	}

	createOptions := v1.CreateOptions{}

	if meta == nil {
		meta = m.ResourceMetadataTemplate.DeepCopy()
	}
	namespace := &corev1.Namespace{
		ObjectMeta: *meta,
		// TODO: set finaliser, so that namespace resource can be captured for debugging
	}

	namespace, err = clientSet.CoreV1().Namespaces().Create(ctx, namespace, createOptions)
	if err != nil {
		return nil, err
	}

	meta.Namespace = namespace.Name
	meta.GenerateName = namespace.Name + "-"

	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: *meta,
	}

	serviceAccount, err = clientSet.CoreV1().ServiceAccounts(namespace.Name).Create(ctx, serviceAccount, createOptions)
	if err != nil {
		return nil, err
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: *meta,
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccount.Name,
				Namespace: namespace.Name,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind: "ClusterRole",
			Name: "admin",
		},
	}

	roleBinding, err = clientSet.RbacV1().RoleBindings(meta.Namespace).Create(ctx, roleBinding, createOptions)
	if err != nil {
		return nil, err
	}

	// revers of the creation order
	m.namespacedClientResources = append(m.namespacedClientResources, []ctrlClient.Object{
		roleBinding,
		serviceAccount,
		namespace,
	})

	clientConfig := rest.CopyConfig(m.Config)
	clientConfig.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s",
		meta.Namespace, serviceAccount.Name)

	clientMaker := &NamespacedClientMaker{
		ClientMakerBase: &ClientMakerBase{
			Config: clientConfig,
			logger: m.logger,
		},
		Namespace: meta.Namespace,
		DefaultControllerRuntimeListOptions: &ctrlClient.ListOptions{
			Namespace: meta.Namespace,
		},
		ResourceMetadataTemplate: v1.ObjectMeta{
			GenerateName: meta.GenerateName,
			Namespace:    meta.Namespace,
		},
	}
	return clientMaker, nil
}

func (m *ClientMaker) Cleanup() error {
	client, err := m.NewControllerRuntimeClient()
	if err != nil {
		return err
	}

	for i := len(m.namespacedClientResources) - 1; i >= 0; i-- {
		for _, resource := range m.namespacedClientResources[i] {
			if err := client.Delete(context.Background(), resource, &ctrlClient.DeleteAllOfOptions{}); err != nil {
				return err
			}
		}
	}

	return nil
}
