package clients

import (
	"context"
	"fmt"
	"io"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgo "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/fluxcd/cli-utils/pkg/kstatus/polling"
	"github.com/fluxcd/pkg/ssa"

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

	cleanupCallbacks []func(context.Context)
}

type NamespacedClientMaker struct {
	*ClientMakerBase

	Namespace                           string
	DefaultControllerRuntimeListOptions *ctrlClient.ListOptions
	ResourceMetadataTemplate            v1.ObjectMeta

	Cleanup func(context.Context)
}

type ResourceManager struct {
	*ssa.ResourceManager
	logger klog.Logger
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

func (m *ClientMakerBase) NewResourceManager() (*ResourceManager, error) {
	client, err := m.NewControllerRuntimeClient()
	if err != nil {
		return nil, err
	}

	resourceManager := &ResourceManager{
		ResourceManager: ssa.NewResourceManager(client,
			polling.NewStatusPoller(client, client.RESTMapper(), polling.Options{}),
			ssa.Owner{
				Field: "kte",
				Group: "addons.kte.dev",
			},
		),
		logger: m.logger,
	}
	return resourceManager, nil
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

	clientMaker.Cleanup = func(ctx context.Context) {
		options := v1.DeleteOptions{}
		if err := clientSet.RbacV1().RoleBindings(meta.Namespace).Delete(ctx, roleBinding.Name, options); ctrlClient.IgnoreNotFound(err) != nil {
			m.logger.Error(err, "failed to delete role binding")
		}
		if err := clientSet.CoreV1().ServiceAccounts(meta.Namespace).Delete(ctx, serviceAccount.Name, options); ctrlClient.IgnoreNotFound(err) != nil {
			m.logger.Error(err, "failed to delete service account")
		}
		if err := clientSet.CoreV1().Namespaces().Delete(ctx, namespace.Name, options); ctrlClient.IgnoreNotFound(err) != nil {
			m.logger.Error(err, "failed to delete namespace")
		}
	}
	m.cleanupCallbacks = append(m.cleanupCallbacks, clientMaker.Cleanup)

	return clientMaker, nil
}

func (m *ClientMaker) Cleanup(ctx context.Context) {
	for i := range m.cleanupCallbacks {
		m.cleanupCallbacks[i](ctx)
	}
}

func (m *ResourceManager) ApplyManifest(ctx context.Context, r io.Reader) error {
	objs, err := ssa.ReadObjects(r)
	if err != nil {
		return err
	}

	if err := ssa.NormalizeUnstructuredListWithScheme(objs, m.Client().Scheme()); err != nil {
		return err
	}

	changeSet, err := m.ApplyAllStaged(ctx, objs, ssa.DefaultApplyOptions())
	if err != nil {
		return err
	}
	for _, change := range changeSet.Entries {
		m.logger.Info(change.String())
	}
	return m.WaitForSet(changeSet.ToObjMetadataSet(),
		ssa.WaitOptions{
			Interval: 2 * time.Second,
			Timeout:  time.Minute,
		})
}

func (m *ResourceManager) ApplyLists(ctx context.Context, objects ...runtime.Object) (*ssa.ChangeSet, error) {
	// TODO: flatten nested lists
	list, err := m.ToNormalizedList(objects...)
	if err != nil {
		return nil, err
	}

	return m.ApplyAllStaged(ctx, list, ssa.DefaultApplyOptions())
}

func (m *ResourceManager) ToNormalizedList(objects ...runtime.Object) ([]*unstructured.Unstructured, error) {
	list := []*unstructured.Unstructured{}
	for i := range objects {
		unstructuredObject, err := ToUnstructured(objects[i])
		if err != nil {
			return nil, err
		}

		if !unstructuredObject.IsList() {
			list = append(list, unstructuredObject)
			continue
		}

		unstructuredList, err := unstructuredObject.ToList()
		if err != nil {
			return nil, err
		}

		for i := range unstructuredList.Items {
			item := unstructuredList.Items[i].DeepCopy()
			if err := ssa.NormalizeUnstructuredWithScheme(item, m.Client().Scheme()); err != nil {
				return nil, err
			}
			list = append(list, item)
		}
	}

	return list, nil
}

func ToUnstructured(obj runtime.Object) (*unstructured.Unstructured, error) {
	// If the incoming object is already unstructured, perform a deep copy first
	// otherwise DefaultUnstructuredConverter ends up returning the inner map without
	// making a copy.
	if _, ok := obj.(runtime.Unstructured); ok {
		obj = obj.DeepCopyObject()
	}
	o, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)

	newUnstr := &unstructured.Unstructured{Object: o}
	return newUnstr, err
}
