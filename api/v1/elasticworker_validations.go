package v1

func ValidateElasticWorkerObject(elasticWorker *ElasticWorker) bool {
	if elasticWorker.Spec.MinReplicas < 0 ||
		elasticWorker.Spec.MinReplicas > elasticWorker.Spec.MaxReplicas {
		return false
	} else {
		return true
	}
}
