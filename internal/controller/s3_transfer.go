package controller

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	hyperbytedbv1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

// Same tag Kind already pulls for the MinIO bucket job. The image includes /bin/sh.
const defaultS3ClientImage = "minio/mc:RELEASE.2024-11-21T17-21-54Z"

const (
	snapshotVolumeName = "snapshot"
	snapshotMountPath  = "/snapshot"
	dataVolumeName     = "data"
	dataMountPath      = "/var/lib/hyperbytedb"
)

func s3ClientImage() string {
	return defaultS3ClientImage
}

func s3Endpoint(s3 hyperbytedbv1alpha1.S3BackupSpec) string {
	if s3.Endpoint != "" {
		return s3.Endpoint
	}
	if s3.Region != "" && s3.Region != "us-east-1" {
		return "https://s3." + s3.Region + ".amazonaws.com"
	}
	return "https://s3.amazonaws.com"
}

func s3Prefix(s3 hyperbytedbv1alpha1.S3BackupSpec) string {
	p := strings.Trim(s3.Prefix, "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

func s3Env(s3 hyperbytedbv1alpha1.S3BackupSpec, extra ...corev1.EnvVar) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "S3_ENDPOINT", Value: s3Endpoint(s3)},
		{Name: "S3_BUCKET", Value: s3.Bucket},
		{Name: "S3_PREFIX", Value: s3Prefix(s3)},
	}
	if s3.CredentialsSecretName != "" {
		env = append(env,
			corev1.EnvVar{
				Name: "AWS_ACCESS_KEY_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: s3.CredentialsSecretName},
						Key:                  "access_key_id",
					},
				},
			},
			corev1.EnvVar{
				Name: "AWS_SECRET_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: s3.CredentialsSecretName},
						Key:                  "secret_access_key",
					},
				},
			},
		)
	}
	return append(env, extra...)
}

func snapshotVolume() corev1.Volume {
	return corev1.Volume{
		Name:         snapshotVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
}

func dataVolume(clusterName string, ordinal int32, readOnly bool) corev1.Volume {
	return corev1.Volume{
		Name: dataVolumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: fmt.Sprintf("data-%s-%d", clusterName, ordinal),
				ReadOnly:  readOnly,
			},
		},
	}
}

func snapshotBackupInitContainer(image string) corev1.Container {
	return corev1.Container{
		Name:  "snapshot",
		Image: image,
		Command: []string{"/bin/sh", "-ec", `echo "Starting hyperbytedb backup..."
hyperbytedb backup --output /snapshot
echo "Backup size: $(du -sh /snapshot | cut -f1)"
`},
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: dataMountPath, ReadOnly: true},
			{Name: snapshotVolumeName, MountPath: snapshotMountPath},
		},
	}
}

func mcUploadContainer(s3 hyperbytedbv1alpha1.S3BackupSpec, retentionDays int32) corev1.Container {
	if retentionDays < 1 {
		retentionDays = 7
	}
	script := fmt.Sprintf(`mc alias set dest "${S3_ENDPOINT}" "${AWS_ACCESS_KEY_ID}" "${AWS_SECRET_ACCESS_KEY}"
TIMESTAMP=$(date +%%Y%%m%%d-%%H%%M%%S)
DEST="dest/${S3_BUCKET}/${S3_PREFIX}${TIMESTAMP}"
echo "Uploading to ${DEST}..."
mc mirror /snapshot "${DEST}/"
echo "Cleaning up backups older than %d days..."
mc rm --recursive --force --older-than "%dd" "dest/${S3_BUCKET}/${S3_PREFIX}" || true
echo "Backup complete"
`, retentionDays, retentionDays)
	return corev1.Container{
		Name:    "upload",
		Image:   s3ClientImage(),
		Command: []string{"/bin/sh", "-ec", script},
		Env:     s3Env(s3),
		VolumeMounts: []corev1.VolumeMount{
			{Name: snapshotVolumeName, MountPath: snapshotMountPath, ReadOnly: true},
		},
	}
}

func mcDownloadInitContainer(s3 hyperbytedbv1alpha1.S3BackupSpec) corev1.Container {
	return corev1.Container{
		Name:  "download",
		Image: s3ClientImage(),
		Command: []string{"/bin/sh", "-ec", `mc alias set dest "${S3_ENDPOINT}" "${AWS_ACCESS_KEY_ID}" "${AWS_SECRET_ACCESS_KEY}"
SRC="dest/${S3_BUCKET}/${S3_PREFIX}"
echo "Downloading backup from ${SRC}..."
mc mirror "${SRC}" /snapshot/
`},
		Env: s3Env(s3),
		VolumeMounts: []corev1.VolumeMount{
			{Name: snapshotVolumeName, MountPath: snapshotMountPath},
		},
	}
}

func restoreDataContainer(image string, ordinal int32) corev1.Container {
	script := fmt.Sprintf(`RESTORE_DIR="/snapshot"
if [ ! -f "${RESTORE_DIR}/manifest.json" ]; then
  SUBDIR=$(find "${RESTORE_DIR}" -maxdepth 1 -mindepth 1 -type d | sort -r | head -1)
  if [ -n "${SUBDIR}" ] && [ -f "${SUBDIR}/manifest.json" ]; then
    RESTORE_DIR="${SUBDIR}"
    echo "Found backup in subdirectory: ${RESTORE_DIR}"
  fi
fi
echo "Restoring data to PVC..."
hyperbytedb restore --input "${RESTORE_DIR}"
echo "Restore complete for ordinal %d"
`, ordinal)
	return corev1.Container{
		Name:    "restore",
		Image:   image,
		Command: []string{"/bin/sh", "-ec", script},
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: dataMountPath},
			{Name: snapshotVolumeName, MountPath: snapshotMountPath, ReadOnly: true},
		},
	}
}
