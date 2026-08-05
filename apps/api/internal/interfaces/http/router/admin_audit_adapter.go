package interfaceshttprouter

import inframetrics "github.com/shiyudesu/frux/internal/infra/metrics"

type adminAuditMetricsAdapter struct{}

func (adminAuditMetricsAdapter) RecordAdminAuditWrite(outcome, result string) {
	inframetrics.ObserveAdminAuditWrite(outcome, result)
}

func (adminAuditMetricsAdapter) RecordDeniedAttemptDrop() {
	inframetrics.ObserveAdminAuditWrite("denied", "dropped")
}
