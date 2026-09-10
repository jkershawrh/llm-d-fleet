package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/llm-d/fleet-llm-d/pkg/auth"
	"github.com/llm-d/fleet-llm-d/pkg/cluster/client"
	fleetgrpc "github.com/llm-d/fleet-llm-d/pkg/grpc"
)

// Run starts the fleet controller HTTP servers and blocks until the context
// is cancelled or a shutdown signal is received. When grpcPort is non-zero,
// a JSON-RPC listener is started alongside the REST API, exposing the
// FleetService defined in api/proto/fleet/v1/fleet.proto.
func (fc *FleetController) Run(ctx context.Context, port, metricsPort, grpcPort int, authCfg auth.Config, tlsCert, tlsKey, mode string, rateLimiter *auth.RateLimiter, rateLimitExempt []string) error {
	// Create a context that is cancelled on SIGINT or SIGTERM.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Wrap the API server mux with rate limiting, authentication,
	// authorization, and request logging. The logging middleware is outermost
	// so it captures the final status code after all other middleware runs.
	//
	// Rate limiting is two-tier, and the order matters.
	//
	// The outer tier is keyed on client address and runs before
	// authentication, because it is the only thing protecting the token
	// verification path from an unauthenticated flood. The inner tier is
	// keyed on the verified subject and runs after authentication, so one
	// principal cannot exhaust the budget of another by changing source
	// address. Neither tier keys on anything the caller can choose.
	mux := fc.SetupRoutes(mode)
	exempt := defaultExemptPaths(rateLimitExempt)
	handler := http.Handler(fc.leaderGate(mux))
	handler = auth.AuthorizationMiddleware(exempt, handler)
	if rateLimiter != nil && authCfg.Enabled {
		subjectLimiter := rateLimiter.Derive()
		defer subjectLimiter.Stop()
		handler = auth.SubjectRateLimitMiddleware(subjectLimiter, exempt, handler)
	}
	handler = auth.AuthMiddleware(authCfg, exempt, handler)
	if rateLimiter != nil {
		handler = auth.RateLimitMiddlewareWithExemptions(rateLimiter, exempt, handler)
	}
	handler = RequestLoggingMiddleware(handler)

	apiServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 180 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	metricsServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", metricsPort),
		Handler:      setupMetricsServer(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	if fc.LeaderElector != nil {
		go func() {
			if err := fc.LeaderElector.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("leader election stopped", "error", err)
			}
		}()
	}

	// Start gRPC (JSON-RPC) server when grpcPort is configured.
	if grpcPort > 0 {
		if authCfg.Provider != auth.ProviderHMAC || authCfg.Secret == "" {
			return fmt.Errorf("JSON-RPC requires the hmac identity provider and FLEET_AUTH_SECRET")
		}
		if tlsCert == "" || tlsKey == "" {
			return fmt.Errorf("JSON-RPC requires --tls-cert and --tls-key")
		}
		rpcSvc := fleetgrpc.NewFleetService(
			func() (interface{}, error) {
				return fc.ClusterClient.ListClusters(context.Background())
			},
			func() (interface{}, error) {
				if fc.Reconciler != nil {
					if pools := fc.Reconciler.ListPools(); len(pools) > 0 {
						return pools, nil
					}
				}
				return fc.PoolRepo.List(context.Background())
			},
		)
		rpcSvc.SetAuthSecret(authCfg.Secret)
		rpcSvc.SetRegisterCluster(func(req fleetgrpc.RegisterClusterRequest) (*fleetgrpc.RegisterClusterResponse, error) {
			if fc.LeaderElector != nil && !fc.LeaderElector.IsLeader() {
				return nil, fmt.Errorf("standby: mutating requests are handled by the elected leader")
			}
			reg := client.ClusterRegistration{
				ID:     req.ID,
				Name:   req.Name,
				Region: req.Region,
				Labels: req.Labels,
			}
			reg, err := client.NormalizeClusterRegistration(reg)
			if err != nil {
				return nil, err
			}
			if err := fc.registerCluster(context.Background(), reg); err != nil {
				return nil, err
			}
			return &fleetgrpc.RegisterClusterResponse{ID: reg.ID, Status: "registered"}, nil
		})

		grpcListener, err := fleetgrpc.ServeTLS(fmt.Sprintf(":%d", grpcPort), rpcSvc, tlsCert, tlsKey)
		if err != nil {
			return fmt.Errorf("grpc server: %w", err)
		}
		defer grpcListener.Close()
		slog.Info("grpc server listening", "port", grpcPort)
	}

	// Start metrics server.
	go func() {
		slog.Info("metrics server listening", "port", metricsPort)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "error", err)
		}
	}()

	// Start API server (with TLS if cert and key are provided).
	go func() {
		if tlsCert != "" && tlsKey != "" {
			slog.Info("api server listening", "port", port, "tls", true)
			if err := apiServer.ListenAndServeTLS(tlsCert, tlsKey); err != nil && err != http.ErrServerClosed {
				slog.Error("api server error", "error", err)
			}
		} else {
			slog.Info("api server listening", "port", port)
			if err := apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("api server error", "error", err)
			}
		}
	}()

	// Start CRD and ModelPlane watchers only while this instance owns the
	// Kubernetes Lease. Without leader election they retain legacy behavior.
	if mode == "publisher" {
		go fc.runGridSyncLoop(ctx)
	} else if mode != "inference" {
		fc.runControlPlaneWatchers(ctx)
	}

	// Mark as ready.
	fc.ready.Store(true)
	slog.Info("fleet-controller is ready")

	// Wait for shutdown signal.
	<-ctx.Done()
	slog.Info("fleet-controller shutting down...")

	// Graceful shutdown with a 15-second deadline.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var shutdownErr error
	if err := apiServer.Shutdown(shutdownCtx); err != nil {
		shutdownErr = fmt.Errorf("api server shutdown: %w", err)
	}
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		if shutdownErr != nil {
			shutdownErr = fmt.Errorf("%v; metrics server shutdown: %w", shutdownErr, err)
		} else {
			shutdownErr = fmt.Errorf("metrics server shutdown: %w", err)
		}
	}

	slog.Info("fleet-controller stopped")
	return shutdownErr
}

