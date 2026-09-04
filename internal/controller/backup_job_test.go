package controller

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	hyperbytedbv1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func testBackupCluster() *hyperbytedbv1alpha1.HyperbytedbCluster {
	return &hyperbytedbv1alpha1.HyperbytedbCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "db-test", Namespace: "tenant-1"},
		Spec: hyperbytedbv1alpha1.HyperbytedbClusterSpec{
			Replicas: ptr.To(int32(1)),
			Image:    "ghcr.io/hyperbyte-cloud/hyperbytedb:0.8.5-beta",
		},
	}
}

func testBackup() *hyperbytedbv1alpha1.HyperbytedbBackup {
	return &hyperbytedbv1alpha1.HyperbytedbBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "db-test-backup-abcd", Namespace: "tenant-1"},
		Spec: hyperbytedbv1alpha1.HyperbytedbBackupSpec{
			ClusterName: "db-test",
			Destination: hyperbytedbv1alpha1.BackupDestination{
				S3: hyperbytedbv1alpha1.S3BackupSpec{
					Bucket:                "hyperbytedb-backups",
					Prefix:                "inst-1",
					Region:                "us-east-1",
					Endpoint:              "http://minio.platform.svc.cluster.local:9000",
					CredentialsSecretName: "hyperbytedb-backup-s3",
				},
			},
			RetentionDays: 7,
		},
	}
}

func TestBuildBackupJobSnapshotsThenUploadsWithMc(t *testing.T) {
	r := &HyperbytedbBackupReconciler{}
	job := r.buildBackupJob(testBackup(), testBackupCluster(), "db-test-backup-abcd")
	spec := job.Spec.Template.Spec

	if len(spec.InitContainers) != 1 {
		t.Fatalf("init containers = %d, want 1 (hyperbytedb snapshot)", len(spec.InitContainers))
	}
	init := spec.InitContainers[0]
	if init.Name != "snapshot" {
		t.Errorf("init name = %q, want snapshot", init.Name)
	}
	if init.Image != "ghcr.io/hyperbyte-cloud/hyperbytedb:0.8.5-beta" {
		t.Errorf("snapshot image = %q", init.Image)
	}
	script := strings.Join(init.Command, " ")
	if !strings.Contains(script, "hyperbytedb backup") {
		t.Errorf("snapshot command missing hyperbytedb backup: %s", script)
	}
	if strings.Contains(script, "aws ") {
		t.Errorf("snapshot must not call aws: %s", script)
	}

	if len(spec.Containers) != 1 {
		t.Fatalf("containers = %d, want 1 (mc upload)", len(spec.Containers))
	}
	up := spec.Containers[0]
	if up.Name != "upload" {
		t.Errorf("container name = %q, want upload", up.Name)
	}
	if !strings.Contains(up.Image, "minio/mc") {
		t.Errorf("upload image = %q, want minio/mc", up.Image)
	}
	upScript := strings.Join(up.Command, " ")
	if !strings.Contains(upScript, "mc ") {
		t.Errorf("upload command missing mc: %s", upScript)
	}
	if strings.Contains(upScript, "aws ") {
		t.Errorf("upload must not call aws: %s", upScript)
	}

	names := map[string]bool{}
	for _, v := range spec.Volumes {
		names[v.Name] = true
	}
	if !names["snapshot"] || !names["data"] {
		t.Errorf("volumes = %v, want snapshot emptyDir and data PVC", names)
	}
}

func TestBuildRestoreJobDownloadsWithMcThenRestores(t *testing.T) {
	r := &HyperbytedbRestoreReconciler{}
	restore := &hyperbytedbv1alpha1.HyperbytedbRestore{
		ObjectMeta: metav1.ObjectMeta{Name: "db-test-restore-abcd", Namespace: "tenant-1"},
		Spec: hyperbytedbv1alpha1.HyperbytedbRestoreSpec{
			ClusterName: "db-test",
			BackupName:  "db-test-backup-abcd",
		},
	}
	s3 := &hyperbytedbv1alpha1.S3BackupSpec{
		Bucket:                "hyperbytedb-backups",
		Prefix:                "inst-1",
		Endpoint:              "http://minio.platform.svc.cluster.local:9000",
		CredentialsSecretName: "hyperbytedb-backup-s3",
	}
	job := r.buildRestoreJob(restore, testBackupCluster(), s3, 0)
	spec := job.Spec.Template.Spec

	if len(spec.InitContainers) != 1 {
		t.Fatalf("init containers = %d, want 1 (mc download)", len(spec.InitContainers))
	}
	dl := spec.InitContainers[0]
	if dl.Name != "download" {
		t.Errorf("init name = %q, want download", dl.Name)
	}
	if !strings.Contains(dl.Image, "minio/mc") {
		t.Errorf("download image = %q, want minio/mc", dl.Image)
	}
	if strings.Contains(strings.Join(dl.Command, " "), "aws ") {
		t.Errorf("download must not call aws: %s", dl.Command)
	}

	if len(spec.Containers) != 1 {
		t.Fatalf("containers = %d, want 1 (hyperbytedb restore)", len(spec.Containers))
	}
	rst := spec.Containers[0]
	if rst.Image != "ghcr.io/hyperbyte-cloud/hyperbytedb:0.8.5-beta" {
		t.Errorf("restore image = %q", rst.Image)
	}
	script := strings.Join(rst.Command, " ")
	if !strings.Contains(script, "hyperbytedb restore") {
		t.Errorf("restore command missing hyperbytedb restore: %s", script)
	}
	if strings.Contains(script, "aws ") {
		t.Errorf("restore must not call aws: %s", script)
	}

	restore.Name = "db-7f4eaf37-8805-46e4-97d2-6a4c7d18c0e2-restore-3f839e85"
	job = r.buildRestoreJob(restore, testBackupCluster(), s3, 0)
	if n := len(job.Name); n > 63 {
		t.Fatalf("restore job name %q is %d bytes; Kubernetes job-name labels must be <=63", job.Name, n)
	}
}
