package conversationaudit

import (
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const auditTimingSamples = 512

type auditPhaseTiming struct {
	Count    uint64  `json:"count"`
	Failures uint64  `json:"failures"`
	TotalMS  float64 `json:"total_ms"`
	P95MS    float64 `json:"p95_ms"`
	P99MS    float64 `json:"p99_ms"`
	samples  [auditTimingSamples]time.Duration
}

var auditTimings = struct {
	sync.Mutex
	phases map[string]*auditPhaseTiming
}{phases: make(map[string]*auditPhaseTiming)}

func observeAuditPhase(phase string, started time.Time, failed bool) {
	observeAuditDuration(phase, time.Since(started), failed)
}

func observeAuditDuration(phase string, duration time.Duration, failed bool) {
	auditTimings.Lock()
	defer auditTimings.Unlock()
	timing := auditTimings.phases[phase]
	if timing == nil {
		timing = &auditPhaseTiming{}
		auditTimings.phases[phase] = timing
	}
	timing.samples[timing.Count%auditTimingSamples] = duration
	timing.Count++
	timing.TotalMS += float64(duration) / float64(time.Millisecond)
	if failed {
		timing.Failures++
	}
}

// Quantiles cover the most recent 512 observations per phase on this process;
// counters are cumulative since startup. No content or identifiers are kept.
func getAuditMetrics(c *gin.Context) {
	phases := make(map[string]auditPhaseTiming)
	auditTimings.Lock()
	for name, timing := range auditTimings.phases {
		copyTiming := *timing
		samples := append([]time.Duration(nil), timing.samples[:min(timing.Count, auditTimingSamples)]...)
		slices.Sort(samples)
		if len(samples) > 0 {
			copyTiming.P95MS = float64(samples[(len(samples)*95+99)/100-1]) / float64(time.Millisecond)
			copyTiming.P99MS = float64(samples[(len(samples)*99+99)/100-1]) / float64(time.Millisecond)
		}
		phases[name] = copyTiming
	}
	auditTimings.Unlock()
	db, cancel := auditWriteDB()
	defer cancel()
	var pending struct {
		Count  int64
		Oldest int64
	}
	if err := db.Model(&ConversationAuditPendingResponse{}).Select("COUNT(*) AS count, COALESCE(MIN(created_at), 0) AS oldest").Scan(&pending).Error; err != nil {
		respondError(c, http.StatusServiceUnavailable, "conversation audit metrics are unavailable")
		return
	}
	oldestSeconds := int64(0)
	if pending.Count > 0 {
		oldestSeconds = max(0, time.Now().Unix()-pending.Oldest)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"phases": phases, "quantile_sample_limit": auditTimingSamples, "pending_count": pending.Count, "oldest_pending_seconds": oldestSeconds}})
}
