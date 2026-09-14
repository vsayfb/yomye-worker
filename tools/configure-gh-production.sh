#!/usr/bin/env bash
set -euo pipefail
ENVIRONMENT=production; SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"; TF="${1:-${SCRIPT_DIR}/../../infrastructure/gcp-infrastructure}"
for cmd in gh terraform; do command -v "$cmd" >/dev/null || { echo "$cmd is required." >&2; exit 1; }; done
gh auth status >/dev/null; REPO="${GH_REPO:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}"
out() { terraform -chdir="$TF" output -raw "$1"; }; setv() { [[ -n "$2" ]] || { echo "$1 is empty." >&2; exit 1; }; gh variable set "$1" -R "$REPO" -e "$ENVIRONMENT" -b "$2"; }
gh api -X PUT "repos/${REPO}/environments/${ENVIRONMENT}" >/dev/null
setv GCP_WORKLOAD_IDENTITY_PROVIDER "$(out github_workload_identity_provider)"; setv GCP_DEPLOY_SERVICE_ACCOUNT "$(out github_deploy_service_account)"
setv GCP_PROJECT_ID "$(out project_id)"; setv GCP_ZONE "$(out compute_zone)"; setv GCP_WORKER_INSTANCE "$(out worker_instance)"; setv GCS_DEPLOY_BUCKET "$(out app_deployments_bucket)"
echo "Configured ${REPO}:${ENVIRONMENT}."
