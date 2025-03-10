package argocd

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	resourcev1 "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/argoproj-labs/argocd-operator/common"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	argoprojv1alpha1 "github.com/argoproj-labs/argocd-operator/api/v1alpha1"
)

const (
	testHTTPProxy  = "example.com:8888"
	testHTTPSProxy = "example.com:8443"
	testNoProxy    = ".example.com"
)

var (
	deploymentNames = []string{
		"argocd-repo-server",
		"argocd-dex-server",
		"argocd-grafana",
		"argocd-redis",
		"argocd-server"}
)

func TestReconcileArgoCD_reconcileRepoDeployment_loglevel(t *testing.T) {
	logf.SetLogger(ZapLogger(true))

	repoDeps := []*argoprojv1alpha1.ArgoCD{
		makeTestArgoCD(func(a *argoprojv1alpha1.ArgoCD) {
			a.Spec.Repo.LogLevel = "warn"
		}),
		makeTestArgoCD(func(a *argoprojv1alpha1.ArgoCD) {
			a.Spec.Repo.LogLevel = "error"
		}),
		makeTestArgoCD(),
	}

	for _, lglv := range repoDeps {

		var ll string
		if lglv.Spec.Repo.LogLevel == "" {
			ll = "info"
		} else {
			ll = lglv.Spec.Repo.LogLevel
		}

		r := makeTestReconciler(t, lglv)

		err := r.reconcileRepoDeployment(lglv)
		assert.NoError(t, err)
		deployment := &appsv1.Deployment{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{
			Name:      "argocd-repo-server",
			Namespace: testNamespace,
		}, deployment)
		assert.NoError(t, err)

		for _, con := range deployment.Spec.Template.Spec.Containers {
			if con.Name == "argocd-repo-server" {
				for cmdKey, cmd := range con.Command {
					if cmd == "--loglevel" {
						if diff := cmp.Diff(ll, con.Command[cmdKey+1]); diff != "" {
							t.Fatalf("reconcileRepoDeployment failed:\n%s", diff)
						}
					}
				}
			}
		}
	}
}

// TODO: This needs more testing for the rest of the RepoDeployment container
// fields.

// reconcileRepoDeployment creates a Deployment with the correct volumes for the
// repo-server.
func TestReconcileArgoCD_reconcileRepoDeployment_volumes(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	r := makeTestReconciler(t, a)

	err := r.reconcileRepoDeployment(a)
	assert.NoError(t, err)
	deployment := &appsv1.Deployment{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Name:      "argocd-repo-server",
		Namespace: testNamespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, repoServerDefaultVolumes(), deployment.Spec.Template.Spec.Volumes)
}

func TestReconcileArgoCD_reconcile_ServerDeployment_env(t *testing.T) {
	t.Run("Test some env set in argocd-server", func(t *testing.T) {
		logf.SetLogger(ZapLogger(true))
		a := makeTestArgoCD()
		a.Spec.Server.Env = []corev1.EnvVar{
			{
				Name:  "FOO",
				Value: "BAR",
			},
			{
				Name:  "BAR",
				Value: "FOO",
			},
		}
		timeout := 600
		a.Spec.Repo.ExecTimeout = &timeout
		r := makeTestReconciler(t, a)

		err := r.reconcileServerDeployment(a)
		assert.NoError(t, err)
		deployment := &appsv1.Deployment{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{
			Name:      "argocd-server",
			Namespace: testNamespace,
		}, deployment)
		assert.NoError(t, err)

		assert.Len(t, deployment.Spec.Template.Spec.Containers[0].Env, 2)
		assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "FOO", Value: "BAR"})
		assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "BAR", Value: "FOO"})
	})

}

