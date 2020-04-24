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

package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	custommetrics "k8s.io/metrics/pkg/client/custom_metrics"

	_ "k8s.io/apimachinery/pkg/labels"
	_ "k8s.io/apimachinery/pkg/runtime/schema"

	elasticclusterv1 "github.com/sarweshsuman/elastic-worker-autoscaler/api/v1"
	"github.com/sarweshsuman/elastic-worker-autoscaler/controllers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = elasticclusterv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "0c9a97ae.sarweshsuman.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	setupLog.Info("initializing custom client")
	customMetricClient, err := initializeCustomMetrics(mgr)
	if err != nil {
		setupLog.Error(err, "unable to setup custom metric client")
		os.Exit(1)
	}

	/*
		gv, err := apiVersionsGetter.PreferredVersion()
		if err != nil {
			panic(err)
		}

		fmt.Printf("Preferred Version %v", gv)
	*/

	if err = (&controllers.ElasticWorkerAutoscalerReconciler{
		CustomMetricsClient: *customMetricClient,
		Client:              mgr.GetClient(),
		Log:                 ctrl.Log.WithName("controllers").WithName("ElasticWorkerAutoscaler"),
		Scheme:              mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ElasticWorkerAutoscaler")
		os.Exit(1)
	}
	if err = (&controllers.ElasticWorkerReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("ElasticWorker"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ElasticWorker")
		os.Exit(1)
	}

	/*
		podNamePrefix := "elasticclustermetric_default_pod1_load"
		podLabels := map[string]string{"name": podNamePrefix}
		selectors := labels.SelectorFromSet(podLabels)

		gvk := schema.FromAPIVersionAndKind("v1", "Pod")
		mvl, err := customMetricClient.NamespacedMetrics("default").GetForObject(gvk.GroupKind(), "custom-metrics-6bf9d8969b-lzkvs", "packets-per-second-1", selectors)
		if err != nil {
			panic(err)
		}
		fmt.Printf("metric = %+v", mvl)
	*/

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func initializeCustomMetrics(mgr ctrl.Manager) (*custommetrics.CustomMetricsClient, error) {
	restConfig := *ctrl.GetConfigOrDie()
	customAutoScalerConfig := restclient.AddUserAgent(&restConfig, "elastic-worker-autoscaler")
	customAutoScalerClient, err := clientset.NewForConfig(customAutoScalerConfig)
	if err != nil {
		return nil, err
	}
	apiVersionsGetter := custommetrics.NewAvailableAPIsGetter(customAutoScalerClient.Discovery())
	customMetricClient := custommetrics.NewForConfig(customAutoScalerConfig, mgr.GetRESTMapper(), apiVersionsGetter)
	return &customMetricClient, nil
}
