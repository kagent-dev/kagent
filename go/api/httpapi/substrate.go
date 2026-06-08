package httpapi

// SubstrateStatusResponse aggregates Agent Substrate control-plane and Kubernetes state.
type SubstrateStatusResponse struct {
	// Enabled is true when the controller is configured with an ate-api endpoint.
	Enabled bool `json:"enabled"`
	// AteAPIError is set when ate-api list calls fail (actors/workers may be partial or empty).
	AteAPIError string `json:"ateApiError,omitempty"`

	WorkerPools    []SubstrateWorkerPoolEntry    `json:"workerPools"`
	ActorTemplates []SubstrateActorTemplateEntry `json:"actorTemplates"`
	Actors         []SubstrateActorEntry         `json:"actors"`
	Workers        []SubstrateWorkerEntry        `json:"workers"`
}

// SubstrateWorkerPoolEntry is a ate.dev WorkerPool CR.
type SubstrateWorkerPoolEntry struct {
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Replicas   int32  `json:"replicas"`
	AteomImage string `json:"ateomImage"`
}

// SubstrateActorTemplateEntry is a ate.dev ActorTemplate CR.
type SubstrateActorTemplateEntry struct {
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	Phase           string `json:"phase,omitempty"`
	GoldenActorID   string `json:"goldenActorId,omitempty"`
	GoldenSnapshot  string `json:"goldenSnapshot,omitempty"`
	WorkerPoolRef   string `json:"workerPoolRef,omitempty"`
	HarnessName     string `json:"harnessName,omitempty"`
	ManagedByKagent bool   `json:"managedByKagent"`
}

// SubstrateActorEntry is runtime state from ate-api (redis).
type SubstrateActorEntry struct {
	ActorID                string `json:"actorId"`
	Status                 string `json:"status"`
	ActorTemplateNamespace string `json:"actorTemplateNamespace,omitempty"`
	ActorTemplateName      string `json:"actorTemplateName,omitempty"`
	AteomPodNamespace      string `json:"ateomPodNamespace,omitempty"`
	AteomPodName           string `json:"ateomPodName,omitempty"`
	AteomPodIP             string `json:"ateomPodIp,omitempty"`
	LastSnapshot           string `json:"lastSnapshot,omitempty"`
	InProgressSnapshot     string `json:"inProgressSnapshot,omitempty"`
	Version                int64  `json:"version,omitempty"`
}

// SubstrateWorkerEntry is a worker assignment from ate-api (redis).
type SubstrateWorkerEntry struct {
	WorkerNamespace string `json:"workerNamespace"`
	WorkerPool      string `json:"workerPool"`
	WorkerPod       string `json:"workerPod"`
	ActorNamespace  string `json:"actorNamespace,omitempty"`
	ActorTemplate   string `json:"actorTemplate,omitempty"`
	ActorID         string `json:"actorId,omitempty"`
	IP              string `json:"ip,omitempty"`
	Version         int64  `json:"version,omitempty"`
}
