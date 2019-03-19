package specs

import (
	"errors"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	onwv1 "github.com/openshift/api/network/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/openshift-azure/pkg/util/random"
	"github.com/openshift/openshift-azure/test/sanity"
)

var _ = Describe("Openshift on Azure customer-admin e2e tests [CustomerAdmin][Fake]", func() {
	It("should not read nodes", func() {
		_, err := sanity.Checker.Client.CustomerAdmin.CoreV1.Nodes().Get("master-000000", metav1.GetOptions{})
		Expect(kerrors.IsForbidden(err)).To(Equal(true))
	})

	It("should have full access on all non-infrastructure namespaces", func() {
		// Create project as normal user
		namespace, err := random.LowerCaseAlphanumericString(5)
		Expect(err).ToNot(HaveOccurred())
		namespace = "e2e-test-" + namespace
		err = sanity.Checker.Client.EndUser.CreateProject(namespace)
		Expect(err).ToNot(HaveOccurred())
		defer sanity.Checker.Client.EndUser.CleanupProject(namespace)

		err = wait.PollImmediate(2*time.Second, 5*time.Minute, func() (bool, error) {
			rb, err := sanity.Checker.Client.CustomerAdmin.RbacV1.RoleBindings(namespace).Get("osa-customer-admin", metav1.GetOptions{})
			if err != nil {
				// still waiting for namespace
				if kerrors.IsNotFound(err) {
					return false, nil
				}
				// still waiting for reconciler and permissions
				if kerrors.IsForbidden(err) {
					return false, nil
				}
				return false, err
			}
			for _, subject := range rb.Subjects {
				if subject.Kind == "Group" && subject.Name == "osa-customer-admins" {
					return true, nil
				}
			}
			return false, errors.New("customer-admins rolebinding does not bind to customer-admins group")
		})
		Expect(err).ToNot(HaveOccurred())
		// get namespace created by user
		_, err = sanity.Checker.Client.CustomerAdmin.ProjectV1.Projects().Get(namespace, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	It("should not list infra namespace secrets", func() {
		// list all secrets in a namespace. should not see any in openshift-azure-logging
		_, err := sanity.Checker.Client.CustomerAdmin.CoreV1.Secrets("openshift-azure-logging").List(metav1.ListOptions{})
		Expect(kerrors.IsForbidden(err)).To(Equal(true))
	})

	It("should not list default namespace secrets", func() {
		// list all secrets in a namespace. should not see any in default
		_, err := sanity.Checker.Client.CustomerAdmin.CoreV1.Secrets("default").List(metav1.ListOptions{})
		Expect(kerrors.IsForbidden(err)).To(Equal(true))
	})

	It("should not able to query groups", func() {
		_, err := sanity.Checker.Client.CustomerAdmin.UserV1.Groups().Get("osa-customer-admins", metav1.GetOptions{})
		Expect(kerrors.IsForbidden(err)).To(Equal(true))
	})

	It("should not be able to escalate privileges", func() {
		_, err := sanity.Checker.Client.CustomerAdmin.RbacV1.ClusterRoleBindings().Create(&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-cluster-admin",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind: "User",
					Name: "customer-cluster-admin",
				},
			},
			RoleRef: rbacv1.RoleRef{
				Name: "cluster-admin",
				Kind: "ClusterRole",
			},
		})
		Expect(kerrors.IsForbidden(err)).To(Equal(true))
	})

	It("should be able to manage Quotas, LimitRanges, and EgressNetworkPolicies", func() {
		// create a project as an end user
		// add quota as customer-admin
		// verify it was added
		namespace, err := random.LowerCaseAlphanumericString(5)
		Expect(err).ToNot(HaveOccurred())
		namespace = "e2e-test-" + namespace
		err = sanity.Checker.Client.EndUser.CreateProject(namespace)
		Expect(err).ToNot(HaveOccurred())
		defer sanity.Checker.Client.EndUser.CleanupProject(namespace)

		err = wait.PollImmediate(2*time.Second, 5*time.Minute, func() (bool, error) {
			rb, err := sanity.Checker.Client.CustomerAdmin.RbacV1.RoleBindings(namespace).Get("osa-customer-admin-project", metav1.GetOptions{})
			if err != nil {
				// still waiting for namespace
				if kerrors.IsNotFound(err) {
					return false, nil
				}
				// still waiting for reconciler and permissions
				if kerrors.IsForbidden(err) {
					return false, nil
				}
				return false, err
			}
			for _, subject := range rb.Subjects {
				if subject.Kind == "Group" && subject.Name == "osa-customer-admins" {
					return true, nil
				}
			}
			return false, errors.New("customer-admins rolebinding does not bind to customer-admins group")
		})
		Expect(err).ToNot(HaveOccurred())

		resQuota := v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testresourcequota",
			},
			Spec: v1.ResourceQuotaSpec{
				Hard: v1.ResourceList{
					"configmaps":             resource.MustParse("1"),
					"persistentvolumeclaims": resource.MustParse("1"),
				},
			},
		}
		// Create a resourcequota
		_, err = sanity.Checker.Client.CustomerAdmin.CoreV1.ResourceQuotas(namespace).Create(&resQuota)
		Expect(err).ToNot(HaveOccurred())

		// modify a resource quota
		cmUpdate := "2"
		resQuota.Spec.Hard["configmaps"] = resource.MustParse(cmUpdate)
		_, err = sanity.Checker.Client.CustomerAdmin.CoreV1.ResourceQuotas(namespace).Update(&resQuota)
		Expect(err).ToNot(HaveOccurred())

		returnResQuota, err := sanity.Checker.Client.CustomerAdmin.CoreV1.ResourceQuotas(namespace).Get(resQuota.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		// Verify update to resourcequota
		Expect(returnResQuota.Spec.Hard["configmaps"]).To(Equal(resource.MustParse(cmUpdate)))

		err = sanity.Checker.Client.CustomerAdmin.CoreV1.ResourceQuotas(namespace).Delete(resQuota.ObjectMeta.Name, &metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		// limitrange test
		limitRange := v1.LimitRange{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testlimiterange",
			},
			Spec: v1.LimitRangeSpec{
				Limits: []v1.LimitRangeItem{
					{
						Type: "Pod",
						Max: v1.ResourceList{
							"cpu":    resource.MustParse("2"),
							"memory": resource.MustParse("500Mi"),
						},
						Min: v1.ResourceList{
							"cpu":    resource.MustParse("200m"),
							"memory": resource.MustParse("256Mi"),
						},
					},
				},
			},
		}

		resLR, err := sanity.Checker.Client.CustomerAdmin.CoreV1.LimitRanges(namespace).Create(&limitRange)
		Expect(err).ToNot(HaveOccurred())
		cpuValue := "3"
		resLR.Spec.Limits[0].Max["cpu"] = resource.MustParse(cpuValue)

		_, err = sanity.Checker.Client.CustomerAdmin.CoreV1.LimitRanges(namespace).Update(resLR)
		Expect(err).ToNot(HaveOccurred())

		returnResLR, err := sanity.Checker.Client.CustomerAdmin.CoreV1.LimitRanges(namespace).Get(resLR.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		// verify updated limit
		Expect(returnResLR.Spec.Limits[0].Max["cpu"]).To(Equal(resource.MustParse(cpuValue)))

		err = sanity.Checker.Client.CustomerAdmin.CoreV1.LimitRanges(namespace).Delete(limitRange.Name, &metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		// egressnetworkpolicy test
		networkPolicy := onwv1.EgressNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testegressnetworkpolicy",
			},
			Spec: onwv1.EgressNetworkPolicySpec{
				Egress: []onwv1.EgressNetworkPolicyRule{
					{
						Type: onwv1.EgressNetworkPolicyRuleAllow,
						To: onwv1.EgressNetworkPolicyPeer{
							DNSName: "www.redhat.com",
						},
					},
				},
			},
		}
		nenp, err := sanity.Checker.Client.CustomerAdmin.NetworkV1.EgressNetworkPolicies(namespace).Create(&networkPolicy)
		Expect(err).ToNot(HaveOccurred())

		updateDNS := "www.openshift.com"
		nenp.Spec.Egress[0].To = onwv1.EgressNetworkPolicyPeer{DNSName: updateDNS}

		_, err = sanity.Checker.Client.CustomerAdmin.NetworkV1.EgressNetworkPolicies(namespace).Update(nenp)
		Expect(err).ToNot(HaveOccurred())

		returnResNWP, err := sanity.Checker.Client.CustomerAdmin.NetworkV1.EgressNetworkPolicies(namespace).Get(networkPolicy.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(returnResNWP.Spec.Egress[0].To).To(Equal(onwv1.EgressNetworkPolicyPeer{DNSName: updateDNS}))

		err = sanity.Checker.Client.CustomerAdmin.NetworkV1.EgressNetworkPolicies(namespace).Delete(networkPolicy.Name, &metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	It("should sync AAD admin group", func() {
		syncSecret, err := sanity.Checker.Client.Admin.CoreV1.Secrets("openshift-infra").Get("aad-group-sync-config", metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		id := "44e69b4e-2e70-42df-bb97-3a890730d7b0"
		testUser := "testuserdisabled"
		gid := syncSecret.Data["customerAdminGroupId"]
		if strings.EqualFold(string(gid), id) {
			// test the users
			oca, err := sanity.Checker.Client.Admin.UserV1.Groups().Get("osa-customer-admins", metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(oca.Users)).To(Equal(1))
			Expect(strings.HasPrefix(oca.Users[0], testUser)).To(BeTrue())
		}
	})
	// Placeholder to test that a ded admin cannot delete pods in the default or openshift- namespaces
})
