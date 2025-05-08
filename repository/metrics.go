package repository

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// lastMirrorTimestamp is a Gauge that captures the timestamp of the last
	// successful git mirror
	lastMirrorTimestamp *prometheus.GaugeVec
	// mirrorCount is a Counter vector of git mirrors
	mirrorCount *prometheus.CounterVec
	// mirrorLatency is a Histogram vector that keeps track of git repo mirror durations
	mirrorLatency *prometheus.HistogramVec
)

// EnableMetrics will enable metrics collection for git mirrors.
// Available metrics are...
//   - git_last_mirror_timestamp - (tags: repo)
//     A Gauge that captures the Timestamp of the last successful git sync per repo.
//   - git_mirror_count - (tags: repo,success)
//     A Counter for each repo sync, incremented with each sync attempt and tagged with the result (success=true|false)
//   - git_mirror_latency_seconds - (tags: repo)
//     A Summary that keeps track of the git sync latency per repo.
func EnableMetrics(metricsNamespace string, registerer prometheus.Registerer) {
	lastMirrorTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "git_last_mirror_timestamp",
		Help:      "Timestamp of the last successful git mirror",
	},
		[]string{
			// name of the repository
			"repo",
		},
	)

	mirrorCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "git_mirror_count",
		Help:      "Count of git mirror operations",
	},
		[]string{
			// name of the repository
			"repo",
			// Whether the apply was successful or not
			"success",
		},
	)

	mirrorLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Name:      "git_mirror_latency_seconds",
		Help:      "Latency for git repo mirror",
		Buckets:   []float64{0.5, 1, 5, 10, 20, 30, 60, 90, 120, 150, 300},
	},
		[]string{
			// name of the repository
			"repo",
		},
	)

	registerer.MustRegister(
		lastMirrorTimestamp,
		mirrorCount,
		mirrorLatency,
	)
}

// recordGitMirror records a repository mirror attempt by updating all the
// relevant metrics
func recordGitMirror(repo string, success bool) {
	// if metrics not enabled return
	if lastMirrorTimestamp == nil || mirrorCount == nil {
		return
	}
	if success {
		lastMirrorTimestamp.With(prometheus.Labels{
			"repo": repo,
		}).Set(float64(time.Now().Unix()))
	}
	mirrorCount.With(prometheus.Labels{
		"repo":    repo,
		"success": strconv.FormatBool(success),
	}).Inc()
}

func updateMirrorLatency(repo string, start time.Time) {
	// if metrics not enabled return
	if mirrorLatency == nil {
		return
	}
	mirrorLatency.WithLabelValues(repo).Observe(time.Since(start).Seconds())
}
