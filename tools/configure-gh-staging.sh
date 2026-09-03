#!/usr/bin/env bash
set -euo pipefail
ENVIRONMENT=staging; SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"; TF="${1:-${SCRIPT_DIR}/../../infrastructure/aws-infrastructure/environment/prod}"
for cmd in gh terraform; do command -v "$cmd" >/dev/null || { echo "$cmd is required." >&2; exit 1; }; done
gh auth status >/dev/null; REPO="${GH_REPO:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}"
out() { terraform -chdir="$TF" output -raw "$1"; }; setv() { [[ -n "$2" ]] || { echo "$1 is empty." >&2; exit 1; }; gh variable set "$1" -R "$REPO" -e "$ENVIRONMENT" -b "$2"; }
gh api -X PUT "repos/${REPO}/environments/${ENVIRONMENT}" >/dev/null
setv DEPLOY_ROLE_ARN "$(out github_actions_deploy_role_arn)"; setv AWS_REGION "${STAGING_AWS_REGION:-eu-central-1}"; setv DEPLOY_BUCKET "$(out app_deployments_bucket_name)"; setv INSTANCE_NAME_TAG "${STAGING_INSTANCE_NAME_TAG:-yevmiye-worker}"
echo "Configured ${REPO}:${ENVIRONMENT}."
