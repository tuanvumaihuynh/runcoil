package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/pflag"

	"github.com/runcoil/runcoil/internal/api/gen"
	"github.com/runcoil/runcoil/internal/api/middleware"
	"github.com/runcoil/runcoil/internal/api/swagger"
	"github.com/runcoil/runcoil/internal/services"
	"github.com/runcoil/runcoil/openapi"
	"github.com/runcoil/runcoil/pkg/healthutil"
	"github.com/runcoil/runcoil/ui"
)

const (
	readyTimeout    = 2 * time.Second
	shutdownTimeout = 10 * time.Second

	sessionCookieName = "runcoil_session"
	maxBodyBytes      = 1 << 20
)

// Config is the configuration of the API server.
type Config struct {
	Port         int
	SecureCookie bool
	Swagger      bool
}

// RegisterFlags declares every key with its default and help text.
func (c *Config) RegisterFlags(fs *pflag.FlagSet) {
	fs.IntVar(&c.Port, "api.port", 8080, "Port of the API and the health checks.")
	fs.BoolVar(&c.SecureCookie, "api.secure-cookie", false, "Send the session cookie over HTTPS only. Turn it on when the dashboard is served over HTTPS.")
	fs.BoolVar(&c.Swagger, "api.swagger", false, "Serve the API docs with Swagger UI at /docs.")
}

// Validate checks the configuration.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}

	return nil
}

// Service serves the dashboard API and the health checks of the process.
type Service struct {
	cfg            Config
	logger         *slog.Logger
	checks         *healthutil.Checks
	accountService services.AccountService
	userService    services.UserService
	projectService services.ProjectService
	apiKeyService  services.APIKeyService
	workerService  services.WorkerService
	runService     services.RunService

	spec       *openapi3.T
	operations map[string]middleware.Operation

	stopped  chan struct{}
	stopOnce sync.Once
}

// New creates the API service. Its /readyz runs checks.
func New(
	cfg Config,
	logger *slog.Logger,
	checks *healthutil.Checks,
	accountService services.AccountService,
	userService services.UserService,
	projectService services.ProjectService,
	apiKeyService services.APIKeyService,
	workerService services.WorkerService,
	runService services.RunService,
) (*Service, error) {
	spec, err := openapi3.NewLoader().LoadFromData(openapi.Spec)
	if err != nil {
		return nil, fmt.Errorf("load openapi spec: %w", err)
	}

	operations, err := middleware.Operations(spec)
	if err != nil {
		return nil, fmt.Errorf("read operations: %w", err)
	}

	return &Service{
		cfg:            cfg,
		logger:         logger,
		checks:         checks,
		accountService: accountService,
		userService:    userService,
		projectService: projectService,
		apiKeyService:  apiKeyService,
		workerService:  workerService,
		runService:     runService,
		spec:           spec,
		operations:     operations,
		stopped:        make(chan struct{}),
	}, nil
}

// Run serves until Stop is called or ctx ends, then shuts the HTTP server down.
func (s *Service) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	return s.serve(ctx, listener)
}

func (s *Service) serve(ctx context.Context, listener net.Listener) error {
	srv := s.newHTTPServer()

	served := make(chan error, 1)
	go func() {
		served <- srv.Serve(listener)
	}()

	s.logger.InfoContext(ctx, "api server listening", slog.String("addr", listener.Addr().String()))

	select {
	case <-s.stopped:
	case <-ctx.Done():
	case err := <-served:
		return fmt.Errorf("serve http: %w", err)
	}

	s.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http: %w", errors.Join(err, srv.Close()))
	}

	return nil
}

// Stop starts a graceful shutdown. It is safe to call more than once, and before Run.
func (s *Service) Stop() {
	s.stopOnce.Do(func() { close(s.stopped) })
}

func (s *Service) newHTTPServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", s.livez)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.Handle("/api/", s.apiHandler())
	mux.Handle("/", uiHandler(ui.Dist()))

	if s.cfg.Swagger {
		swagger.Register(mux, openapi.Spec)
	} else {
		mux.Handle("/docs", http.NotFoundHandler())
		mux.Handle("/docs/", http.NotFoundHandler())
	}

	return &http.Server{
		Handler:           mux,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func (s *Service) apiHandler() http.Handler {
	strict := gen.NewStrictHandlerWithOptions(
		s.newHandler(),
		// The last middleware wraps the others, so it runs first: CheckPermission, then
		// AuthorizeProject.
		[]gen.StrictMiddlewareFunc{
			middleware.AuthorizeProject(s.operations, s.accountService),
			middleware.CheckPermission(s.operations),
		},
		gen.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  s.handleRequestError,
			ResponseErrorHandlerFunc: s.handleResponseError,
		},
	)

	routes := gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseURL:          "/api/v1",
		BaseRouter:       http.NewServeMux(),
		ErrorHandlerFunc: s.handleRequestError,
	})

	return middleware.Chain(routes,
		middleware.LogRequest(s.logger),
		middleware.Recover(),
		middleware.LimitBody(maxBodyBytes),
		middleware.CheckOrigin(),
		middleware.Authenticate(sessionCookieName, s.accountService, s.handleResponseError),
		middleware.ValidateRequest(s.spec, "/api/v1"),
	)
}

var _ gen.StrictServerInterface = (*handler)(nil)

type handler struct {
	*authHandler
	*projectHandler
	*userHandler
	*memberHandler
	*apiKeyHandler
	*workerHandler
	*runHandler
	*versionHandler
}

func (s *Service) newHandler() *handler {
	return &handler{
		authHandler:    newAuthHandler(s.accountService, s.cfg.SecureCookie),
		projectHandler: newProjectHandler(s.projectService),
		userHandler:    newUserHandler(s.userService),
		memberHandler:  newMemberHandler(s.projectService),
		apiKeyHandler:  newAPIKeyHandler(s.apiKeyService),
		workerHandler:  newWorkerHandler(s.workerService),
		runHandler:     newRunHandler(s.runService),
		versionHandler: newVersionHandler(),
	}
}

func (s *Service) livez(w http.ResponseWriter, _ *http.Request) {
	writeHealth(w, http.StatusOK, "ok")
}

func (s *Service) readyz(w http.ResponseWriter, r *http.Request) {
	if isStopping(s.stopped) {
		writeHealth(w, http.StatusServiceUnavailable, "shutting down")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()

	if err := s.checks.Ready(ctx); err != nil {
		s.logger.WarnContext(ctx, "readiness check failed", slog.Any("error", err))
		writeHealth(w, http.StatusServiceUnavailable, "not ready")
		return
	}

	writeHealth(w, http.StatusOK, "ok")
}

func isStopping(stopped <-chan struct{}) bool {
	select {
	case <-stopped:
		return true
	default:
		return false
	}
}

func writeHealth(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
