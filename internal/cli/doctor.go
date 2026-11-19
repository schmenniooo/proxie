package cli

import (
	"context"

	"github.com/schmenniooo/proxie/internal/config"
	"github.com/schmenniooo/proxie/internal/doctor"
)

// DoctorCmd checks the config and the dependencies.
type DoctorCmd struct {
	JSON bool `help:"Print the results as JSON."`
}

// Run runs the checks and fails when any check fails. An invalid config is
// a failed check, not an error.
func (d *DoctorCmd) Run(ctx context.Context, cfg *config.Config, s *streams) error {
	err := cfg.Complete(s.environ)
	return d.report(s, doctor.Run(ctx, *cfg, err))
}

func (d *DoctorCmd) report(s *streams, results []doctor.Result) error {
	write := doctor.WriteTable
	if d.JSON {
		write = doctor.WriteJSON
	}
	if err := write(s.stdout, results); err != nil {
		return err
	}
	if doctor.Failed(results) {
		return exitCode(1)
	}
	return nil
}
