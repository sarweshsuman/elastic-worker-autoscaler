/*


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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ElasticWorkerAutoscalerSpec defines the desired state of ElasticWorkerAutoscaler
type ElasticWorkerAutoscalerSpec struct {

	// scaleTargetRef points to the target ElasticWorker object to scale
	ScaleTargetRef CrossVersionObjectReference `json:"scaleTargetRef"`

	// metric defines what metric to collect
	// scope is limited to custom metrics only
	// metric is limited to resource type pods only
	Metric MetricSpec `json:"metricSpec"`

	// targetValue currently supported as percentage of Load only
	TargetValue int `json:"targetValue"`

	// shutdown hook is called so that the processes within pods are
	// shutdown safely before those pods are labelled for termination.
	ScaleIn ScaleInSpec `json:"scaleInSpec"`
}

// Currently it only supports http hooks
type ScaleInSpec struct {
	// http endpoint to hit to start shutdown of a pod
	// Limitation:
	// currently this is specific to flower api
	// it needs to be fast and stateless and siliently ignore error
	// if same pod is sent multiple times, it should not error out.
	ShutdownHttpHook string `json:"shutdownHttpHook"`

	// to retrieve worker pod specific metric
	Metric MetricSpec `json:"podMetricSpec"`

	// label to use to set for marking a pod to be
	// applicable for termination
	// +optional
	MarkForTerminationLabel Labels `json:"markForTerminationLabel,omitempty"`

	// Scale in back off in seconds.
	// By default it waits 30 seconds to scale-in since it saw first scale-in opportunity.
	// +optional
	ScaleInBackOff int `json:"scaleInBackOff,omitempty"`
}

type Labels map[string]string

type CrossVersionObjectReference struct {
	// Kind of the referent;
	// Kind is fixed at ElasticWorker
	// Kind string `json:"kind"`
	// Name of the referent;
	Name string `json:"name"`
	// namespace of the referent object;
	Namespace string `json:"namespace"`
	// API version of the referent
	// +optional
	// APIVersion is same as ElasticWorkerAutoscaler type.
	// APIVersion string `json:"apiVersion,omitempty"`
}

type MetricSpec struct {
	// metric name to retrieve, like total_cluster_load
	Name string `json:"name"`
	// resource name can be for example pod name or a fixed name like cluster name
	// If this is not provided, then pod name will be used.
	// +optional
	ResourceName string `json:"resourceName,omitempty"`
	// namespace where the resource is located
	Namespace string `json:"namespace"`
}

/*
TODO: Implementation Pending
// AirflowElasticWorkerObjectStatus describes if the object is valid or invalid.
// +kubebuilder:validation:Enum=Valid;InValid
type ElasticWorkerAutoscalerObjectStatus string

const (
	// Valid Object is accepted by the controller
	ValidObject ElasticWorkerAutoscalerObjectStatus = "Valid"

	// InValid Object is not accepted by the controller
	InValidObject ElasticWorkerAutoscalerObjectStatus = "InValid"
)
*/

// ElasticWorkerAutoscalerStatus defines the observed state of ElasticWorkerAutoscaler
type ElasticWorkerAutoscalerStatus struct {
	/*
		TODO: Implement status object
		// Specifies if ElasticWorker Object status is valid or not.
		// +optional
		ObjectStatus ElasticWorkerAutoscalerObjectStatus `json:"objectStatus,omitempty"`

		// reason if the object is invalid
		// +optional
		Reason string `json:"reason,omitempty"`

		// Information when was the last scale in was successfully completed
		// +optional
		LastScaledAt *metav1.Time `json:"lastScaledAt,omitempty"`
	*/
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ElasticWorkerAutoscaler is the Schema for the elasticworkerautoscalers API
type ElasticWorkerAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ElasticWorkerAutoscalerSpec   `json:"spec,omitempty"`
	Status ElasticWorkerAutoscalerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ElasticWorkerAutoscalerList contains a list of ElasticWorkerAutoscaler
type ElasticWorkerAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ElasticWorkerAutoscaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ElasticWorkerAutoscaler{}, &ElasticWorkerAutoscalerList{})
}

// +kubebuilder:docs-gen:collapse=Root Object Definitions
