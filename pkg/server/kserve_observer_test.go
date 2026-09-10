package server

import (
	"context"
	"errors"
	"testing"

	"github.com/llm-d/fleet-llm-d/pkg/serving"
)

type fakeResourceReader struct {
	resources map[string][]byte
	err       error
}

func (f fakeResourceReader) GetResource(_ context.Context, clusterID, _ string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.resources[clusterID], nil
}

func TestObserveKServeClustersReturnsOnlyCurrentReadyCapacity(t *testing.T) {
	ready := []byte(`{"metadata":{"generation":2},"status":{"url":"https://ready.example","observedGeneration":2,"conditions":[{"type":"Ready","status":"True"}]}}`)
	stale := []byte(`{"metadata":{"generation":2},"status":{"url":"https://stale.example","observedGeneration":1,"conditions":[{"type":"Ready","status":"True"}]}}`)
	actual, err := observeKServeClusters(context.Background(), fakeResourceReader{resources: map[string][]byte{
		"cluster-ready": ready, "cluster-stale": stale,
	}}, serving.KServeRenderer{Namespace: "models"}, "granite", []string{"cluster-ready", "cluster-stale"})
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != 1 || actual[0] != "cluster-ready" {
		t.Fatalf("actual = %v", actual)
	}
}

func TestObserveKServeClustersFailsClosedWhenAPIMissing(t *testing.T) {
	actual, err := observeKServeClusters(context.Background(), fakeResourceReader{err: errors.New("API server returned 404")}, serving.KServeRenderer{}, "granite", []string{"cluster-a"})
	if err == nil || len(actual) != 0 {
		t.Fatalf("actual=%v err=%v", actual, err)
	}
}
