package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	corev1alpha1 "package-operator.run/apis/core/v1alpha1"
	"package-operator.run/package-operator/internal/testutil"
)

func TestBootstrapperBootstrap(t *testing.T) {
	c := testutil.NewClient()
	var initCalled bool
	b := &Bootstrapper{
		log:    testr.New(t),
		client: c,
		init: func(ctx context.Context) (
			*corev1alpha1.ClusterPackage, error,
		) {
			initCalled = true
			return &corev1alpha1.ClusterPackage{}, nil
		},
	}

	c.On("Get", mock.Anything, mock.Anything,
		mock.AnythingOfType("*v1.Deployment"),
		mock.Anything).
		Run(func(args mock.Arguments) {
			depl := args.Get(2).(*appsv1.Deployment)
			depl.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
		}).
		Return(nil)

	ctx := context.Background()
	err := b.Bootstrap(
		ctx, func(ctx context.Context) error { return nil })
	require.NoError(t, err)
	assert.True(t, initCalled)
}

func TestBootstrapper_bootstrap(t *testing.T) {
	c := testutil.NewClient()
	b := &Bootstrapper{client: c}

	var (
		runManagerCalled bool
		runManagerCtx    context.Context
	)

	c.On("Get", mock.Anything, mock.Anything,
		mock.AnythingOfType("*v1alpha1.ClusterPackage"),
		mock.Anything).
		Run(func(args mock.Arguments) {
			pkg := args.Get(2).(*corev1alpha1.ClusterPackage)
			meta.SetStatusCondition(
				&pkg.Status.Conditions, metav1.Condition{
					Type:   corev1alpha1.PackageAvailable,
					Status: metav1.ConditionTrue,
				})
		}).
		Return(nil)

	ctx, cancel := context.WithTimeout(
		context.Background(), 2*time.Second)
	defer cancel()
	err := b.bootstrap(ctx, func(ctx context.Context) error {
		runManagerCalled = true
		runManagerCtx = ctx
		<-ctx.Done()
		return nil
	})
	require.NoError(t, err)
	assert.True(t, runManagerCalled)
	assert.Equal(t, context.Canceled, runManagerCtx.Err())
}

func TestBootstrapper_needsBootstrap(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		c := testutil.NewClient()

		c.On("Get", mock.Anything, mock.Anything,
			mock.AnythingOfType("*v1.Deployment"),
			mock.Anything).
			Return(errors.NewNotFound(schema.GroupResource{}, ""))

		b := &Bootstrapper{client: c}
		needsBootstrap, err := b.needsBootstrap(
			context.Background())
		require.NoError(t, err)
		assert.True(t, needsBootstrap)
	})

	t.Run("not available", func(t *testing.T) {
		c := testutil.NewClient()

		c.On("Get", mock.Anything, mock.Anything,
			mock.AnythingOfType("*v1.Deployment"),
			mock.Anything).
			Return(nil)

		b := &Bootstrapper{client: c}
		needsBootstrap, err := b.needsBootstrap(
			context.Background())
		require.NoError(t, err)
		assert.True(t, needsBootstrap)
	})

	t.Run("available", func(t *testing.T) {
		c := testutil.NewClient()

		c.On("Get", mock.Anything, mock.Anything,
			mock.AnythingOfType("*v1.Deployment"),
			mock.Anything).
			Run(func(args mock.Arguments) {
				depl := args.Get(2).(*appsv1.Deployment)
				depl.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				}
			}).
			Return(nil)

		b := &Bootstrapper{client: c}
		needsBootstrap, err := b.needsBootstrap(
			context.Background())
		require.NoError(t, err)
		assert.False(t, needsBootstrap)
	})
}
