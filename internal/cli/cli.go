// Package cli is the proxie command line, built with Kong.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/alecthomas/kong"
	"github.com/schmenniooo/proxie/internal/config"
	"github.com/schmenniooo/proxie/internal/doctor"
)

// name is the program name in help and error messages.
const name = "proxie"

const description = `A caching reverse proxy for LLM APIs.

Every flag falls back to the PROXIE_* environment variable shown next to it,
then to its default. Tenants are configured through the environment only:
PROXIE_API_KEY_<TENANT> (comma-separated keys) and optionally
PROXIE_UPSTREAM_URL_<TENANT>.`

// CLI is the root command. The embedded config makes every setting a flag
// that all commands share.
type CLI struct {
	config.Config `embed:""`

	Serve      ServeCmd   `cmd:"" default:"withargs" help:"Run the proxy (the default when no command is given)."`
	Doctor     DoctorCmd  `cmd:"" help:"Check the config and its dependencies: Redis, upstreams, log file."`
	ShowConfig ConfigCmd  `cmd:"" name:"config" help:"Print the resolved config with secrets masked."`
	Version    VersionCmd `cmd:"" help:"Print the version and commit."`
}

// exitCode ends a command with the given exit code. Commands return it after
// they have already reported the problem themselves.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit code %d", int(e)) }

// exitPanic carries Kong's exit code (for --help) out of Parse.
type exitPanic struct{ code int }

// Run parses args (without the program name), runs the selected command and
// returns the process exit code. Settings come from args and the process
// environment.
//
// A command's Run method gets its parameters from Kong by type: the values
// bound below (ctx, the parsed config and stdout) plus the command struct
// itself as receiver.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) (code int) {
	var cli CLI
	parser, err := kong.New(&cli,
		kong.Name(name),
		kong.Description(description),
		kong.Writers(stdout, stderr),
		kong.Exit(func(code int) { panic(exitPanic{code}) }),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.BindTo(stdout, (*io.Writer)(nil)),
		kong.Bind(&cli.Config),
	)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 1
	}
	defer func() {
		switch p := recover().(type) {
		case nil:
		case exitPanic:
			code = p.code
		default:
			panic(p)
		}
	}()

	kctx, err := parser.Parse(args)
	if err != nil {
		var pe *kong.ParseError
		// A value from the environment Kong cannot parse (for example
		// PROXIE_CACHE_TTL=soon) is a failed config check for doctor, not a
		// crash. Usage errors (ctx.Error) are still reported as such.
		if errors.As(err, &pe) && pe.Context != nil && pe.Context.Error == nil && pe.Context.Command() == "doctor" {
			return exit(stderr, (&DoctorCmd{JSON: slices.Contains(args, "--json")}).report(stdout, doctor.Run(ctx, config.Config{}, err)))
		}
		_, _ = fmt.Fprintf(stderr, "%s: %v\nRun %s --help for usage.\n", name, err, name)
		return 2
	}
	return exit(stderr, kctx.Run())
}

func exit(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	if code := exitCode(0); errors.As(err, &code) {
		return int(code)
	}
	_, _ = fmt.Fprintf(stderr, "%s: %v\n", name, err)
	return 1
}
