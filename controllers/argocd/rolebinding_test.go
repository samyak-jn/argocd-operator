package argocd

import (
	"context"
	"fmt"
	"os"
	"testing"

	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestReconcileArgoCD_reconcileRoleBinding(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	r := makeTestReconciler(t, a)
	p := policyRuleForApplicationController()

	assert.NilError(t, createNamespace(r, a.Namespace, ""))
	assert.NilError(t, createNamespace(r, "newTestNamespace", a.Namespace))

	workloadIdentifier := "xrb"

	assert.NilError(t, r.reconcileRoleBinding(workloadIdentifier, p, a))

	roleBinding := &rbacv1.RoleBinding{}
	expectedName := fmt.Sprintf("%s-%s", a.Name, workloadIdentifier)
	assert.NilError(t, r.Client.Get(context.TODO(), types.NamespacedName{Name: expectedName, Namespace: a.Namespace}, roleBinding))
	assert.NilError(t, r.Client.Get(context.TODO(), types.NamespacedName{Name: expectedName, Namespace: "newTestNamespace"}, roleBinding))

	// update role reference and subject of the rolebinding
	roleBinding.RoleRef.Name = "not-xrb"
	roleBinding.Subjects[0].Name = "not-xrb"
	assert.NilError(t, r.Client.Update(context.TODO(), roleBinding))

	// try reconciling it again and verify if the changes are overwritten
	assert.NilError(t, r.reconcileRoleBinding(workloadIdentifier, p, a))

	roleBinding = &rbacv1.RoleBinding{}
	assert.NilError(t, r.Client.Get(context.TODO(), types.NamespacedName{Name: expectedName, Namespace: a.Namespace}, roleBinding))
}

func TestReconcileArgoCD_reconcileRoleBinding_dex_disabled(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	r := makeTestReconciler(t, a)
	assert.NilError(t, createNamespace(r, a.Namespace, ""))

	rules := policyRuleForDexServer()
	rb := newRoleBindingWithname(dexServer, a)

	// Dex is enabled, creates a role binding
	assert.NilError(t, r.reconcileRoleBinding(dexServer, rules, a))
	assert.NilError(t, r.Client.Get(context.TODO(), types.NamespacedName{Name: rb.Name, Namespace: a.Namespace}, rb))

	// Disable Dex, deletes the existing role binding
	os.Setenv("DISABLE_DEX", "true")
	defer os.Unsetenv("DISABLE_DEX")

	_, err := r.reconcileRole(dexServer, rules, a)
	assert.NilError(t, err)
	assert.NilError(t, r.reconcileRoleBinding(dexServer, rules, a))
	assert.ErrorContains(t, r.Client.Get(context.TODO(), types.NamespacedName{Name: rb.Name, Namespace: a.Namespace}, rb), "not found")
}

func TestReconcileArgoCD_reconcileClusterRoleBinding(t *testing.T) {
	logf.SetLogger(ZapLogger(true))
	a := makeTestArgoCD()
	r := makeTestReconciler(t, a)

	workloadIdentifier := "x"
	expectedClusterRole := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: workloadIdentifier}}
	expectedServiceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: workloadIdentifier, Namespace: a.Namespace}}

	assert.NilError(t, r.reconcileClusterRoleBinding(workloadIdentifier, expectedClusterRole, expectedServiceAccount, a))

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	expectedName := fmt.Sprintf("%s-%s-%s", a.Name, a.Namespace, workloadIdentifier)
	assert.NilError(t, r.Client.Get(context.TODO(), types.NamespacedName{Name: expectedName}, clusterRoleBinding))

	// update role reference and subject of the clusterrolebinding
	clusterRoleBinding.RoleRef.Name = "not-x"
	clusterRoleBinding.Subjects[0].Name = "not-x"
	assert.NilError(t, r.Client.Update(context.TODO(), clusterRoleBinding))

	// try reconciling it again and verify if the changes are overwritten
	assert.NilError(t, r.reconcileClusterRoleBinding(workloadIdentifier, expectedClusterRole, expectedServiceAccount, a))

	clusterRoleBinding = &rbacv1.ClusterRoleBinding{}
	assert.NilError(t, r.Client.Get(context.TODO(), types.NamespacedName{Name: expectedName}, clusterRoleBinding))
}