func TestReconcileArgoCD_reconcileRepoDeployment_env(t *testing.T) {
	t.Run("Test some env set in argocd-repo-server", func(t *testing.T) {
		logf.SetLogger(ZapLogger(true))
		a := makeTestArgoCD()
		a.Spec.Repo.Env = []corev1.EnvVar{
			{
				Name:  "FOO",
				Value: "BAR",
			},
			{
				Name:  "BAR",
				Value: "FOO",
			},
		}
		timeout := 600
		a.Spec.Repo.ExecTimeout = &timeout
		r := makeTestReconciler(t, a)

		err := r.reconcileRepoDeployment(a)
		assert.NoError(t, err)
		deployment := &appsv1.Deployment{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{
			Name:      "argocd-repo-server",
			Namespace: testNamespace,
		}, deployment)
		assert.NoError(t, err)

		assert.Len(t, deployment.Spec.Template.Spec.Containers[0].Env, 3)
		assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "FOO", Value: "BAR"})
		assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "BAR", Value: "FOO"})
		assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "ARGOCD_EXEC_TIMEOUT", Value: "600"})
	})

	t.Run("ExecTimeout set", func(t *testing.T) {
		logf.SetLogger(ZapLogger(true))
		a := makeTestArgoCD()
		timeout := 600
		a.Spec.Repo.ExecTimeout = &timeout
		r := makeTestReconciler(t, a)

		err := r.reconcileRepoDeployment(a)
		assert.NoError(t, err)
		deployment := &appsv1.Deployment{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{
			Name:      "argocd-repo-server",
			Namespace: testNamespace,
		}, deployment)
		assert.NoError(t, err)

		assert.Len(t, deployment.Spec.Template.Spec.Containers[0].Env, 1)
		assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "ARGOCD_EXEC_TIMEOUT", Value: "600"})
	})

	t.Run("ExecTimeout set with env set explicitly", func(t *testing.T) {
		logf.SetLogger(ZapLogger(true))
		a := makeTestArgoCD()
		timeout := 600
		a.Spec.Repo.ExecTimeout = &timeout
		a.Spec.Repo.Env = []corev1.EnvVar{
			{
				Name:  "ARGOCD_EXEC_TIMEOUT",
				Value: "20",
			},
		}
		r := makeTestReconciler(t, a)

		err := r.reconcileRepoDeployment(a)
		assert.NoError(t, err)
		deployment := &appsv1.Deployment{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{
			Name:      "argocd-repo-server",
			Namespace: testNamespace,
		}, deployment)
		assert.NoError(t, err)

		assert.Len(t, deployment.Spec.Template.Spec.Containers[0].Env, 1)
		assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "ARGOCD_EXEC_TIMEOUT", Value: "600"})
	})
	t.Run("ExecTimeout not set", func(t *testing.T) {
		logf.SetLogger(ZapLogger(true))
		a := makeTestArgoCD()
		r := makeTestReconciler(t, a)

		err := r.reconcileRepoDeployment(a)
		assert.NoError(t, err)
		deployment := &appsv1.Deployment{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{
			Name:      "argocd-repo-server",
			Namespace: testNamespace,
		}, deployment)
		assert.NoError(t, err)
		assert.Empty(t, deployment.Spec.Template.Spec.Containers[0].Env)
	})
}

// reconcileRepoDeployment creates a Deployment with the correct mounts for the
// repo-server.
func TestReconcileArgoCD_reconcileRepoDeployment_mounts(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	r := makeTestReconciler(t, a)

	err := r.reconcileRepoDeployment(a)
	assert.NoError(t, err)

	deployment := &appsv1.Deployment{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Name:      "argocd-repo-server",
		Namespace: testNamespace,
	}, deployment)
	assert.NoError(t, err)
	assert.Equal(t, repoServerDefaultVolumeMounts(), deployment.Spec.Template.Spec.Containers[0].VolumeMounts)
}

func TestReconcileArgoCD_reconcileDexDeployment_with_dex_disabled(t *testing.T) {
	restoreEnv(t)
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	r := makeTestReconciler(t, a)

	os.Setenv("DISABLE_DEX", "true")
	assert.NoError(t, r.reconcileDexDeployment(a))

	deployment := &appsv1.Deployment{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "argocd-dex-server", Namespace: a.Namespace}, deployment)
	assert.True(t, apierrors.IsNotFound(err))
}

