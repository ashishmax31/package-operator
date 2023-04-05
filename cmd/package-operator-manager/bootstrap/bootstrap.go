package bootstrap

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "package-operator.run/apis/core/v1alpha1"
	"package-operator.run/package-operator/cmd/package-operator-manager/components"
	"package-operator.run/package-operator/internal/controllers"
	"package-operator.run/package-operator/internal/packages/packagecontent"
	"package-operator.run/package-operator/internal/packages/packageimport"
	"package-operator.run/package-operator/internal/packages/packageloader"
)

const packageOperatorDeploymentName = "package-operator-manager"

type Bootstrapper struct {
	client client.Client
	log    logr.Logger
	init   func(ctx context.Context) (
		*corev1alpha1.ClusterPackage, error,
	)

	pkoNamespace string
}

func NewBootstrapper(
	scheme *runtime.Scheme, log logr.Logger,
	uncachedClient components.UncachedClient,
	opts components.Options,
) (*Bootstrapper, error) {
	c := uncachedClient
	init := newInitializer(
		c, packageloader.New(scheme, packageloader.WithDefaults),
		packageimport.Folder, opts.SelfBootstrap, opts.SelfBootstrapConfig,
	)

	return &Bootstrapper{
		log:    log.WithName("bootstrapper"),
		client: c,
		init:   init.Init,

		pkoNamespace: opts.Namespace,
	}, nil
}

func (b *Bootstrapper) Bootstrap(ctx context.Context, runManager func(ctx context.Context) error) error {
	ctx = logr.NewContext(ctx, b.log)

	log := b.log
	log.Info("running self-bootstrap")
	defer log.Info("self-bootstrap done")

	if _, err := b.init(ctx); err != nil {
		return err
	}

	needsBootstrap, err := b.needsBootstrap(ctx)
	if err != nil {
		return fmt.Errorf("checking if self-bootstrap is needed: %w", err)
	}
	if !needsBootstrap {
		return nil
	}
	return b.bootstrap(ctx, runManager)
}

func (b *Bootstrapper) bootstrap(ctx context.Context, runManager func(ctx context.Context) error) error {
	// Stop when Package Operator is installed.
	ctx, cancel := context.WithCancel(ctx)
	go b.cancelWhenPackageAvailable(ctx, cancel)

	// Force Adoption of objects during initial bootstrap to take ownership of
	// CRDs, Namespace, ServiceAccount and ClusterRoleBinding.
	if err := os.Setenv(controllers.ForceAdoptionEnvironmentVariable, "1"); err != nil {
		return err
	}
	if err := runManager(ctx); err != nil {
		return fmt.Errorf("running manager for self-bootstrap: %w", err)
	}
	return nil
}

func (b *Bootstrapper) needsBootstrap(ctx context.Context) (bool, error) {
	deploy := &appsv1.Deployment{}
	err := b.client.Get(ctx, client.ObjectKey{
		Name:      packageOperatorDeploymentName,
		Namespace: b.pkoNamespace,
	}, deploy)
	if errors.IsNotFound(err) {
		// Deployment does not exist.
		return true, nil
	}
	if err != nil {
		return false, err
	}

	for _, cond := range deploy.Status.Conditions {
		if cond.Type == appsv1.DeploymentAvailable &&
			cond.Status == corev1.ConditionTrue {
			// Deployment is available -> nothing to do.
			return false, nil
		}
	}
	return true, nil
}

func (b *Bootstrapper) cancelWhenPackageAvailable(
	ctx context.Context, cancel context.CancelFunc,
) {
	log := logr.FromContextOrDiscard(ctx)
	err := wait.PollImmediateUntilWithContext(
		ctx, packageOperatorPackageCheckInterval,
		func(ctx context.Context) (done bool, err error) {
			return b.isPackageAvailable(ctx)
		})
	if err != nil {
		panic(err)
	}

	log.Info("Package Operator bootstrapped successfully!")
	cancel()
}

func (b *Bootstrapper) isPackageAvailable(ctx context.Context) (
	available bool, err error,
) {
	packageOperatorPackage := &corev1alpha1.ClusterPackage{}
	err = b.client.Get(ctx, client.ObjectKey{
		Name: packageOperatorClusterPackageName,
	}, packageOperatorPackage)
	if err != nil {
		return false, err
	}

	if meta.IsStatusConditionTrue(
		packageOperatorPackage.Status.Conditions,
		corev1alpha1.PackageAvailable,
	) {
		return true, nil
	}
	return false, nil
}

type packageLoader interface {
	FromFiles(
		ctx context.Context, files packagecontent.Files,
		opts ...packageloader.Option,
	) (*packagecontent.Package, error)
}

type bootstrapperLoadFilesFn func(
	ctx context.Context, path string) (packagecontent.Files, error)
