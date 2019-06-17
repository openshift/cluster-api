/*
Copyright 2019 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type FunctionLabel string

const (
	clusterAPISubsystem               = "cluster_api"
	Create              FunctionLabel = "CreateMachine"
	Delete              FunctionLabel = "DeleteMachine"
)

var (
	machineCreateCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Subsystem: clusterAPISubsystem,
			Name:      "machine_create_total",
			Help:      "Cumulative number of machine create api calls",
		},
	)

	machineDeleteCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Subsystem: clusterAPISubsystem,
			Name:      "machine_delete_total",
			Help:      "Cumulative number of machine delete api calls",
		},
	)

	failedMachineCreateCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: clusterAPISubsystem,
			Name:      "failed_machine_create_total",
			Help:      "Number of times provider instance create has failed.",
		}, []string{"reason"},
	)

	failedMachineDeleteCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: clusterAPISubsystem,
			Name:      "failed_machine_delete_total",
			Help:      "Number of times provider instance delete has failed.",
		}, []string{"reason"},
	)

	functionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: clusterAPISubsystem,
			Name:      "function_duration_seconds",
			Help:      "Time taken by various functions",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.5, 5.0, 7.5, 10.0, 12.5, 15.0, 17.5, 20.0, 22.5, 25.0, 27.5, 30.0, 50.0, 75.0, 100.0, 1000.0},
		}, []string{"function"},
	)
)

// RegisterAll registers all metrics.
func RegisterAll() {
	metrics.Registry.MustRegister(machineCreateCount)
	metrics.Registry.MustRegister(machineDeleteCount)
	metrics.Registry.MustRegister(failedMachineCreateCount)
	metrics.Registry.MustRegister(failedMachineDeleteCount)
	metrics.Registry.MustRegister(functionDuration)
}

// RegisterMachineCreate records a create operation
func RegisterMachineCreate() {
	machineCreateCount.Inc()
}

// RegisterMachineDelete records a delete operation
func RegisterMachineDelete() {
	machineDeleteCount.Inc()
}

// RegisterFailedMachineCreate records a failed create operation
func RegisterFailedMachineCreate(reason string) {
	failedMachineCreateCount.WithLabelValues(reason).Inc()
}

// RegisterFailedMachineDelete records a failed delete operation
func RegisterFailedMachineDelete(reason string) {
	failedMachineDeleteCount.WithLabelValues(reason).Inc()
}

// UpdateDurationFromStart records the duration of the step identified by the
// label using start time
func UpdateDurationFromStart(label FunctionLabel, start time.Time) {
	duration := time.Now().Sub(start)
	UpdateDuration(label, duration)
}

// UpdateDuration records the duration of the step identified by the label
func UpdateDuration(label FunctionLabel, duration time.Duration) {
	functionDuration.WithLabelValues(string(label)).Observe(duration.Seconds())
}
