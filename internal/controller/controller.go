// Package controller implements the controller-runtime reconciler for
// orchestrating TaskRun lifecycles within the RoboDev operator.
package controller

import (
	"context"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/robodev-inc/robodev/internal/config"
)

// Reconciler implements the controller-runtime reconcile.Reconciler interface
// for orchestrating TaskRun lifecycles.
type Reconciler struct {
	config *config.Config
	logger *slog.Logger
}

// NewReconciler creates a new Reconciler with the given configuration and logger.
func NewReconciler(cfg *config.Config, logger *slog.Logger) *Reconciler {
	return &Reconciler{
		config: cfg,
		logger: logger,
	}
}

// Reconcile handles a single reconciliation request. This is the core loop
// that drives TaskRun state transitions.
func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	r.logger.InfoContext(ctx, "reconciling", "name", req.Name, "namespace", req.Namespace)
	return reconcile.Result{}, nil
}