// When Dex is disabled, the Dex Deployment should be removed.
func TestReconcileArgoCD_reconcileDexDeployment_removes_dex_when_disabled(t *testing.T) {
	restoreEnv(t)
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	r := makeTestReconciler(t, a)
	os.Setenv("DISABLE_DEX", "true")

	assert.NoError(t, r.reconcileDexDeployment(a))

	a = makeTestArgoCD()
	assert.NoError(t, r.reconcileDexDeployment(a))

	deployment := &appsv1.Deployment{}
	assertNotFound(t, r.Client.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      "argocd-dex-server",
			Namespace: a.Namespace,
		},
		deployment))
}

func TestReconcileArgoCD_reconcileDeployments_Dex_with_resources(t *testing.T) {
	restoreEnv(t)

	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCDWithResources()
	r := makeTestReconciler(t, a)

	assert.NoError(t, r.reconcileDexDeployment(a))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Client.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      a.Name + "-dex-server",
			Namespace: a.Namespace,
		},
		deployment))

	testResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resourcev1.MustParse("128Mi"),
			corev1.ResourceCPU:    resourcev1.MustParse("250m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
			corev1.ResourceCPU:    resourcev1.MustParse("500m"),
		},
	}
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources, testResources)
	assert.Equal(t, deployment.Spec.Template.Spec.InitContainers[0].Resources, testResources)
}

// reconcileRepoDeployments creates a Deployment with the proxy settings from the
// environment propagated.
func TestReconcileArgoCD_reconcileDeployments_proxy(t *testing.T) {
	restoreEnv(t)
	os.Setenv("HTTP_PROXY", testHTTPProxy)
	os.Setenv("HTTPS_PROXY", testHTTPSProxy)
	os.Setenv("no_proxy", testNoProxy)

	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD(func(a *argoprojv1alpha1.ArgoCD) {
		a.Spec.Grafana.Enabled = true
	})
	r := makeTestReconciler(t, a)

	err := r.reconcileDeployments(a)
	assert.NoError(t, err)

	for _, v := range deploymentNames {
		assertDeploymentHasProxyVars(t, r.Client, v)
	}
}

// reconcileRepoDeployments creates a Deployment with the proxy settings from the
// environment propagated.
//
// If the deployments already exist, they should be updated to reflect the new
// environment variables.
func TestReconcileArgoCD_reconcileDeployments_proxy_update_existing(t *testing.T) {
	restoreEnv(t)
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD(func(a *argoprojv1alpha1.ArgoCD) {
		a.Spec.Grafana.Enabled = true
	})
	r := makeTestReconciler(t, a)
	err := r.reconcileDeployments(a)
	assert.NoError(t, err)
	for _, v := range deploymentNames {
		refuteDeploymentHasProxyVars(t, r.Client, v)
	}

	os.Setenv("HTTP_PROXY", testHTTPProxy)
	os.Setenv("HTTPS_PROXY", testHTTPSProxy)
	os.Setenv("no_proxy", testNoProxy)

	logf.SetLogger(ZapLogger(true))

	err = r.reconcileDeployments(a)
	assert.NoError(t, err)

	for _, v := range deploymentNames {
		assertDeploymentHasProxyVars(t, r.Client, v)
	}
}

// TODO: This should be subsumed into testing of the HA setup.
func TestReconcileArgoCD_reconcileDeployments_HA_proxy(t *testing.T) {
	restoreEnv(t)
	os.Setenv("HTTP_PROXY", testHTTPProxy)
	os.Setenv("HTTPS_PROXY", testHTTPSProxy)
	os.Setenv("no_proxy", testNoProxy)

	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD(func(a *argoprojv1alpha1.ArgoCD) {
		a.Spec.HA.Enabled = true
	})
	r := makeTestReconciler(t, a)

	err := r.reconcileDeployments(a)
	assert.NoError(t, err)

	assertDeploymentHasProxyVars(t, r.Client, "argocd-redis-ha-haproxy")
}

