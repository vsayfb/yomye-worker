# GitHub environment tools

Run `configure-gh-staging.sh` for AWS staging or
`configure-gh-production.sh` for GCP production after applying the matching
Terraform root. Set `GH_REPO=owner/repository` to override repository detection;
an alternate Terraform root may be passed as the first argument. OIDC is used,
so no static cloud credential secret is stored in GitHub.
