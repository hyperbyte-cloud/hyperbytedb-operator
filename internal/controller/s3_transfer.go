package controller

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	hyperbytedbv1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

// The S3 client for backup upload and restore download. minio/mc is gone
// from Docker Hub (and quay.io/minio needs a login), so jobs on a node
// without it cached could not start. The AWS CLI image is maintained, has
// /bin/sh, works with MinIO and other S3-compatible stores, and trusts the
// CA bundle shipped in botocore, which no longer holds the compromised
// eMudhra emSign ECC Root CA - G3 (CVE-2026-111943).
const defaultS3ClientImage = "amazon/aws-cli:2.38.5"

const (
	snapshotVolumeName = "snapshot"
	snapshotMountPath  = "/snapshot"
	dataVolumeName     = "data"
	dataMountPath      = "/var/lib/hyperbytedb"
)

// shellCommand runs script with /bin/sh, stopping at the first failure.
func shellCommand(script string) []string {
	return []string{"/bin/sh", "-ec", script}
}

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

func s3Region(s3 hyperbytedbv1alpha1.S3BackupSpec) string {
	if s3.Region != "" {
		return s3.Region
	}
	return "us-east-1"
}

func s3Env(s3 hyperbytedbv1alpha1.S3BackupSpec, extra ...corev1.EnvVar) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "S3_ENDPOINT", Value: s3Endpoint(s3)},
		{Name: "S3_BUCKET", Value: s3.Bucket},
		{Name: "S3_PREFIX", Value: s3Prefix(s3)},
		{Name: "AWS_DEFAULT_REGION", Value: s3Region(s3)},
		// No credential files or config in the image's home: everything
		// comes from the environment.
		{Name: "AWS_CONFIG_FILE", Value: "/tmp/aws-config"},
	}
	// A custom endpoint (MinIO and other S3-compatible stores) takes
	// path-style bucket addressing; AWS itself is addressed by region.
	if s3.Endpoint != "" {
		env = append(env, corev1.EnvVar{Name: "S3_PATH_STYLE", Value: "1"})
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
		Command: shellCommand(`echo "Starting hyperbytedb backup..."
hyperbytedb backup --output /snapshot
echo "Backup size: $(du -sh /snapshot | cut -f1)"
`),
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: dataMountPath, ReadOnly: true},
			{Name: snapshotVolumeName, MountPath: snapshotMountPath},
		},
	}
}

// awsSetup is the preamble of every S3 client script: path-style addressing
// for a custom endpoint, and $EP for the endpoint flag.
const awsSetup = `if [ -n "${S3_PATH_STYLE:-}" ]; then
  aws configure set default.s3.addressing_style path
  EP="--endpoint-url ${S3_ENDPOINT}"
else
  EP=""
fi
`

func s3UploadContainer(s3 hyperbytedbv1alpha1.S3BackupSpec, retentionDays int32) corev1.Container {
	if retentionDays < 1 {
		retentionDays = 7
	}
	// Backups go to <prefix><UTC timestamp>/; retention deletes the
	// timestamped folders older than the cutoff, by name, as mc's
	// --older-than did by object age.
	script := awsSetup + fmt.Sprintf(`TIMESTAMP=$(date -u +%%Y%%m%%d-%%H%%M%%S)
DEST="s3://${S3_BUCKET}/${S3_PREFIX}${TIMESTAMP}/"
echo "Uploading to ${DEST}..."
aws $EP s3 sync /snapshot "${DEST}" --only-show-errors
echo "Cleaning up backups older than %d days..."
CUTOFF=$(date -u -d "-%d days" +%%Y%%m%%d-%%H%%M%%S)
aws $EP s3 ls "s3://${S3_BUCKET}/${S3_PREFIX}" | awk '$1 == "PRE" { print $2 }' | while read -r dir; do
  ts="${dir%%/}"
  case "$ts" in
    [0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9]) ;;
    *) continue ;;
  esac
  if [ "$ts" \< "$CUTOFF" ]; then
    echo "Removing ${dir}"
    aws $EP s3 rm --recursive "s3://${S3_BUCKET}/${S3_PREFIX}${dir}" --only-show-errors
  fi
done || true
echo "Backup complete"
`, retentionDays, retentionDays)
	return corev1.Container{
		Name:    "upload",
		Image:   s3ClientImage(),
		Command: shellCommand(script),
		Env:     s3Env(s3),
		VolumeMounts: []corev1.VolumeMount{
			{Name: snapshotVolumeName, MountPath: snapshotMountPath, ReadOnly: true},
		},
	}
}

// s3DownloadInitContainer fetches the newest timestamped backup under the
// prefix into /snapshot/<timestamp>/, where the restore step looks for it;
// with none, the whole prefix (a backup stored at its root).
func s3DownloadInitContainer(s3 hyperbytedbv1alpha1.S3BackupSpec) corev1.Container {
	return corev1.Container{
		Name:  "download",
		Image: s3ClientImage(),
		Command: shellCommand(awsSetup + `SRC="s3://${S3_BUCKET}/${S3_PREFIX}"
LATEST=$(aws $EP s3 ls "${SRC}" | awk '$1 == "PRE" { print $2 }' \
  | grep -E '^[0-9]{8}-[0-9]{6}/$' | sort | tail -1 || true)
if [ -n "${LATEST}" ]; then
  echo "Downloading backup ${SRC}${LATEST}..."
  aws $EP s3 sync "${SRC}${LATEST}" "/snapshot/${LATEST}" --only-show-errors
else
  echo "Downloading backup from ${SRC}..."
  aws $EP s3 sync "${SRC}" /snapshot/ --only-show-errors
fi
`),
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
		Command: shellCommand(script),
		VolumeMounts: []corev1.VolumeMount{
			{Name: dataVolumeName, MountPath: dataMountPath},
			{Name: snapshotVolumeName, MountPath: snapshotMountPath, ReadOnly: true},
		},
	}
}