func TestReconcileArgoCD_reconcileDeployments_HA_proxy_with_resources(t *testing.T) {
	restoreEnv(t)
	os.Setenv("HTTP_PROXY", testHTTPProxy)
	os.Setenv("HTTPS_PROXY", testHTTPSProxy)
	os.Setenv("no_proxy", testNoProxy)

	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCDWithResources(func(a *argoprojv1alpha1.ArgoCD) {
		a.Spec.HA.Enabled = true
	})
	r := makeTestReconciler(t, a)

	assert.NoError(t, r.reconcileRedisHAProxyDeployment(a))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Client.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      a.Name + "-redis-ha-haproxy",
			Namespace: a.Namespace,
		},
		deployment))

	testResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resourcev1.MustParse("128Mi"),
			corev1.ResourceCPU:    resourcev1.MustParse("250m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
			corev1.ResourceCPU:    resourcev1.MustParse("500m"),
		},
	}
	assert.Equal(t, deployment.Spec.Template.Spec.Containers[0].Resources, testResources)
	assert.Equal(t, deployment.Spec.Template.Spec.InitContainers[0].Resources, testResources)
}

func TestReconcileArgoCD_reconcileRepoDeployment_updatesVolumeMounts(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-repo-server",
			Namespace: testNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Command: []string{"testing"},
							Image:   "test-image",
						},
					},
				},
			},
		},
	}
	r := makeTestReconciler(t, a, d)

	err := r.reconcileRepoDeployment(a)
	assert.NoError(t, err)

	deployment := &appsv1.Deployment{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Name:      "argocd-repo-server",
		Namespace: testNamespace,
	}, deployment)
	assert.NoError(t, err)

	assert.Len(t, deployment.Spec.Template.Spec.Volumes, 5)
	assert.Len(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts, 5)
}

func Test_proxyEnvVars(t *testing.T) {
	restoreEnv(t)
	os.Setenv("HTTP_PROXY", testHTTPProxy)
	os.Setenv("HTTPS_PROXY", testHTTPSProxy)
	os.Setenv("no_proxy", testNoProxy)
	envTests := []struct {
		vars []corev1.EnvVar
		want []corev1.EnvVar
	}{
		{
			vars: []corev1.EnvVar{},
			want: []corev1.EnvVar{
				{Name: "HTTP_PROXY", Value: "example.com:8888"},
				{Name: "HTTPS_PROXY", Value: "example.com:8443"},
				{Name: "no_proxy", Value: ".example.com"},
			},
		},
		{
			vars: []corev1.EnvVar{
				{Name: "TEST_VAR", Value: "testing"},
			},
			want: []corev1.EnvVar{
				{Name: "TEST_VAR", Value: "testing"},
				{Name: "HTTP_PROXY", Value: "example.com:8888"},
				{Name: "HTTPS_PROXY", Value: "example.com:8443"},
				{Name: "no_proxy", Value: ".example.com"},
			},
		},
	}

	for _, tt := range envTests {
		e := proxyEnvVars(tt.vars...)
		assert.Equal(t, tt.want, e)
	}
}

func TestReconcileArgoCD_reconcileDeployment_nodePlacement(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD((func(a *argoprojv1alpha1.ArgoCD) {
		a.Spec.NodePlacement = &argoprojv1alpha1.ArgoCDNodePlacementSpec{
			NodeSelector: deploymentDefaultNodeSelector(),
			Tolerations:  deploymentDefaultTolerations(),
		}
	}))
	r := makeTestReconciler(t, a)
	err := r.reconcileRepoDeployment(a) //can use other deployments as well
	assert.NoError(t, err)
	deployment := &appsv1.Deployment{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{
		Name:      "argocd-repo-server",
		Namespace: testNamespace,
	}, deployment)
	assert.NoError(t, err)

	if diff := cmp.Diff(deploymentDefaultNodeSelector(), deployment.Spec.Template.Spec.NodeSelector); diff != "" {
		t.Fatalf("reconcileDeployment failed:\n%s", diff)
	}
	if diff := cmp.Diff(deploymentDefaultTolerations(), deployment.Spec.Template.Spec.Tolerations); diff != "" {
		t.Fatalf("reconcileDeployment failed:\n%s", diff)
	}
}

