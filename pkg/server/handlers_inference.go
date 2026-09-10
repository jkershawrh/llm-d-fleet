package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/llm-d/fleet-llm-d/pkg/auth"
	"github.com/llm-d/fleet-llm-d/pkg/tenant/quota"
)

const (
	defaultCPUModel = "granite-2b-cpu"
	defaultGPUModel = "ibm-granite/granite-3.1-8b-instruct"
	defaultGPUAlias = "granite-8b-gpu"
)

type inferenceEnvelope struct {
	Model     string          `json:"model"`
	Prompt    json.RawMessage `json:"prompt,omitempty"`
	Messages  json.RawMessage `json:"messages,omitempty"`
	TenantID  string          `json:"tenant_id,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Policy    string          `json:"policy,omitempty"`
	MaxTokens int64           `json:"max_tokens,omitempty"`
}

func (fc *FleetController) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	fc.handleInference(w, r, true)
}

func (fc *FleetController) handleCompletions(w http.ResponseWriter, r *http.Request) {
	fc.handleInference(w, r, false)
}

func (fc *FleetController) handleInference(w http.ResponseWriter, r *http.Request, chat bool) {
	start := time.Now()
	requestsTotal.Inc()
	defer ObserveRequest(start)
	if fc.InferenceSlots != nil {
		select {
		case fc.InferenceSlots <- struct{}{}:
			defer func() { <-fc.InferenceSlots }()
		case <-r.Context().Done():
			return
		default:
			inferenceErrors.Inc("concurrency_limit")
			w.Header().Set("Retry-After", "1")
			writeInferenceError(w, http.StatusTooManyRequests, "concurrency_limit", "inference gateway is at capacity", requestID(r))
			return
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var envelope inferenceEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return
	}
	text, err := inferenceText(envelope, chat)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	requestID := requestID(r)
	modelClass := fc.requestedModelClass(envelope.Model)
	tenantID := envelope.TenantID
	if identity := auth.GetIdentity(r); identity != nil {
		identityTenant := identity.Tenant
		if identityTenant == "" {
			identityTenant = identity.Subject
		}
		if identity.HasRole(auth.RoleTenant) || tenantID == "" {
			tenantID = identityTenant
		}
	} else if claims := auth.GetClaims(r); claims != nil {
		// Compatibility for tests and internal callers that directly install
		// legacy claims without traversing AuthMiddleware.
		if claims.Role == auth.RoleTenant || tenantID == "" {
			tenantID = claims.Subject
		}
	}
	requestedTokens := envelope.MaxTokens
	if requestedTokens <= 0 {
		requestedTokens = 256
	}
	decision, err := fc.classifyAndRoute(r.Context(), routeRequest{
		Text: text, RequestID: requestID, Model: envelope.Model,
		TenantID: tenantID, SessionID: envelope.SessionID, Policy: envelope.Policy,
	})
	if err != nil {
		inferenceErrors.Inc("no_eligible_provider")
		writeInferenceError(w, http.StatusServiceUnavailable, "no_compatible_capacity", err.Error(), requestID)
		return
	}
	if modelClass == "gpu" {
		gpuProvider := fc.nextHealthyProvider(r.Context(), fc.gpuPhysicalModel())
		if gpuProvider == "" {
			inferenceErrors.Inc("no_eligible_provider")
			writeInferenceError(w, http.StatusServiceUnavailable, "no_compatible_capacity", "the requested GPU model has no compatible healthy provider", requestID)
			return
		}
		decision.TargetCluster = gpuProvider
		decision.Reason = "explicit-model"
		if envelope.SessionID != "" && fc.Routing != nil && fc.Routing.SessionTable != nil {
			fc.Routing.SessionTable.Unbind(envelope.SessionID)
			fc.Routing.SessionTable.Bind(envelope.SessionID, gpuProvider)
		}
	}
	physicalModel := fc.CPUPhysicalModel
	if physicalModel == "" {
		physicalModel = defaultCPUModel
	}
	if modelClass == "gpu" || (modelClass == "" && (decision.SemanticLabel == "COMPLEX" || decision.SemanticLabel == "REASONING" || fc.providerServesModel(decision.TargetCluster, fc.gpuPhysicalModel()))) {
		physicalModel = fc.GPUPhysicalModel
		if physicalModel == "" {
			physicalModel = defaultGPUModel
		}
	}
	if physicalModel != fc.cpuPhysicalModel() {
		gpuProvider := fc.nextHealthyProvider(r.Context(), physicalModel)
		if gpuProvider == "" {
			inferenceErrors.Inc("no_eligible_provider")
			writeInferenceError(w, http.StatusServiceUnavailable, "no_compatible_capacity", "the selected GPU model has no compatible healthy provider", requestID)
			return
		}
		decision.TargetCluster = gpuProvider
		if modelClass == "" {
			decision.Reason = "semantic-escalation"
		}
		if envelope.SessionID != "" && fc.Routing != nil && fc.Routing.SessionTable != nil {
			fc.Routing.SessionTable.Unbind(envelope.SessionID)
			fc.Routing.SessionTable.Bind(envelope.SessionID, gpuProvider)
		}
	}
	if physicalModel == fc.cpuPhysicalModel() && decision.Reason != "session-affinity" {
		if selected := fc.nextHealthyProvider(r.Context(), physicalModel); selected != "" {
			decision.TargetCluster = selected
			decision.Reason = "compatible-round-robin"
			if envelope.SessionID != "" && fc.Routing != nil && fc.Routing.SessionTable != nil {
				fc.Routing.SessionTable.Unbind(envelope.SessionID)
				fc.Routing.SessionTable.Bind(envelope.SessionID, selected)
			}
		} else {
			inferenceErrors.Inc("no_eligible_provider")
			writeInferenceError(w, http.StatusServiceUnavailable, "no_compatible_capacity", "the requested CPU model has no compatible healthy provider", requestID)
			return
		}
	}
	if tenantID != "" && fc.QuotaEnforcer != nil {
		check := quota.QuotaCheckRequest{TokensRequested: requestedTokens, Model: physicalModel, ClusterID: decision.TargetCluster}
		var result quota.QuotaCheckResult
		if consumer, ok := fc.QuotaEnforcer.(*quota.DefaultQuotaEnforcer); ok {
			result, err = consumer.ReserveQuota(r.Context(), tenantID, check)
			if err == nil && result.Allowed {
				defer func() {
					releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if releaseErr := consumer.ReleaseQuota(releaseCtx, tenantID); releaseErr != nil {
						slog.Error("release tenant quota reservation", "tenant_id", tenantID, "error", releaseErr)
					}
				}()
			}
		} else {
			result, err = fc.QuotaEnforcer.CheckQuota(r.Context(), tenantID, check)
		}
		if err != nil {
			inferenceErrors.Inc("quota_unavailable")
			writeInferenceError(w, http.StatusServiceUnavailable, "quota_unavailable", "tenant quota service is unavailable", requestID)
			return
		}
		if !result.Allowed {
			inferenceErrors.Inc("quota_denied")
			w.Header().Set("Retry-After", "60")
			writeInferenceError(w, http.StatusTooManyRequests, "quota_exceeded", result.Reason, requestID)
			return
		}
	}
	var forwarded map[string]json.RawMessage
	_ = json.Unmarshal(body, &forwarded)
	modelJSON, _ := json.Marshal(physicalModel)
	forwarded["model"] = modelJSON
	delete(forwarded, "tenant_id")
	delete(forwarded, "session_id")
	delete(forwarded, "policy")
	body, _ = json.Marshal(forwarded)
	target, err := fc.inferenceTarget(physicalModel)
	if err != nil {
		writeInferenceError(w, http.StatusServiceUnavailable, "gateway_not_configured", err.Error(), requestID)
		return
	}
	// #nosec G704 -- BaseURL comes only from the operator-selected inference
	// provider configuration; the fixed OpenAI path cannot select a host.
	upstream, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target.BaseURL+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		writeInferenceError(w, http.StatusInternalServerError, "proxy_error", "could not create upstream request", requestID)
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Accept", r.Header.Get("Accept"))
	upstream.Header.Set("X-Request-ID", requestID)
	upstream.Header.Set("X-Fleet-Target-Cluster", decision.TargetCluster)
	upstream.Header.Set("X-Fleet-Routing-Reason", decision.Reason)
	if target.APIToken != "" {
		upstream.Header.Set("Authorization", "Bearer "+target.APIToken)
	}
	client := fc.InferenceClient
	if client == nil {
		client = &http.Client{Timeout: 180 * time.Second}
	}
	inferenceActive.Inc()
	defer inferenceActive.Dec()
	// #nosec G704 -- upstream was built from the trusted provider configuration
	// above and carries no caller-controlled authority or destination.
	resp, err := client.Do(upstream)
	if r.Context().Err() == nil && physicalModel == fc.cpuPhysicalModel() && retryableInferenceFailure(resp, err) {
		if retryCluster := fc.nextHealthyProviderExcluding(r.Context(), physicalModel, decision.TargetCluster); retryCluster != "" {
			if resp != nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			retryBody, bodyErr := upstream.GetBody()
			if bodyErr == nil {
				retryRequest := upstream.Clone(r.Context())
				retryRequest.Body = retryBody
				retryRequest.Header.Set("X-Fleet-Target-Cluster", retryCluster)
				inferenceRetries.Inc("provider_failure")
				slog.Warn("retrying inference on alternate provider", "request_id", requestID, "failed_cluster", decision.TargetCluster, "retry_cluster", retryCluster)
				decision.TargetCluster = retryCluster
				decision.Reason = "health-failover"
				// #nosec G704 -- retryRequest is a clone of the same trusted upstream;
				// only the fleet-qualified cluster header changes.
				resp, err = client.Do(retryRequest)
			}
		}
	}
	if err != nil {
		inferenceErrors.Inc("upstream_unavailable")
		slog.Warn("inference upstream failed", "request_id", requestID, "cluster", decision.TargetCluster, "error", err)
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			writeInferenceError(w, http.StatusGatewayTimeout, "upstream_timeout", "inference upstream timed out", requestID)
			return
		}
		writeInferenceError(w, http.StatusBadGateway, "upstream_unavailable", "inference upstream failed", requestID)
		return
	}
	defer resp.Body.Close()
	// Router execution evidence is authoritative only when a response represents
	// a successful backend execution. Proxy/EPP 5xx responses may be generated
	// before any provider is selected and therefore legitimately carry no
	// X-Fleet-Router-Upstream header. Preserve those errors below instead of
	// misclassifying them as a routing-integrity failure. Successful responses
	// remain fail-closed when their execution identity is absent or unrecognized.
	if target.Provider == InferenceProviderLLMD && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		actualCluster, evidenceErr := fc.routerClusterForUpstream(resp.Header.Get("X-Fleet-Router-Upstream"), physicalModel)
		if evidenceErr != nil {
			inferenceErrors.Inc("routing_evidence_mismatch")
			_, _ = io.Copy(io.Discard, resp.Body)
			slog.Error("Router execution evidence rejected", "request_id", requestID, "error", evidenceErr)
			writeInferenceError(w, http.StatusBadGateway, "routing_evidence_mismatch", "Router execution identity did not match the fleet-qualified provider set", requestID)
			return
		}
		decision.TargetCluster = actualCluster
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		inferenceErrors.Inc("no_eligible_provider")
		_, _ = io.Copy(io.Discard, resp.Body)
		writeInferenceError(w, http.StatusServiceUnavailable, "no_compatible_capacity", "the selected provider is unavailable", requestID)
		return
	}
	copyResponseHeaders(w.Header(), resp.Header)
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("X-Fleet-Routed-To", decision.TargetCluster)
	w.Header().Set("X-Fleet-Routing-Reason", decision.Reason)
	w.Header().Set("X-Fleet-Actual-Model", physicalModel)
	w.Header().Set("X-Fleet-Data-Plane", string(target.Provider))
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		inferenceErrors.Inc("stream_interrupted")
	}
	inferenceRequests.Inc(decision.TargetCluster + ":" + physicalModel)
}

func retryableInferenceFailure(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	return resp != nil && (resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout)
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func (fc *FleetController) requestedModelClass(requested string) string {
	if requested == "" {
		return ""
	}
	if requested == fc.cpuPhysicalModel() || requested == defaultCPUModel {
		return "cpu"
	}
	gpuModel := fc.gpuPhysicalModel()
	if requested == gpuModel || requested == defaultGPUModel || requested == defaultGPUAlias {
		return "gpu"
	}
	return ""
}

func (fc *FleetController) cpuPhysicalModel() string {
	if fc.CPUPhysicalModel != "" {
		return fc.CPUPhysicalModel
	}
	return defaultCPUModel
}

func (fc *FleetController) gpuPhysicalModel() string {
	if fc.GPUPhysicalModel != "" {
		return fc.GPUPhysicalModel
	}
	return defaultGPUModel
}

func inferenceText(req inferenceEnvelope, chat bool) (string, error) {
	if chat {
		var messages []struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(req.Messages, &messages); err != nil || len(messages) == 0 {
			return "", fmt.Errorf("messages must be a non-empty array")
		}
		for i := len(messages) - 1; i >= 0; i-- {
			var text string
			if json.Unmarshal(messages[i].Content, &text) == nil && strings.TrimSpace(text) != "" {
				return text, nil
			}
		}
		return "", fmt.Errorf("messages must contain text content")
	}
	var prompt string
	if err := json.Unmarshal(req.Prompt, &prompt); err != nil || strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt must be a non-empty string")
	}
	return prompt, nil
}

func requestID(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); id != "" && len(id) <= 128 {
		return id
	}
	return fmt.Sprintf("fleet-%d", time.Now().UnixNano())
}

func copyResponseHeaders(dst, src http.Header) {
	for _, key := range []string{"Content-Type", "Cache-Control", "Retry-After"} {
		if value := src.Get(key); value != "" {
			dst.Set(key, value)
		}
	}
}

func writeInferenceError(w http.ResponseWriter, status int, code, message, requestID string) {
	w.Header().Set("X-Request-ID", requestID)
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": requestID}})
}
