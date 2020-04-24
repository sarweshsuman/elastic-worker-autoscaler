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
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ElasticWorkerSpec defines the desired state of ElasticWorker
type ElasticWorkerSpec struct {
	// Label selector for pods. Existing ReplicaSets whose pods are
	// selected by this will be the ones affected by this airflowelasticworker.
	// It must match the pod template's labels.
	Selector *metav1.LabelSelector `json:"selector" `

	// MinReplicas is the intial num of replica workers
	// that will be started and then can scale up to maxReplicas.
	MinReplicas int `json:"minReplicas"`

	// Scale is additional num of pods by which replicas needs to scale up or down.
	// Scale can be any num, negative or positive, controller will limit
	// actual replicas by MinReplicas <= ActualReplicas <= MaxReplicas
	// +optional
	Scale int `json:"scale,omitempty"`

	// MaxReplicas is the upper limit for the number of replicas to which the workers can scale up to.
	// It cannot be less that minReplicas.
	MaxReplicas int `json:"maxReplicas"`

	// ScaleInPolicy specifies how to scale in replicas
	// +optional
	Policy *ScaleInPolicy `json:"scaleInPolicy,omitempty"`

	// Template describes the pods that will be created.
	Template v1.PodTemplateSpec `json:"template"`
}

// ScaleInPolicy describes how the replicas will be scaled in.
// Only one of the following scale in policies may be specified.
// If none of the following policies is specified, the default one
// is ScaleInImmediately.
type ScaleInPolicy struct {
	// describes which policy to use to scale down replica
	Name PolicyName `json:"name"`

	// Label selector for pods to be scaled down.
	// It only ties to ScaleInBySelector Policy.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// PolicyName describes scale down policy to use.
// +kubebuilder:validation:Enum=immediate;selector;disabled
type PolicyName string

const (
	// ScaleInImmediately is policy that scales down pods immediately.
	// Pod selection is random.
	ScaleInImmediately PolicyName = "immediate"

	// ScaleInBySelector is policy that scales only those pods
	// that have defined labels.
	ScaleInBySelector PolicyName = "selector"

	// ScaleInDisabled disables scaling down.
	ScaleInDisabled PolicyName = "disabled"

	// DeferredScaleIn PolicyName = "deferred"
)

// AirflowElasticWorkerObjectStatus describes if the object is valid or invalid.
// +kubebuilder:validation:Enum=Valid;InValid
type ElasticWorkerObjectStatus string

const (
	// Valid Object is accepted by the controller
	ValidObject ElasticWorkerObjectStatus = "Valid"

	// InValid Object is not accepted by the controller
	InValidObject ElasticWorkerObjectStatus = "InValid"
)

// ElasticWorkerStatus defines the observed state of ElasticWorker
type ElasticWorkerStatus struct {
	// Specifies if ElasticWorker Object status is valid or not.
	// +optional
	ObjectStatus ElasticWorkerObjectStatus `json:"objectStatus,omitempty"`

	// Num of replicas available between MinReplicas and MaxReplicas
	// +optional
	ActualReplicas int `json:"actualReplicas,omitempty"`

	// Information when was the last time the status was updated.
	// +optional
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ElasticWorker is the Schema for the elasticworkers API
type ElasticWorker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ElasticWorkerSpec   `json:"spec,omitempty"`
	Status ElasticWorkerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ElasticWorkerList contains a list of ElasticWorker
type ElasticWorkerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ElasticWorker `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ElasticWorker{}, &ElasticWorkerList{})
}

// +kubebuilder:docs-gen:collapse=Root Object Definitions