func deploymentDefaultNodeSelector() map[string]string {
	nodeSelector := map[string]string{
		"test_key1": "test_value1",
		"test_key2": "test_value2",
	}
	return nodeSelector
}
func deploymentDefaultTolerations() []corev1.Toleration {
	toleration := []corev1.Toleration{
		{
			Key:    "test_key1",
			Value:  "test_value1",
			Effect: corev1.TaintEffectNoSchedule,
		},
		{
			Key:      "test_key2",
			Value:    "test_value2",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}
	return toleration
}

func TestReconcileArgoCD_reconcileDexDeployment(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	r := makeTestReconciler(t, a)

	assert.NoError(t, r.reconcileDexDeployment(a))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Client.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      "argocd-dex-server",
			Namespace: a.Namespace,
		},
		deployment))
	want := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: "static-files",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		},
		InitContainers: []corev1.Container{
			{
				Name:  "copyutil",
				Image: getArgoContainerImage(a),
				Command: []string{
					"cp",
					"-n",
					"/usr/local/bin/argocd",
					"/shared/argocd-dex",
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "static-files",
						MountPath: "/shared",
					},
				},
				ImagePullPolicy: corev1.PullAlways,
			},
		},
		Containers: []corev1.Container{
			{
				Name:  "dex",
				Image: getDexContainerImage(a),
				Command: []string{
					"/shared/argocd-dex",
					"rundex",
				},
				Ports: []corev1.ContainerPort{
					{
						Name:          "http",
						ContainerPort: 5556,
					},
					{
						Name:          "grpc",
						ContainerPort: 5557,
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "static-files", MountPath: "/shared"}},
				ImagePullPolicy: corev1.PullAlways,
			},
		},
		ServiceAccountName: "argocd-argocd-dex-server",
	}

	assert.Equal(t, want, deployment.Spec.Template.Spec)
}

func TestReconcileArgoCD_reconcileDexDeployment_withUpdate(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	r := makeTestReconciler(t, a)

	// Creates the deployment and then changes the CR and rereconciles.
	assert.NoError(t, r.reconcileDexDeployment(a))
	a.Spec.Image = "justatest"
	a.Spec.Version = "latest"
	a.Spec.Dex.Image = "testdex"
	a.Spec.Dex.Version = "v0.0.1"
	assert.NoError(t, r.reconcileDexDeployment(a))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Client.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      "argocd-dex-server",
			Namespace: a.Namespace,
		},
		deployment))
	want := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: "static-files",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		},
		InitContainers: []corev1.Container{
			{
				Name:  "copyutil",
				Image: "justatest:latest",
				Command: []string{
					"cp",
					"-n",
					"/usr/local/bin/argocd",
					"/shared/argocd-dex",
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "static-files",
						MountPath: "/shared",
					},
				},
				ImagePullPolicy: corev1.PullAlways,
			},
		},
		Containers: []corev1.Container{
			{
				Name:  "dex",
				Image: "testdex:v0.0.1",
				Command: []string{
					"/shared/argocd-dex",
					"rundex",
				},
				Ports: []corev1.ContainerPort{
					{
						Name:          "http",
						ContainerPort: 5556,
					},
					{
						Name:          "grpc",
						ContainerPort: 5557,
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "static-files", MountPath: "/shared"}},
				ImagePullPolicy: corev1.PullAlways,
			},
		},
		ServiceAccountName: "argocd-argocd-dex-server",
	}
	assert.Equal(t, want, deployment.Spec.Template.Spec)
}

