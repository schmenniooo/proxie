package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/schmenniooo/proxie/internal/config"
)

// ConfigCmd prints the resolved config: flags, then env vars, then defaults.
type ConfigCmd struct {
	JSON bool `help:"Print the config as JSON."`
}

// Run prints the config through Config.LogValue, so secrets are masked
// exactly as in the startup log line.
func (c *ConfigCmd) Run(cfg *config.Config, stdout io.Writer) error {
	if err := cfg.Complete(os.Environ()); err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}
	v := cfg.LogValue()
	if c.JSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(toJSON(v))
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	writeText(tw, "", v)
	return tw.Flush()
}

// writeText writes one "key  value" line per leaf, with dotted keys for
// groups (tenants.acme.keys).
func writeText(tw *tabwriter.Writer, prefix string, v slog.Value) {
	v = v.Resolve()
	if v.Kind() != slog.KindGroup {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", prefix, textValue(v))
		return
	}
	for _, a := range v.Group() {
		key := a.Key
		if prefix != "" {
			key = prefix + "." + key
		}
		writeText(tw, key, a.Value)
	}
}

func textValue(v slog.Value) string {
	if list, ok := v.Any().([]string); ok {
		return strings.Join(list, ",")
	}
	return v.String()
}

// toJSON converts v into values encoding/json can marshal.
func toJSON(v slog.Value) any {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindGroup:
		m := map[string]any{}
		for _, a := range v.Group() {
			m[a.Key] = toJSON(a.Value)
		}
		return m
	case slog.KindBool, slog.KindInt64, slog.KindUint64, slog.KindFloat64, slog.KindAny:
		return v.Any()
	default:
		return v.String()
	}
}
