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

package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	custommetrics "k8s.io/metrics/pkg/client/custom_metrics"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	elasticclusterv1 "github.com/sarweshsuman/elastic-worker-autoscaler/api/v1"
)

var (
	gvk = schema.FromAPIVersionAndKind("v1", "Pod")
)

// ElasticWorkerAutoscalerReconciler reconciles a ElasticWorkerAutoscaler object
type ElasticWorkerAutoscalerReconciler struct {
	CustomMetricsClient custommetrics.CustomMetricsClient
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=elasticcluster.sarweshsuman.com,resources=elasticworkerautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=elasticcluster.sarweshsuman.com,resources=elasticworkerautoscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=elasticcluster.sarweshsuman.com,resources=elasticworkers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=elasticcluster.sarweshsuman.com,resources=elasticworkers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get
// +kubebuilder:rbac:groups="custom.metrics.k8s.io",resources=pods/total_cluster_load,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="custom.metrics.k8s.io",resources=pods/load,verbs=get;list;watch;create;update;patch;delete

func (r *ElasticWorkerAutoscalerReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("Elastic Worker Autoscaler", req.NamespacedName)

	log.Info("Start Processing")
	defer log.Info("Stop Processing")

	// retrieving corresponding ElasticWorkerAutoscaler object
	elasticWorkerAutoscaler, err := r.getElasticWorkerAutoscalerObject(req)
	if err != nil {
		log.Error(err, "Error in retrieving ElasticWorkerAutoscaler object, no reattempt will be made on this object.")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// retrieving corresponding ElasticWorker object
	elasticWorker, namespacedName, err := r.getElasticWorkerObject(req, elasticWorkerAutoscaler)
	if err != nil {
		// TODO: after 5 unsuccessful attempts mark this elastic worker autoscaler object as invalid
		// and don't attempt retry
		log.Error(err, "Error in retrieving ElasticWorker object, re-attempt in 5 seconds")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, client.IgnoreNotFound(err)
	}

	log.V(1).Info("info", "ElasticWorkerAutoscaler Name", elasticWorkerAutoscaler.ObjectMeta.Name)
	log.V(1).Info("info", "ElasticWorker Name", elasticWorker.ObjectMeta.Name)

	loadStatsInt, err := r.getClusterMetric(elasticWorkerAutoscaler, elasticWorker)
	if err != nil {
		log.Error(err, "Error in retrieving cluster metric, re-attempt in 5 seconds")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	log.V(1).Info("cluster-metric-retrieved", elasticWorkerAutoscaler.Spec.Metric.Name, loadStatsInt)

	log.V(1).Info("info", "total cluster load metric", loadStatsInt)
	log.V(1).Info("info", "target value of metric", elasticWorkerAutoscaler.Spec.TargetValue)

	if loadStatsInt > elasticWorkerAutoscaler.Spec.TargetValue {
		// Need to scale out the workers
		log.Info("scaling-out")

		// resetting scale-in scaleInBackOffPeriod
		if elasticWorkerAutoscaler.Annotations == nil {
			elasticWorkerAutoscaler.Annotations = make(map[string]string)
		}
		_, ok := elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"]
		if ok == false {
			elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"] = "true"
		}

		if elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"] == "true" {
			elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"] = "false"
			if err := r.Update(ctx, elasticWorkerAutoscaler); err != nil {
				log.Error(err, "unable to update ElasticWorkerAutoscaler Object")
				return ctrl.Result{}, err
			}
		}

		newReplicas := int(math.Ceil(float64(loadStatsInt*elasticWorker.Status.ActualReplicas) / float64(elasticWorkerAutoscaler.Spec.TargetValue)))
		scaleOutBy := newReplicas - elasticWorker.Status.ActualReplicas

		log.V(1).Info("scaling-out", "scale out by:", scaleOutBy)

		if scaleOutBy == 0 {
			log.V(1).Info("scaling-out", "no scale out will be performed", "reattempt in 5 seconds")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		elasticWorker.Spec.Scale = scaleOutBy
		if err := r.Update(ctx, elasticWorker); err != nil {
			log.Error(err, "unable to update elasticWorker Object")
			return ctrl.Result{}, err
		}
		log.V(1).Info("scaling-out-complete")

	} else if loadStatsInt <= int(float64(elasticWorkerAutoscaler.Spec.TargetValue)*0.70) {
		// Need to scale in the workers

		if elasticWorker.Status.ActualReplicas == elasticWorker.Spec.MinReplicas {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		log.Info("scaling-in")
		/* will not attempt to scale-in at first opportunity */
		scaleInBackOffInSeconds := elasticWorkerAutoscaler.Spec.ScaleIn.ScaleInBackOff
		if scaleInBackOffInSeconds == 0 {
			// default 30 seconds backoff
			scaleInBackOffInSeconds = 30
		}
		if elasticWorkerAutoscaler.Annotations == nil {
			elasticWorkerAutoscaler.Annotations = make(map[string]string)
		}
		scaleInBackOffPeriod, ok := elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"]
		if ok == false {
			elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"] = "false"
			scaleInBackOffPeriod = "false"
		}
		if scaleInBackOffPeriod == "false" {
			log.V(1).Info(fmt.Sprintf("Backin off scale-in for %v seconds", scaleInBackOffInSeconds))
			elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"] = "true"
			elasticWorkerAutoscaler.Annotations["scaleInBackOffStartTime"] = strconv.FormatInt(time.Now().Unix(), 10)
			if err := r.Update(ctx, elasticWorkerAutoscaler); err != nil {
				log.Error(err, "unable to update ElasticWorkerAutoscaler Object")
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		} else {
			log.V(1).Info("Scale In Backoff Period is Active")
			scaleInBackOffStartTime, ok := elasticWorkerAutoscaler.Annotations["scaleInBackOffStartTime"]
			if ok == false {
				log.V(1).Info("Error, scaleInBackOffStartTime not set in annotation, reattempt in 5 seconds")
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			scaleInBackOffStartTimeInSeconds, err := strconv.Atoi(scaleInBackOffStartTime)
			if err != nil {
				log.V(1).Info("Error, unable to parse annoation scaleInBackOffStartTime, re-attempt in 5 seconds")
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			if int64(scaleInBackOffStartTimeInSeconds+scaleInBackOffInSeconds) >= time.Now().Unix() {
				log.V(1).Info("Scale In Backoff Duration not crossed")
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			// backoff duration complete
			elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"] = "false"
			if err := r.Update(ctx, elasticWorkerAutoscaler); err != nil {
				log.Error(err, "unable to update ElasticWorkerAutoscaler Object")
				return ctrl.Result{}, err
			}
		}

		newReplicas := int(math.Ceil(float64(loadStatsInt*elasticWorker.Status.ActualReplicas) / float64(elasticWorkerAutoscaler.Spec.TargetValue)))
		/*
			newReplicas should not be lower than minReplicas.
			If it is, then we scale it down to minReplicas only.
			Although, ElasticWorker controller handles this automatically,
			but this controller also shuts down the process in the pod before,
			and we don't want to shutdown the processes from minReplicas.
		*/
		if newReplicas < elasticWorker.Spec.MinReplicas {
			newReplicas = elasticWorker.Spec.MinReplicas
		}
		scaleInBy := elasticWorker.Status.ActualReplicas - newReplicas
		log.V(1).Info("scaling-in", "scale in by:", scaleInBy)

		if scaleInBy == 0 {
			log.V(1).Info("scaling-in", "no scale in will be performed", "")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// Finding all the pods controlled by ElasticWorker
		var podList = corev1.PodList{}
		if err := r.List(ctx, &podList, client.InNamespace(namespacedName.Namespace), client.MatchingFields{jobOwnerKey: namespacedName.Name}); err != nil {
			log.Error(err, fmt.Sprintf("Unable to list pods for ElasticWorker %v in namespace %v, re-attempt in 5 seconds", namespacedName.Name, namespacedName.Namespace))
			return ctrl.Result{RequeueAfter: 5 * time.Second}, err
		}

		if len(podList.Items) == 0 {
			log.V(1).Info("scaling-in", fmt.Sprintf("no pod found for ElasticWorker %v in namspace %v, reattempt in 5 seconds", elasticWorker.ObjectMeta.Name, namespacedName.Namespace), "")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// retrieving metric for each pod and extracting based on load=0
		candidatePods, err := r.getMetricSortedPodList(elasticWorkerAutoscaler, podList)
		if err != nil {
			log.Error(err, fmt.Sprintf("unable to get sorted pod list via metric, will attempt in 5 seconds"))
			return ctrl.Result{RequeueAfter: 5 * time.Second}, client.IgnoreNotFound(err)
		}

		if len(*candidatePods) == 0 {
			log.V(1).Info("scaling-in", fmt.Sprintf("no candidate pod(i.e. load 0) found for ElasticWorker %v in namspace %v, reattempt in 5 seconds", elasticWorker.ObjectMeta.Name, namespacedName.Namespace), "")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		log.V(1).Info("scaling-in", "num of pods with 0 load", len(*candidatePods))

		// label to use for termination of pod via selector policy
		terminationLabel := make(map[string]string)
		if elasticWorkerAutoscaler.Spec.ScaleIn.MarkForTerminationLabel != nil {
			for k, v := range elasticWorkerAutoscaler.Spec.ScaleIn.MarkForTerminationLabel {
				terminationLabel[k] = v
			}
		} else {
			terminationLabel["delete"] = "true"
		}

		// pods that will be labeled with termination label
		var actualCandidatePods PodMetricList
		if scaleInBy > len(*candidatePods) {
			actualCandidatePods = *candidatePods
		} else {
			actualCandidatePods = (*candidatePods)[:scaleInBy]
		}

		// before labeling, will attempt to send shutdown notification to pods
		podNames := []string{}
		for idx := range actualCandidatePods {
			podNames = append(podNames, actualCandidatePods[idx].Pod.ObjectMeta.Name)
		}

		// This is limitation where shutdown hook is expected to be only one for one cluster
		// it needs to be stateless, in case same pods are sent multiple times
		// it needs to be fast.
		requestBody, err := json.Marshal(map[string][]string{"shutdown_workers": podNames})
		if err != nil {
			log.Error(err, "unable to marshal request body for shutdown hook call, reattempt in 5 seconds")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// sending request to shutdown hook
		// needs to be a sync request, in case of async, there is still chance of data loss
		resp, err := http.Post(elasticWorkerAutoscaler.Spec.ScaleIn.ShutdownHttpHook, "application/json", bytes.NewBuffer(requestBody))
		if err != nil {
			log.Error(err, "unable to send shutdown hook request, reattempt in 5 seconds")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		if resp.StatusCode != http.StatusOK {
			log.Error(err, fmt.Sprintf("unable to shutdown pods, status code of shutdown hook is %v", resp.StatusCode))
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// if any process fails after this, it can be safely retried

		// setting termination label
		for _, item := range actualCandidatePods {
			log.V(1).Info("scaling-in", "setting termination label on pod name:", item.Pod.ObjectMeta.Name)

			for k, v := range terminationLabel {
				item.Pod.ObjectMeta.Labels[k] = v
			}
			if err := r.Update(ctx, &item.Pod); err != nil {
				log.Error(err, fmt.Sprintf("unable to update pod: %v", err))
				return ctrl.Result{RequeueAfter: 5 * time.Second}, err
			}
		}

		// setting scale property ElasticWorker object
		if len(actualCandidatePods) > 0 {
			elasticWorker.Spec.Scale = -1 * len(actualCandidatePods)
			if err := r.Update(ctx, elasticWorker); err != nil {
				log.Error(err, "unable to update elasticWorker Object")
				return ctrl.Result{RequeueAfter: 5 * time.Second}, err
			}

			log.V(1).Info("scaling-in", "scale in complete for pods", len(actualCandidatePods))
		}

	} else {
		log.V(4).Info("auto-scaling", "Not scaling. Load is in acceptable limit: ", loadStatsInt)
		// resetting scale-in scaleInBackOffPeriod if required
		if elasticWorkerAutoscaler.Annotations == nil {
			elasticWorkerAutoscaler.Annotations = make(map[string]string)
		}
		_, ok := elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"]
		if ok == false {
			elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"] = "true"
		}

		if elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"] == "true" {
			elasticWorkerAutoscaler.Annotations["scaleInBackOffPeriod"] = "false"
			if err := r.Update(ctx, elasticWorkerAutoscaler); err != nil {
				log.Error(err, "unable to update ElasticWorkerAutoscaler Object")
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *ElasticWorkerAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&elasticclusterv1.ElasticWorkerAutoscaler{}).
		Complete(r)
}

// Retrieve ElasticWorkerAutoscaler Objects from cache
func (r *ElasticWorkerAutoscalerReconciler) getElasticWorkerAutoscalerObject(req ctrl.Request) (*elasticclusterv1.ElasticWorkerAutoscaler, error) {
	ctx := context.Background()
	log := r.Log.WithValues("Elastic Worker Autoscaler", req.NamespacedName)

	var elasticWorkerAutoscaler elasticclusterv1.ElasticWorkerAutoscaler

	if err := r.Get(ctx, req.NamespacedName, &elasticWorkerAutoscaler); err != nil {
		log.Error(err, fmt.Sprintf("unable to fetch ElasticWorkerAutoscaler object with name %s in namespace %s", req.Name, req.NamespacedName))
		if errors.IsNotFound(err) {
			log.Error(err, "ElasticWorkerAutoscaler object not found.")
			return nil, err
		}
		return nil, err
	}
	return &elasticWorkerAutoscaler, nil
}

func (r *ElasticWorkerAutoscalerReconciler) getElasticWorkerObject(req ctrl.Request, elasticWorkerAutoscaler *elasticclusterv1.ElasticWorkerAutoscaler) (*elasticclusterv1.ElasticWorker, types.NamespacedName, error) {
	ctx := context.Background()
	log := r.Log.WithValues("Elastic Worker Autoscaler", req.NamespacedName)

	var elasticWorker elasticclusterv1.ElasticWorker

	// building namespaced name from Elastic Worker Autoscaler object.
	namespacedName := types.NamespacedName{
		Namespace: elasticWorkerAutoscaler.Spec.ScaleTargetRef.Namespace,
		Name:      elasticWorkerAutoscaler.Spec.ScaleTargetRef.Name,
	}

	if err := r.Get(ctx, namespacedName, &elasticWorker); err != nil {
		log.Error(err, fmt.Sprintf("unable to fetch ElasticWorker object with name %s in namespace %s", namespacedName.Name, namespacedName.Namespace))
		if errors.IsNotFound(err) {
			log.Error(err, "ElasticWorker object not found.")
			return nil, namespacedName, err
		}
		return nil, namespacedName, err
	}
	return &elasticWorker, namespacedName, nil
}
