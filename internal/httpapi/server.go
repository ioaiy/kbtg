package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ioaiy/kbtg/internal/balance"
	"github.com/ioaiy/kbtg/internal/skinport"
)

// Server владеет всеми зависимостями. Handlers — методы на *Server (Mat Ryer pattern).
type Server struct {
	balance  *balance.Service
	skinport *skinport.Service
	log      *slog.Logger
	router   *chi.Mux
}

func NewServer(
	balanceSvc *balance.Service,
	skinportSvc *skinport.Service,
	log *slog.Logger,
) *Server {
	s := &Server{
		balance:  balanceSvc,
		skinport: skinportSvc,
		log:      log,
	}
	s.router = s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) Router() *chi.Mux {
	return s.router
}

func (s *Server) routes() *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(loggingMiddleware(s.log))

	r.Get("/healthz", s.handleLive)
	r.Get("/readyz", s.handleReady)

	r.Route("/v1", func(r chi.Router) {
		r.Get("/items", s.handleGetItems)
		r.Get("/users/{id}/balance", s.handleGetBalance)
		r.Post("/users/{id}/debit", s.handleDebit)
	})

	return r
}

func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ready"}`))
}

// Run блокирует до отмены ctx, после чего ждёт shutdownTO на завершение in-flight.
func (s *Server) Run(ctx context.Context, addr string, readTO, writeTO, idleTO, shutdownTO time.Duration) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  readTO,
		WriteTimeout: writeTO,
		IdleTimeout:  idleTO,
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("http server starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		s.log.Info("http server shutdown initiated")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTO)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			s.log.Error("http server shutdown error", "err", err)
			return err
		}
		s.log.Info("http server shutdown complete")
		return nil
	case err := <-errCh:
		return err
	}
}