func TestReconcileArgoCD_reconcileServerDeployment(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	r := makeTestReconciler(t, a)
	assert.NoError(t, r.reconcileServerDeployment(a))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Client.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      "argocd-server",
			Namespace: a.Namespace,
		},
		deployment))
	want := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:            "argocd-server",
				Image:           getArgoContainerImage(a),
				ImagePullPolicy: corev1.PullAlways,
				Command: []string{
					"argocd-server",
					"--staticassets",
					"/shared/app",
					"--dex-server",
					"http://argocd-dex-server.argocd.svc.cluster.local:5556",
					"--repo-server",
					"argocd-repo-server.argocd.svc.cluster.local:8081",
					"--redis",
					"argocd-redis.argocd.svc.cluster.local:6379",
					"--loglevel",
					"info",
					"--logformat",
					"text",
				},
				Ports: []corev1.ContainerPort{
					{ContainerPort: 8080},
					{ContainerPort: 8083},
				},
				LivenessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/healthz",
							Port: intstr.FromInt(8080),
						},
					},
					InitialDelaySeconds: 3,
					PeriodSeconds:       30,
				},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/healthz",
							Port: intstr.FromInt(8080),
						},
					},
					InitialDelaySeconds: 3,
					PeriodSeconds:       30,
				},
				VolumeMounts: serverDefaultVolumeMounts(),
			},
		},
		Volumes:            serverDefaultVolumes(),
		ServiceAccountName: "argocd-argocd-server",
	}

	assert.Equal(t, want, deployment.Spec.Template.Spec)
}

func TestReconcileArgoCD_reconcileServerDeploymentWithInsecure(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD(func(a *argoprojv1alpha1.ArgoCD) {
		a.Spec.Server.Insecure = true
	})
	r := makeTestReconciler(t, a)

	assert.NoError(t, r.reconcileServerDeployment(a))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Client.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      "argocd-server",
			Namespace: a.Namespace,
		},
		deployment))
	want := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:            "argocd-server",
				Image:           getArgoContainerImage(a),
				ImagePullPolicy: corev1.PullAlways,
				Command: []string{
					"argocd-server",
					"--insecure",
					"--staticassets",
					"/shared/app",
					"--dex-server",
					"http://argocd-dex-server.argocd.svc.cluster.local:5556",
					"--repo-server",
					"argocd-repo-server.argocd.svc.cluster.local:8081",
					"--redis",
					"argocd-redis.argocd.svc.cluster.local:6379",
					"--loglevel",
					"info",
					"--logformat",
					"text",
				},
				Ports: []corev1.ContainerPort{
					{ContainerPort: 8080},
					{ContainerPort: 8083},
				},
				LivenessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/healthz",
							Port: intstr.FromInt(8080),
						},
					},
					InitialDelaySeconds: 3,
					PeriodSeconds:       30,
				},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/healthz",
							Port: intstr.FromInt(8080),
						},
					},
					InitialDelaySeconds: 3,
					PeriodSeconds:       30,
				},
				VolumeMounts: serverDefaultVolumeMounts(),
			},
		},
		Volumes:            serverDefaultVolumes(),
		ServiceAccountName: "argocd-argocd-server",
	}

	assert.Equal(t, want, deployment.Spec.Template.Spec)
}

func TestReconcileArgoCD_reconcileServerDeploymentChangedToInsecure(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	r := makeTestReconciler(t, a)

	assert.NoError(t, r.reconcileServerDeployment(a))

	a = makeTestArgoCD(func(a *argoprojv1alpha1.ArgoCD) {
		a.Spec.Server.Insecure = true
	})
	assert.NoError(t, r.reconcileServerDeployment(a))

	deployment := &appsv1.Deployment{}
	assert.NoError(t, r.Client.Get(
		context.TODO(),
		types.NamespacedName{
			Name:      "argocd-server",
			Namespace: a.Namespace,
		},
		deployment))
	want := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:            "argocd-server",
				Image:           getArgoContainerImage(a),
				ImagePullPolicy: corev1.PullAlways,
				Command: []string{
					"argocd-server",
					"--insecure",
					"--staticassets",
					"/shared/app",
					"--dex-server",
					"http://argocd-dex-server.argocd.svc.cluster.local:5556",
					"--repo-server",
					"argocd-repo-server.argocd.svc.cluster.local:8081",
					"--redis",
					"argocd-redis.argocd.svc.cluster.local:6379",
					"--loglevel",
					"info",
					"--logformat",
					"text",
				},
				Ports: []corev1.ContainerPort{
					{ContainerPort: 8080},
					{ContainerPort: 8083},
				},
				LivenessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/healthz",
							Port: intstr.FromInt(8080),
						},
					},
					InitialDelaySeconds: 3,
					PeriodSeconds:       30,
				},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/healthz",
							Port: intstr.FromInt(8080),
						},
					},
					InitialDelaySeconds: 3,
					PeriodSeconds:       30,
				},
				VolumeMounts: serverDefaultVolumeMounts(),
			},
		},
		Volumes:            serverDefaultVolumes(),
		ServiceAccountName: "argocd-argocd-server",
	}

	assert.Equal(t, want, deployment.Spec.Template.Spec)
}

