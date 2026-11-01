package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

const agentsUsage = `List the AI agents and bots ciblame recognises, and the signals it uses.

Usage:
  ciblame agents [--json] [--signatures FILE]
`

func runAgents(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("ciblame agents", stderr, agentsUsage)
	asJSON := fs.Bool("json", false, "print the signatures as JSON")
	sigPath := fs.String("signatures", "", "JSON file with extra signatures to include")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	c, err := buildClassifier(*sigPath)
	if err != nil {
		return err
	}
	sigs := c.Signatures()
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"version": 1, "signatures": sigs})
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tKIND\tVENDOR\tSIGNALS")
	for _, s := range sigs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Name, s.Kind, orDash(s.Vendor), strings.Join(s.Signals(), ", "))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "\nAnything else counts as a generic bot (accounts ending in [bot]) or a human.")
	fmt.Fprintln(stdout, "Missing an agent? Add a signature: https://github.com/amlaban65/ciblame/blob/main/CONTRIBUTING.md")
	return nil
}
