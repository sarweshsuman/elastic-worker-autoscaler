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
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	//customclient "k8s.io/metrics/pkg/client/custom_metrics"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	elasticclusterv1 "github.com/sarweshsuman/elastic-worker-autoscaler/api/v1"
)

var (
	jobOwnerKey = ".metadata.controller"
	apiGVStr    = elasticclusterv1.GroupVersion.String()
	apiGK       = elasticclusterv1.GroupVersion.WithKind("ElasticWorker").GroupKind()
)

// ElasticWorkerReconciler reconciles a ElasticWorker object
type ElasticWorkerReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=elasticcluster.sarweshsuman.com,resources=elasticworkers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=elasticcluster.sarweshsuman.com,resources=elasticworkers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get

func (r *ElasticWorkerReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("elasticworker", req.NamespacedName)

	log.V(1).Info("Reconciling", "ElasticWorker", req.Name)

	/* Read the ElasticWorker Object with name req.Name */
	var elasticWorker elasticclusterv1.ElasticWorker
	if err := r.Get(ctx, req.NamespacedName, &elasticWorker); err != nil {
		log.Error(err, "unable to fetch ElasticWorker object.")
		if errors.IsNotFound(err) {
			log.Error(err, "ElasticWorker object not found.")
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		return ctrl.Result{}, err
	}

	/* Validating ElasticWorker Object */

	valid := elasticclusterv1.ValidateElasticWorkerObject(&elasticWorker)
	if valid == false {
		// Validation failed
		if elasticWorker.Status.ObjectStatus != elasticclusterv1.InValidObject {
			elasticWorker.Status.ObjectStatus = elasticclusterv1.InValidObject
			if err := r.Status().Update(ctx, &elasticWorker); err != nil {
				log.Error(err, "unable to update ElasticWorker status")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, errors.NewInvalid(apiGK, req.Name, field.ErrorList{field.Invalid(field.NewPath("field"), "minReplicas", strconv.Itoa(elasticWorker.Spec.MinReplicas))})
	}

	// all validation passed
	if elasticWorker.Status.ObjectStatus != elasticclusterv1.ValidObject {
		elasticWorker.Status.ObjectStatus = elasticclusterv1.ValidObject
		if err := r.Status().Update(ctx, &elasticWorker); err != nil {
			log.Error(err, "unable to update ElasticWorker status")
			return ctrl.Result{}, err
		}
	}

	/* Validation End */

	// list all the pods in req.Namespace and of elasticworkers req.Name
	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.InNamespace(req.Namespace), client.MatchingFields{jobOwnerKey: req.Name}); err != nil {
		log.Error(err, "unable to list associated pods")
		return ctrl.Result{}, err
	}

	/* Segregate Pods */

	var pendingOrRunningPods []*corev1.Pod
	var otherPods []*corev1.Pod
	var nextPodNameIndex = 0

	for idx, item := range podList.Items {

		// Highest index for creating name of the replica pod.
		arr := strings.Split(item.Name, "-")
		index, err := strconv.Atoi(arr[len(arr)-2])
		if err != nil {
			log.Error(err, "unable to get nextIndex")
			index = 1
		}
		if index > nextPodNameIndex {
			nextPodNameIndex = index
		}

		if item.DeletionTimestamp != nil {
			otherPods = append(otherPods, &podList.Items[idx])
			continue
		}

		switch item.Status.Phase {
		case corev1.PodPending, corev1.PodRunning:
			pendingOrRunningPods = append(pendingOrRunningPods, &podList.Items[idx])
		default:
			otherPods = append(otherPods, &podList.Items[idx])
		}
	}

	log.V(1).Info("Pod(s)", "no. of running pods", len(pendingOrRunningPods))
	log.V(1).Info("Pod(s)", "no. of failed/succeeded/unknown pods", len(otherPods))

	currentRunningPods := len(pendingOrRunningPods)
	var acceptableRunningPods int
	if actualReplicas, ok := elasticWorker.Annotations["actualReplicas"]; ok {
		val, err := strconv.Atoi(actualReplicas)
		if err != nil {
			log.Error(err, "Unable to get actualReplicas from annotations")
			acceptableRunningPods = elasticWorker.Spec.MinReplicas
		} else {
			acceptableRunningPods = val
		}
	} else {
		acceptableRunningPods = elasticWorker.Spec.MinReplicas
	}

	// If Scale is set to other than 0
	acceptableRunningPods = acceptableRunningPods + elasticWorker.Spec.Scale
	if acceptableRunningPods > elasticWorker.Spec.MaxReplicas {
		acceptableRunningPods = elasticWorker.Spec.MaxReplicas
	} else if acceptableRunningPods < elasticWorker.Spec.MinReplicas {
		acceptableRunningPods = elasticWorker.Spec.MinReplicas
	}

	// Bringing the Actual State to Desired State
	if currentRunningPods < acceptableRunningPods {
		// Scale Out
		for ; currentRunningPods < acceptableRunningPods; currentRunningPods++ {
			nextPodNameIndex++
			pod, _ := r.createPodForElasticWorker(&elasticWorker, nextPodNameIndex)
			if err := r.Create(ctx, pod); err != nil {
				log.Error(err, "unable to create Pod for ElasticWorker", "ElasticWorker", elasticWorker)
				return ctrl.Result{}, err
			}
		}
	} else if currentRunningPods > acceptableRunningPods {
		// Scale In
		log.V(1).Info("Scaling In")

		if elasticWorker.Spec.Policy == nil ||
			elasticWorker.Spec.Policy.Name == elasticclusterv1.ScaleInImmediately {
			// Default ScaleInPolicy applies.

			podsTerminated := 0
			idx := currentRunningPods
			for ; idx > acceptableRunningPods; idx-- {

				if err := r.Delete(ctx, pendingOrRunningPods[idx-1]); err != nil {
					log.Error(err, "unable to delete pod")
					return ctrl.Result{}, err
				}

				podsTerminated++

			}

			log.V(1).Info("scale-in", "pods terminated", podsTerminated)

		} else {

			switch elasticWorker.Spec.Policy.Name {
			case elasticclusterv1.ScaleInDisabled:

				log.V(1).Info("scale-in", "scale-in policy set: ", elasticWorker.Spec.Policy.Name)

			case elasticclusterv1.ScaleInBySelector:

				if elasticWorker.Spec.Policy.Selector == nil ||
					elasticWorker.Spec.Policy.Selector.MatchLabels == nil {

					log.V(1).Info("scale-in", "policy", elasticWorker.Spec.Policy.Name, " property", "selector/MatchingFields not defined.")

				} else {

					// list all the pods in req.Namespace and owned by req.Name
					// and ScaleDownBySelector defined.
					selectorLabels := client.MatchingLabels{}
					for k, v := range elasticWorker.Spec.Policy.Selector.MatchLabels {
						selectorLabels[k] = v
					}
					log.V(1).Info("scale-in", "sector labels", selectorLabels)
					var podsToDelete corev1.PodList
					if err := r.List(ctx, &podsToDelete, client.InNamespace(req.Namespace), client.MatchingFields{jobOwnerKey: req.Name}, selectorLabels); err != nil {
						log.Error(err, "unable to list associated pods")
						return ctrl.Result{}, err
					}

					var candidateDeletePods []*corev1.Pod

					// Filter out terminating pods.
					for idx, item := range podsToDelete.Items {
						if item.DeletionTimestamp != nil {
							continue
						}
						candidateDeletePods = append(candidateDeletePods, &podsToDelete.Items[idx])
					}

					scaleInLimit := currentRunningPods - acceptableRunningPods

					log.V(1).Info("scale-in", "policy", elasticWorker.Spec.Policy.Name)
					log.V(1).Info("scale-in", "current state(no of pods)", currentRunningPods)
					log.V(1).Info("scale-in", "accepted state(no of pods)", acceptableRunningPods)
					log.V(1).Info("scale-in", "scaleInLimit", scaleInLimit)

					idx := 0
					for ; idx < scaleInLimit && idx < len(candidateDeletePods); idx++ {

						if err := r.Delete(ctx, candidateDeletePods[idx]); err != nil {
							log.Error(err, "unable to delete pod")
							return ctrl.Result{}, err
						}

					}

					log.V(1).Info("scale-in", "pods terminated", idx)

				}
			default:
				log.V(1).Info("unknown scale in policy.")
			}
		}

	}

	log.V(1).Info("Pods Terminated")

	// Finding out actualReplicas left.
	podList = corev1.PodList{}
	if err := r.List(ctx, &podList, client.InNamespace(req.Namespace), client.MatchingFields{jobOwnerKey: req.Name}); err != nil {
		log.Error(err, "unable to list associated pods")
		return ctrl.Result{}, err
	}

	var actualReplicas []*corev1.Pod
	// Filter out terminating pods.
	for idx, item := range podList.Items {
		if item.DeletionTimestamp != nil {
			continue
		}
		actualReplicas = append(actualReplicas, &podList.Items[idx])
	}

	log.V(1).Info("pods", "actual replicas count", len(actualReplicas))

	if elasticWorker.Annotations == nil {
		elasticWorker.Annotations = make(map[string]string)
	}
	elasticWorker.Annotations["actualReplicas"] = strconv.Itoa(len(actualReplicas))
	elasticWorker.Spec.Scale = 0
	if err := r.Update(ctx, &elasticWorker); err != nil {
		log.Error(err, "unable to update elasticWorker Object")
		return ctrl.Result{}, err
	}
	elasticWorker.Status.ActualReplicas = len(actualReplicas)
	if err := r.Status().Update(ctx, &elasticWorker); err != nil {
		log.Error(err, "unable to update elasticWorker status Object")
		return ctrl.Result{}, err
	}

	log.V(1).Info("Sync Complete")

	return ctrl.Result{}, nil
}

func (r *ElasticWorkerReconciler) SetupWithManager(mgr ctrl.Manager) error {

	if err := mgr.GetFieldIndexer().IndexField(&corev1.Pod{}, jobOwnerKey, func(rawObj runtime.Object) []string {
		// grab the job object, extract the owner...
		pod := rawObj.(*corev1.Pod)
		owner := metav1.GetControllerOf(pod)
		if owner == nil {
			return nil
		}
		if owner.APIVersion != apiGVStr || owner.Kind != "ElasticWorker" {
			return nil
		}
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&elasticclusterv1.ElasticWorker{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

func (r *ElasticWorkerReconciler) createPodForElasticWorker(elasticWorker *elasticclusterv1.ElasticWorker, idx int) (*corev1.Pod, error) {
	name := fmt.Sprintf("%s-replica-%d-%d", elasticWorker.Name, idx, time.Now().Unix())
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
			Name:        name,
			Namespace:   elasticWorker.Namespace,
		},
		Spec: *elasticWorker.Spec.Template.Spec.DeepCopy(),
	}
	for k, v := range elasticWorker.Spec.Template.Labels {
		pod.Labels[k] = v
	}
	if err := ctrl.SetControllerReference(elasticWorker, pod, r.Scheme); err != nil {
		return nil, err
	}
	return pod, nil
}