func TestReconcileArgoCD_reconcileRedisDeployment(t *testing.T) {
	// tests reconciler hook for redis deployment
	cr := makeTestArgoCD()
	r := makeTestReconciler(t, cr)

	defer resetHooks()()
	Register(testDeploymentHook)

	assert.NoError(t, r.reconcileRedisDeployment(cr))
	d := &appsv1.Deployment{}
	assert.NoError(t, r.Client.Get(context.TODO(), types.NamespacedName{Name: cr.Name + "-redis", Namespace: cr.Namespace}, d))
	assert.Equal(t, int32(3), *d.Spec.Replicas)
}

func TestReconcileArgoCD_reconcileRedisDeployment_with_error(t *testing.T) {
	// tests reconciler hook for redis deployment
	cr := makeTestArgoCD()
	r := makeTestReconciler(t, cr)

	defer resetHooks()()
	Register(testErrorHook)

	assert.Error(t, r.reconcileRedisDeployment(cr), "this is a test error")
}

func restoreEnv(t *testing.T) {
	keys := []string{
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "no_proxy",
		"DISABLE_DEX"}
	env := map[string]string{}
	for _, v := range keys {
		env[v] = os.Getenv(v)
	}
	t.Cleanup(func() {
		for k, v := range env {
			os.Setenv(k, v)
		}
	})
}

func operationProcessors(n int32) argoCDOpt {
	return func(a *argoprojv1alpha1.ArgoCD) {
		a.Spec.Controller.Processors.Operation = n
	}
}

func appSync(d time.Duration) argoCDOpt {
	return func(a *argoprojv1alpha1.ArgoCD) {
		a.Spec.Controller.AppSync = &metav1.Duration{Duration: d}
	}
}
func Test_UpdateNodePlacement(t *testing.T) {

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-sample-server",
			Namespace: testNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{
						"test_key1": "test_value1",
						"test_key2": "test_value2",
					},
					Tolerations: []corev1.Toleration{
						{
							Key:    "test_key1",
							Value:  "test_value1",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
		},
	}
	deployment2 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-sample-server",
			Namespace: testNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{
						"test_key1": "test_value1",
					},
					Tolerations: []corev1.Toleration{
						{
							Key:    "test_key1",
							Value:  "test_value1",
							Effect: corev1.TaintEffectNoExecute,
						},
					},
				},
			},
		},
	}
	expectedChange := false
	actualChange := false
	updateNodePlacement(deployment, deployment, &actualChange)
	if actualChange != expectedChange {
		t.Fatalf("updateNodePlacement failed, value of changed: %t", actualChange)
	}
	updateNodePlacement(deployment, deployment2, &actualChange)
	if actualChange == expectedChange {
		t.Fatalf("updateNodePlacement failed, value of changed: %t", actualChange)
	}
}

func parallelismLimit(n int32) argoCDOpt {
	return func(a *argoprojv1alpha1.ArgoCD) {
		a.Spec.Controller.ParallelismLimit = n
	}
}

