//go:build e2e
// +build e2e

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/chinflux/chinflux-operator/test/utils"
)

const (
	operatorNamespace = "chinflux-operator-system"
	testNamespace     = "default"
)

var _ = Describe("ChinfluxOperator", Ordered, func() {
	var controllerPodName string

	BeforeAll(func() {
		By("creating operator namespace")
		cmd := exec.Command("kubectl", "create", "ns", operatorNamespace)
		_, _ = utils.Run(cmd) // ignore if already exists

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy",
			fmt.Sprintf("IMG=%s", managerImage),
			"ENABLE_WEBHOOKS=false")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	AfterAll(func() {
		By("cleaning up test resources")
		cmd := exec.Command("kubectl", "delete", "chinfluxclusters", "--all", "-n", testNamespace)
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "chinfluxbackups", "--all", "-n", testNamespace)
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "chinfluxrestores", "--all", "-n", testNamespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)
	})

	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", operatorNamespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			}

			By("Fetching events")
			cmd = exec.Command("kubectl", "get", "events", "-n", testNamespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Events:\n%s", eventsOutput)
			}
		}
	})

	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	Context("Operator", func() {
		It("should have the controller-manager pod running", func() {
			verifyControllerUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", operatorNamespace,
				)
				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1))
				controllerPodName = podNames[0]

				cmd = exec.Command("kubectl", "get", "pods", controllerPodName,
					"-o", "jsonpath={.status.phase}", "-n", operatorNamespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"))
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})
	})

	Context("Single-node ChinfluxCluster", func() {
		const clusterName = "e2e-single"

		It("should create a single-node cluster and reconcile all resources", func() {
			By("applying a single-node ChinfluxCluster CR")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = singleNodeCR(clusterName)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the ConfigMap was created")
			verifyResource := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "configmap",
					clusterName+"-config", "-n", testNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}
			Eventually(verifyResource).Should(Succeed())

			By("verifying the headless Service was created")
			verifyHeadless := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "service",
					clusterName+"-headless", "-n", testNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}
			Eventually(verifyHeadless).Should(Succeed())

			By("verifying the client Service was created")
			verifyClient := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "service",
					clusterName, "-n", testNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}
			Eventually(verifyClient).Should(Succeed())

			By("verifying the StatefulSet was created with 1 replica")
			verifySTS := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "statefulset",
					clusterName, "-n", testNamespace,
					"-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}
			Eventually(verifySTS).Should(Succeed())

			By("verifying the ChinfluxCluster status phase")
			verifyPhase := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "chinfluxcluster",
					clusterName, "-n", testNamespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Or(
					Equal("Running"),
					Equal("Initializing"),
					Equal("Pending"),
				))
			}
			Eventually(verifyPhase).Should(Succeed())
		})

		It("should clean up the single-node cluster", func() {
			cmd := exec.Command("kubectl", "delete", "chinfluxcluster",
				clusterName, "-n", testNamespace, "--wait=true", "--timeout=60s")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			verifyDeleted := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "statefulset",
					clusterName, "-n", testNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred())
			}
			Eventually(verifyDeleted).Should(Succeed())
		})
	})

	Context("Multi-node ChinfluxCluster", func() {
		const clusterName = "e2e-cluster"

		It("should create a 3-node cluster", func() {
			By("applying a 3-node ChinfluxCluster CR")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = clusterCR(clusterName, 3)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the StatefulSet has 3 replicas")
			verifySTS := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "statefulset",
					clusterName, "-n", testNamespace,
					"-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("3"))
			}
			Eventually(verifySTS).Should(Succeed())

			By("verifying the cluster status shows correct replica count")
			verifyReplicas := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "chinfluxcluster",
					clusterName, "-n", testNamespace,
					"-o", "jsonpath={.status.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("3"))
			}
			Eventually(verifyReplicas, 5*time.Minute).Should(Succeed())
		})

		It("should scale from 3 to 5 nodes", func() {
			By("patching the cluster to 5 replicas")
			cmd := exec.Command("kubectl", "patch", "chinfluxcluster", clusterName,
				"-n", testNamespace, "--type=merge",
				"-p", `{"spec":{"replicas":5}}`)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the StatefulSet has 5 replicas")
			verifySTS := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "statefulset",
					clusterName, "-n", testNamespace,
					"-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("5"))
			}
			Eventually(verifySTS, 3*time.Minute).Should(Succeed())
		})

		It("should clean up the multi-node cluster", func() {
			cmd := exec.Command("kubectl", "delete", "chinfluxcluster",
				clusterName, "-n", testNamespace, "--wait=true", "--timeout=120s")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("ChinfluxBackup", func() {
		const (
			clusterName = "e2e-backup-cluster"
			backupName  = "e2e-test-backup"
		)

		BeforeEach(func() {
			By("creating a cluster for backup testing")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = singleNodeCR(clusterName)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			verifySTS := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "statefulset",
					clusterName, "-n", testNamespace,
					"-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}
			Eventually(verifySTS).Should(Succeed())
		})

		AfterEach(func() {
			cmd := exec.Command("kubectl", "delete", "chinfluxbackup",
				backupName, "-n", testNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "chinfluxcluster",
				clusterName, "-n", testNamespace, "--ignore-not-found", "--wait=false")
			_, _ = utils.Run(cmd)
		})

		It("should create a ChinfluxBackup and track status", func() {
			By("applying a ChinfluxBackup CR")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = backupCR(backupName, clusterName)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the backup status is set")
			verifyBackup := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "chinfluxbackup",
					backupName, "-n", testNamespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Or(
					Equal("Pending"),
					Equal("Running"),
					Equal("Completed"),
					Equal("Failed"),
				))
			}
			Eventually(verifyBackup, 2*time.Minute).Should(Succeed())
		})
	})

	Context("ChinfluxRestore", func() {
		const (
			clusterName = "e2e-restore-cluster"
			restoreName = "e2e-test-restore"
			backupName  = "e2e-restore-backup"
		)

		BeforeEach(func() {
			By("creating a cluster for restore testing")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = singleNodeCR(clusterName)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			verifySTS := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "statefulset",
					clusterName, "-n", testNamespace,
					"-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"))
			}
			Eventually(verifySTS).Should(Succeed())
		})

		AfterEach(func() {
			cmd := exec.Command("kubectl", "delete", "chinfluxrestore",
				restoreName, "-n", testNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "chinfluxcluster",
				clusterName, "-n", testNamespace, "--ignore-not-found", "--wait=false")
			_, _ = utils.Run(cmd)
		})

		It("should create a ChinfluxRestore and track status", func() {
			By("applying a ChinfluxRestore CR")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = restoreCR(restoreName, clusterName, backupName)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the restore status is set")
			verifyRestore := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "chinfluxrestore",
					restoreName, "-n", testNamespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Or(
					Equal("Pending"),
					Equal("ScalingDown"),
					Equal("Restoring"),
					Equal("ScalingUp"),
					Equal("Completed"),
					Equal("Failed"),
				))
			}
			Eventually(verifyRestore, 2*time.Minute).Should(Succeed())
		})
	})
})
