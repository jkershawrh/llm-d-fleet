package serving

import (
	"bytes"
	"encoding/json"
	"testing"

	v1alpha1 "github.com/llm-d/fleet-llm-d/pkg/apis/fleet/v1alpha1"
)

func TestKServeRendererDelegatesLifecycle(t *testing.T) {
	spec := v1alpha1.FleetInferencePoolSpec{
		Model:   v1alpha1.ModelSpec{Name: "granite", OciRef: "oci://models/granite:1"},
		Serving: v1alpha1.ServingSpec{Target: v1alpha1.ServingTargetKServeLLMInferenceService, KServe: &v1alpha1.KServeServingSpec{Replicas: 2}},
	}
	raw, err := (KServeRenderer{Namespace: "models"}).Render("granite", spec)
	if err != nil {
		t.Fatal(err)
	}
	var resource map[string]interface{}
	if err := json.Unmarshal(raw, &resource); err != nil {
		t.Fatal(err)
	}
	if resource["kind"] != "LLMInferenceService" {
		t.Fatalf("kind = %v", resource["kind"])
	}
	if resource["apiVersion"] != KServeAPIVersion {
		t.Fatalf("apiVersion = %v", resource["apiVersion"])
	}
	specMap := resource["spec"].(map[string]interface{})
	if specMap["replicas"] != float64(2) {
		t.Fatalf("replicas = %v", specMap["replicas"])
	}
	if _, ok := specMap["template"]; ok {
		t.Fatal("fleet renderer must not create KServe workload templates")
	}
}

func TestKServeRendererIsDeterministicAndBuildsResourcePath(t *testing.T) {
	spec := v1alpha1.FleetInferencePoolSpec{
		Model:   v1alpha1.ModelSpec{Name: "granite", OciRef: "oci://models/granite:1"},
		Serving: v1alpha1.ServingSpec{Target: v1alpha1.ServingTargetKServeLLMInferenceService},
	}
	renderer := KServeRenderer{Namespace: "model space"}
	first, err := renderer.Render("granite", spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderer.Render("granite", spec)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("render output is not deterministic")
	}
	if got := renderer.ResourcePath("granite"); got != "/apis/serving.kserve.io/v1alpha2/namespaces/model%20space/llminferenceservices/granite" {
		t.Fatalf("resource path = %q", got)
	}
}

func TestInferencePoolRemainsDefault(t *testing.T) {
	serving := v1alpha1.ServingSpec{}
	if serving.EffectiveTarget() != v1alpha1.ServingTargetInferencePool {
		t.Fatalf("default = %q", serving.EffectiveTarget())
	}
	_, err := (KServeRenderer{}).Render("model", v1alpha1.FleetInferencePoolSpec{Serving: serving})
	if err == nil {
		t.Fatal("expected non-KServe target rejection")
	}
}

func TestKServeStatusReady(t *testing.T) {
	status := KServeStatus{}
	status.Conditions = append(status.Conditions, KServeCondition{Type: "Ready", Status: "True"})
	if !status.Ready() {
		t.Fatal("Ready condition was not honored")
	}
}

func TestKServeResourceRequiresCurrentReadyStatusAndEndpoint(t *testing.T) {
	raw := []byte(`{
		"metadata":{"generation":3},
		"status":{"url":"https://granite.example","observedGeneration":3,
		"conditions":[{"type":"Ready","status":"True","reason":"Ready"}]}}
	`)
	resource, err := ParseKServeResource(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !resource.ObservedReady() || resource.Status.Endpoint() != "https://granite.example" {
		t.Fatalf("resource not observed ready: %#v", resource)
	}
	resource.Status.ObservedGeneration = 2
	if resource.ObservedReady() {
		t.Fatal("stale status was accepted")
	}
	resource.Status.ObservedGeneration = 3
	resource.Status.URL = ""
	if resource.ObservedReady() {
		t.Fatal("ready status without endpoint was accepted")
	}
}

func TestKServeStatusFallsBackToAddress(t *testing.T) {
	raw := []byte(`{"metadata":{"generation":1},"status":{"observedGeneration":1,"addresses":[{"url":"https://internal.example"}],"conditions":[{"type":"Ready","status":"True"}]}}`)
	resource, err := ParseKServeResource(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !resource.ObservedReady() || resource.Status.Endpoint() != "https://internal.example" {
		t.Fatalf("address fallback failed: %#v", resource)
	}
}