func assertDeploymentHasProxyVars(t *testing.T, c client.Client, name string) {
	t.Helper()
	deployment := &appsv1.Deployment{}
	err := c.Get(context.TODO(), types.NamespacedName{
		Name:      name,
		Namespace: testNamespace,
	}, deployment)
	assert.NoError(t, err)

	want := []corev1.EnvVar{
		{Name: "HTTP_PROXY", Value: testHTTPProxy},
		{Name: "HTTPS_PROXY", Value: testHTTPSProxy},
		{Name: "no_proxy", Value: testNoProxy},
	}
	for _, c := range deployment.Spec.Template.Spec.Containers {
		assert.Len(t, c.Env, len(want))
		for _, w := range want {
			assert.Contains(t, c.Env, w)
		}
	}
	for _, c := range deployment.Spec.Template.Spec.InitContainers {
		assert.Len(t, c.Env, len(want))
		for _, w := range want {
			assert.Contains(t, c.Env, w)
		}
	}
}

func refuteDeploymentHasProxyVars(t *testing.T, c client.Client, name string) {
	t.Helper()
	deployment := &appsv1.Deployment{}
	err := c.Get(context.TODO(), types.NamespacedName{
		Name:      name,
		Namespace: testNamespace,
	}, deployment)
	assert.NoError(t, err)

	names := []string{"http_proxy", "https_proxy", "no_proxy"}
	for _, name := range names {
		for _, c := range deployment.Spec.Template.Spec.Containers {
			for _, envVar := range c.Env {
				assert.NotEqual(t, strings.ToLower(envVar.Name), name)
			}
		}
		for _, c := range deployment.Spec.Template.Spec.InitContainers {
			for _, envVar := range c.Env {
				assert.NotEqual(t, strings.ToLower(envVar.Name), name)
			}
		}
	}
}

func assertNotFound(t *testing.T, err error) {
	t.Helper()
	assert.True(t, apierrors.IsNotFound(err))
}

func controllerProcessors(n int32) argoCDOpt {
	return func(a *argoprojv1alpha1.ArgoCD) {
		a.Spec.Controller.Processors.Status = n
	}
}

// repoServerVolumes returns the list of expected default volumes for the repo server
func repoServerDefaultVolumes() []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: "ssh-known-hosts",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: common.ArgoCDKnownHostsConfigMapName,
					},
				},
			},
		},
		{
			Name: "tls-certs",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: common.ArgoCDTLSCertsConfigMapName,
					},
				},
			},
		},
		{
			Name: "gpg-keys",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: common.ArgoCDGPGKeysConfigMapName,
					},
				},
			},
		},
		{
			Name: "gpg-keyring",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "argocd-repo-server-tls",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: common.ArgoCDRepoServerTLSSecretName,
					Optional:   boolPtr(true),
				},
			},
		},
	}
	return volumes
}

// repoServerDefaultVolumeMounts return the default volume mounts for the repo server
func repoServerDefaultVolumeMounts() []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{Name: "ssh-known-hosts", MountPath: "/app/config/ssh"},
		{Name: "tls-certs", MountPath: "/app/config/tls"},
		{Name: "gpg-keys", MountPath: "/app/config/gpg/source"},
		{Name: "gpg-keyring", MountPath: "/app/config/gpg/keys"},
		{Name: "argocd-repo-server-tls", MountPath: "/app/config/reposerver/tls"},
	}
	return mounts
}

func serverDefaultVolumes() []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: "ssh-known-hosts",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: common.ArgoCDKnownHostsConfigMapName,
					},
				},
			},
		}, {
			Name: "tls-certs",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: common.ArgoCDTLSCertsConfigMapName,
					},
				},
			},
		}, {
			Name: "argocd-repo-server-tls",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: common.ArgoCDRepoServerTLSSecretName,
					Optional:   boolPtr(true),
				},
			},
		},
	}
	return volumes
}

func serverDefaultVolumeMounts() []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{
			Name:      "ssh-known-hosts",
			MountPath: "/app/config/ssh",
		}, {
			Name:      "tls-certs",
			MountPath: "/app/config/tls",
		}, {
			Name:      "argocd-repo-server-tls",
			MountPath: "/app/config/server/tls",
		},
	}
	return mounts
}
