package controllers

import (
	"fmt"
	"k8s.io/apimachinery/pkg/labels"
	"sort"

	elasticclusterv1 "github.com/sarweshsuman/elastic-worker-autoscaler/api/v1"
	corev1 "k8s.io/api/core/v1"
)

// this can be redesigned to accept any metric value
type PodMetric struct {
	// pod for which this object is
	Pod corev1.Pod
	// metric name fetched
	MetricName string
	// metric value
	MetricValue int64
}

type PodMetricList []PodMetric

func (p PodMetricList) Len() int           { return len(p) }
func (p PodMetricList) Less(i, j int) bool { return p[i].MetricValue < p[j].MetricValue }
func (p PodMetricList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// empty metric selector labels
var emptyMetricSelectorLabels = map[string]string{}

// get cluster metric, ex. total_cluster_load
func (r *ElasticWorkerAutoscalerReconciler) getClusterMetric(elasticWorkerAutoscaler *elasticclusterv1.ElasticWorkerAutoscaler, elasticWorker *elasticclusterv1.ElasticWorker) (int, error) {
	log := r.Log.WithValues("getClusterMetric", elasticWorkerAutoscaler.ObjectMeta.Name)

	metricSelectors := labels.SelectorFromSet(emptyMetricSelectorLabels)
	metricValue, err := r.CustomMetricsClient.NamespacedMetrics(elasticWorkerAutoscaler.Spec.Metric.Namespace).GetForObject(gvk.GroupKind(),
		elasticWorker.ObjectMeta.Name,
		elasticWorkerAutoscaler.Spec.Metric.Name,
		metricSelectors)
	if err != nil {
		log.Error(err, fmt.Sprintf("unable to fetch cluster metric %v for ElasticWorker cluster %v", elasticWorkerAutoscaler.Spec.Metric.Name, elasticWorker.ObjectMeta.Name))
		return 0, err
	}

	loadStats, parseOK := metricValue.Value.AsInt64()
	if parseOK == false {
		log.V(1).Info("getClusterMetric", "unable to parse metric value into int64 value:", metricValue.Value)
		return 0, err
	}
	return int(loadStats), nil
}

func (r *ElasticWorkerAutoscalerReconciler) getMetricSortedPodList(elasticWorkerAutoscaler *elasticclusterv1.ElasticWorkerAutoscaler, podList corev1.PodList) (*PodMetricList, error) {
	log := r.Log.WithValues("getMetricSortedPodList", elasticWorkerAutoscaler.ObjectMeta.Name)

	var podMetricList PodMetricList

	for idx, item := range podList.Items {

		if item.DeletionTimestamp != nil {
			continue
		}
		log.V(1).Info("getMetricSortedPodList", "attempting to retrieve metric for pod:", item.ObjectMeta.Name)

		metricSelectors := labels.SelectorFromSet(emptyMetricSelectorLabels)
		metricValue, err := r.CustomMetricsClient.NamespacedMetrics(elasticWorkerAutoscaler.Spec.ScaleIn.Metric.Namespace).GetForObject(gvk.GroupKind(),
			item.ObjectMeta.Name,
			elasticWorkerAutoscaler.Spec.ScaleIn.Metric.Name,
			metricSelectors)
		if err != nil {
			log.Error(err, fmt.Sprintf("unable to fetch metric %v for pod %v", elasticWorkerAutoscaler.Spec.ScaleIn.Metric.Name, item.ObjectMeta.Name))
			// will ignore error
			continue
		}

		loadStats, parseOK := metricValue.Value.AsInt64()
		if parseOK == false {
			log.V(1).Info("getMetricSortedPodList", "unable to parse metric value into int64 value:", metricValue.Value)
			// will ignore error
			continue
		}
		log.V(1).Info("getMetricSortedPodList", item.ObjectMeta.Name, loadStats)

		/* Only if the load on a pod is 0, it will be considered for termination */
		if loadStats == 0 {
			pod := PodMetric{Pod: podList.Items[idx], MetricName: elasticWorkerAutoscaler.Spec.ScaleIn.Metric.Name, MetricValue: loadStats}
			podMetricList = append(podMetricList, pod)
		}
	}
	// although the pod that have load 0 is considered but this is here if we choose to consider pod with some load
	sort.Sort(podMetricList)
	return &podMetricList, nil
}