func (fc *FleetController) leaderGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fc.LeaderElector != nil &&
			r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions &&
			!fc.LeaderElector.IsLeader() {
			w.Header().Set("Retry-After", "3")
			writeError(w, http.StatusServiceUnavailable, "standby: mutating requests are handled by the elected leader")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (fc *FleetController) runControlPlaneWatchers(ctx context.Context) {
	workers := make([]func(context.Context), 0, 3)
	if fc.CRDWatcher != nil {
		workers = append(workers, fc.CRDWatcher.Run)
	}
	if fc.ModelPlaneWatcher != nil {
		workers = append(workers, fc.ModelPlaneWatcher.Run)
	}
	if fc.Actuator != nil {
		workers = append(workers, fc.runAutoscalingLoop)
	}
	if fc.GridSignalPoller != nil {
		workers = append(workers, fc.runGridSignalLoop)
	}
	if fc.effectiveRoutingProvider() != nil {
		workers = append(workers, fc.runGridSyncLoop)
	}

	if fc.LeaderElector == nil {
		startWatcherGroup(ctx, workers)
		return
	}

	go runLeaderScopedWorkers(ctx, fc.LeaderElector.IsLeader, 250*time.Millisecond, 10*time.Second, workers)
}

func startWatcherGroup(ctx context.Context, workers []func(context.Context)) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Add(len(workers))
		for _, worker := range workers {
			worker := worker
			go func() {
				defer wg.Done()
				worker(ctx)
			}()
		}
		wg.Wait()
	}()
	return done
}

func runLeaderScopedWorkers(ctx context.Context, isLeader func() bool, interval, stopTimeout time.Duration, workers []func(context.Context)) {
	if len(workers) == 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var cancel context.CancelFunc
	var done <-chan struct{}
	var stopTimer *time.Timer
	var stopDeadline <-chan time.Time
	clearStopTimer := func() {
		if stopTimer != nil && !stopTimer.Stop() {
			select {
			case <-stopTimer.C:
			default:
			}
		}
		stopTimer = nil
		stopDeadline = nil
	}
	requestStop := func() {
		if cancel == nil {
			return
		}
		cancel()
		cancel = nil
		clearStopTimer()
		stopTimer = time.NewTimer(stopTimeout)
		stopDeadline = stopTimer.C
	}
	defer clearStopTimer()

	for {
		leader := isLeader()
		if leader && done == nil {
			leaderCtx, leaderCancel := context.WithCancel(ctx)
			cancel = leaderCancel
			done = startWatcherGroup(leaderCtx, workers)
			slog.Info("leader acquired: started control-plane watchers")
		} else if !leader && cancel != nil {
			slog.Info("leadership lost: stopping control-plane watchers")
			requestStop()
		}

		select {
		case <-ctx.Done():
			if cancel != nil {
				cancel()
			}
			clearStopTimer()
			if done != nil {
				timer := time.NewTimer(stopTimeout)
				select {
				case <-done:
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
				case <-timer.C:
					slog.Warn("watcher shutdown still incomplete", "timeout", stopTimeout)
				}
			}
			return
		case <-done:
			wasStopping := cancel == nil
			clearStopTimer()
			cancel = nil
			done = nil
			if wasStopping {
				slog.Info("leadership lost: control-plane watchers stopped")
			} else {
				slog.Info("WARNING: control-plane watchers exited while leadership is active")
			}
		case <-stopDeadline:
			stopTimer = nil
			stopDeadline = nil
			slog.Warn("watcher stop still incomplete, restart remains blocked", "timeout", stopTimeout)
		case <-ticker.C:
		}
	}
}
