# Grafana Cloud dashboard

Import `categorization-worker.json` from **Dashboards → New → Import** in Grafana Cloud. During import, select:

- the Prometheus-compatible Grafana Cloud Metrics datasource receiving OTLP metrics;
- the Tempo Grafana Cloud Traces datasource receiving OTLP traces.

The dashboard assumes the standard OpenTelemetry-to-Prometheus name translation used by Grafana Cloud and the worker's staging `service.name` of `worker`. The Service dashboard variable can be edited after import if the deployed `SERVICE_NAME` differs.

The worker currently records its SQS/domain metrics but does not record the registered HTTP metric instruments, so the dashboard intentionally does not claim `/health` or `/ready` request telemetry.

## Metric coverage

- successful, failed, and retryable SQS processing;
- end-to-end processing p50, p95, and p99;
- AI provider failures and provider-to-provider fallbacks;
- canonical category creation versus reuse;
- category creation ratio as a taxonomy-drift signal;
- Ollama embedding failures;
- recent sampled Tempo traces and a staging runbook.

The panels use low-cardinality labels emitted by the application: `message_type`, `generator`, `reason`, `from_generator`, `to_generator`, `workflow`, and `action`.
