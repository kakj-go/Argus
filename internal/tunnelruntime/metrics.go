package tunnelruntime

import "github.com/prometheus/client_golang/prometheus"

var (
	activeTunnels = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "argus", Subsystem: "tunnel", Name: "active",
		Help: "Current active SSH tunnels held by this process.",
	}, []string{"kind"})
	tunnelBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "argus", Subsystem: "tunnel", Name: "bytes_total",
		Help: "Bytes relayed through SSH tunnels.",
	}, []string{"kind"})
	throttledEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "argus", Subsystem: "tunnel", Name: "throttled_total",
		Help: "Tunnel relay writes delayed by the aggregate bandwidth limit.",
	}, []string{"kind"})
	reconnectEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "argus", Subsystem: "tunnel", Name: "reconnect_total",
		Help: "Tunnel reconnect attempts.",
	}, []string{"kind"})
	takeoverEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "argus", Subsystem: "tunnel", Name: "lease_takeover_total",
		Help: "Tunnel leases taken over from a previous owner.",
	}, []string{"kind"})
)

func init() {
	prometheus.MustRegister(activeTunnels, tunnelBytes, throttledEvents, reconnectEvents, takeoverEvents)
}

func AddActive(kind string, delta float64) {
	if kind != "" {
		activeTunnels.WithLabelValues(kind).Add(delta)
	}
}

func RecordBytes(kind string, count int) {
	if kind != "" && count > 0 {
		tunnelBytes.WithLabelValues(kind).Add(float64(count))
	}
}

func RecordThrottled(kind string) {
	if kind != "" {
		throttledEvents.WithLabelValues(kind).Inc()
	}
}

func RecordReconnect(kind string) {
	if kind != "" {
		reconnectEvents.WithLabelValues(kind).Inc()
	}
}

func RecordTakeover(kind string) {
	if kind != "" {
		takeoverEvents.WithLabelValues(kind).Inc()
	}
}
