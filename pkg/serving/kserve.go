package serving

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	v1alpha1 "github.com/llm-d/fleet-llm-d/pkg/apis/fleet/v1alpha1"
)

// KServeRenderer translates fleet placement into the KServe-owned
// LLMInferenceService lifecycle API. It deliberately does not create the
// underlying Deployment, InferencePool, Gateway, or Router resources.
type KServeRenderer struct {
	Namespace string
}

const KServeAPIVersion = "serving.kserve.io/v1alpha2"

func (r KServeRenderer) Render(name string, spec v1alpha1.FleetInferencePoolSpec) ([]byte, error) {
	if spec.Serving.EffectiveTarget() != v1alpha1.ServingTargetKServeLLMInferenceService {
		return nil, fmt.Errorf("serving target is %q, not kserveLLMInferenceService", spec.Serving.EffectiveTarget())
	}
	modelURI := strings.TrimSpace(spec.Model.OciRef)
	if spec.Serving.KServe != nil && strings.TrimSpace(spec.Serving.KServe.ModelURI) != "" {
		modelURI = strings.TrimSpace(spec.Serving.KServe.ModelURI)
	}
	if modelURI == "" {
		modelURI = strings.TrimSpace(spec.Model.Source)
	}
	if modelURI == "" {
		return nil, fmt.Errorf("KServe model URI is required")
	}
	replicas := 1
	if spec.Serving.KServe != nil {
		if spec.Serving.KServe.Replicas > 0 {
			replicas = spec.Serving.KServe.Replicas
		}
	}
	namespace := r.Namespace
	if namespace == "" {
		namespace = "default"
	}
	metadata := map[string]interface{}{
		"name": name, "namespace": namespace,
		"labels": map[string]string{"fleet.llm-d.ai/managed-by": "fleet-controller"},
	}
	if criticality := strings.TrimSpace(kserveCriticality(spec)); criticality != "" {
		// Criticality is fleet policy metadata, not a KServe v1alpha2 model
		// field. Keep it visible without extending KServe's lifecycle schema.
		metadata["annotations"] = map[string]string{"fleet.llm-d.ai/criticality": criticality}
	}
	resource := map[string]interface{}{
		"apiVersion": KServeAPIVersion,
		"kind":       "LLMInferenceService",
		"metadata":   metadata,
		"spec": map[string]interface{}{
			"model":    map[string]interface{}{"name": spec.Model.Name, "uri": modelURI},
			"replicas": replicas,
			"router":   map[string]interface{}{"gateway": map[string]interface{}{}, "route": map[string]interface{}{}, "scheduler": map[string]interface{}{}},
		},
	}
	return json.Marshal(resource)
}

func kserveCriticality(spec v1alpha1.FleetInferencePoolSpec) string {
	if spec.Serving.KServe == nil {
		return ""
	}
	return spec.Serving.KServe.Criticality
}

// ResourcePath returns the namespaced API path for the KServe-owned object.
func (r KServeRenderer) ResourcePath(name string) string {
	namespace := r.Namespace
	if namespace == "" {
		namespace = "default"
	}
	return "/apis/" + KServeAPIVersion + "/namespaces/" + url.PathEscape(namespace) +
		"/llminferenceservices/" + url.PathEscape(name)
}

type KServeResource struct {
	Metadata struct {
		Generation int64 `json:"generation,omitempty"`
	} `json:"metadata,omitempty"`
	Status KServeStatus `json:"status,omitempty"`
}

type KServeStatus struct {
	URL       string `json:"url,omitempty"`
	Addresses []struct {
		URL string `json:"url,omitempty"`
	} `json:"addresses,omitempty"`
	ObservedGeneration int64             `json:"observedGeneration,omitempty"`
	Conditions         []KServeCondition `json:"conditions,omitempty"`
}

type KServeCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
}

// Ready reports KServe's lifecycle status without attempting to reproduce its
// rollout or readiness logic.
func (s KServeStatus) Ready() bool {
	for _, condition := range s.Conditions {
		if condition.Type == "Ready" {
			return strings.EqualFold(condition.Status, "true")
		}
	}
	return false
}

// Endpoint returns the primary KServe-reported address without deriving one
// from fleet configuration.
func (s KServeStatus) Endpoint() string {
	if strings.TrimSpace(s.URL) != "" {
		return strings.TrimSpace(s.URL)
	}
	for _, address := range s.Addresses {
		if strings.TrimSpace(address.URL) != "" {
			return strings.TrimSpace(address.URL)
		}
	}
	return ""
}

// ObservedReady requires current-generation KServe readiness and a published
// endpoint. Fleet does not reproduce workload or Router readiness logic.
func (r KServeResource) ObservedReady() bool {
	if r.Metadata.Generation <= 0 || r.Status.ObservedGeneration != r.Metadata.Generation || r.Status.Endpoint() == "" {
		return false
	}
	return r.Status.Ready()
}

func ParseKServeResource(raw []byte) (KServeResource, error) {
	var resource KServeResource
	if err := json.Unmarshal(raw, &resource); err != nil {
		return KServeResource{}, fmt.Errorf("parse KServe LLMInferenceService status: %w", err)
	}
	return resource, nil
}
