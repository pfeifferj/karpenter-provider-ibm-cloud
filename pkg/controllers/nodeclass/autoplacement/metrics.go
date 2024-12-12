package autoplacement

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	instanceTypeSelections = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "karpenter_ibm_instance_type_selections_total",
			Help: "Number of automatic instance type selections performed",
		},
		[]string{"nodeclass", "result"},
	)

	subnetSelections = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "karpenter_ibm_subnet_selections_total",
			Help: "Number of automatic subnet selections performed",
		},
		[]string{"nodeclass", "result"},
	)

	selectedInstanceTypes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "karpenter_ibm_selected_instance_types",
			Help: "Number of instance types selected for each nodeclass",
		},
		[]string{"nodeclass"},
	)

	selectedSubnets = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "karpenter_ibm_selected_subnets",
			Help: "Number of subnets selected for each nodeclass",
		},
		[]string{"nodeclass"},
	)

	instanceTypeSelectionLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "karpenter_ibm_instance_type_selection_duration_seconds",
			Help:    "Duration of instance type selection operations",
			Buckets: []float64{0.1, 0.5, 1.0, 2.0, 5.0},
		},
		[]string{"nodeclass"},
	)

	subnetSelectionLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "karpenter_ibm_subnet_selection_duration_seconds",
			Help:    "Duration of subnet selection operations",
			Buckets: []float64{0.1, 0.5, 1.0, 2.0, 5.0},
		},
		[]string{"nodeclass"},
	)
)

func init() {
	// Register metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		instanceTypeSelections,
		subnetSelections,
		selectedInstanceTypes,
		selectedSubnets,
		instanceTypeSelectionLatency,
		subnetSelectionLatency,
	)
}
