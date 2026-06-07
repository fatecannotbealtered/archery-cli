package cmd

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var referenceCmd = &cobra.Command{
	Use:   "reference",
	Short: "Print all commands and flags in a structured format",
	Long:  "Outputs every command, subcommand, and flag in a machine-parseable format. Designed for AI Agents and script integration.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if jsonMode {
			output.PrintJSON(buildReferenceTree(rootCmd))
			return nil
		}
		printReference(cmd, rootCmd)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(referenceCmd)
}

// refFlag describes a CLI flag for JSON reference output.
type refFlag struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default string `json:"default"`
	Usage   string `json:"usage"`
}

// refCommand is a node in the machine-readable command tree.
type refCommand struct {
	Name                 string       `json:"name"`
	Path                 string       `json:"path,omitempty"`
	Use                  string       `json:"use"`
	Short                string       `json:"short,omitempty"`
	Write                bool         `json:"write,omitempty"`
	RequiresConfirmation bool         `json:"requiresConfirmation,omitempty"`
	RiskLevel            string       `json:"riskLevel,omitempty"`
	BlastRadius          string       `json:"blastRadius,omitempty"`
	OutputType           string       `json:"outputType,omitempty"`
	Formats              []string     `json:"formats,omitempty"`
	PositionalArgs       []string     `json:"positionalArgs,omitempty"`
	SupportsFields       bool         `json:"supportsFields,omitempty"`
	Flags                []refFlag    `json:"flags,omitempty"`
	Commands             []refCommand `json:"commands,omitempty"`
}

// refTree is the root JSON document for `reference --json`.
type refTree struct {
	Tool          string         `json:"tool"`
	Version       string         `json:"version"`
	SecurityTier  string         `json:"security_tier"`
	GlobalFlags   []refFlag      `json:"globalFlags"`
	ExitCodes     map[int]string `json:"exitCodes"`
	Commands      []refCommand   `json:"commands"`
}

var positionalArgRe = regexp.MustCompile(`<([^>]+)>`)

func buildReferenceTree(root *cobra.Command) refTree {
	tree := refTree{
		Tool:          "archery-cli",
		Version:       root.Version,
		SecurityTier:  "T1",
		GlobalFlags:   collectPersistentRefFlags(root),
		ExitCodes: map[int]string{
			0: "success",
			1: "error",
			2: "bad_args",
			3: "not_found",
			4: "auth_or_permission",
			5: "confirm_required",
			6: "conflict",
			7: "retryable",
			8: "timeout",
		},
	}
	children := root.Commands()
	sort.Slice(children, func(i, j int) bool {
		return children[i].Name() < children[j].Name()
	})
	for _, child := range children {
		if child.Hidden || !child.IsAvailableCommand() {
			continue
		}
		if sub := commandToRef(child, "", ""); sub.Name != "" {
			tree.Commands = append(tree.Commands, sub)
		}
	}
	return tree
}

func collectPersistentRefFlags(root *cobra.Command) []refFlag {
	var flags []refFlag
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		flags = append(flags, refFlagFromPflag(f))
	})
	return flags
}

func commandToRef(cmd *cobra.Command, prefix, pathPrefix string) refCommand {
	if cmd.Hidden {
		return refCommand{}
	}
	name := prefix + cmd.Use
	path := strings.TrimSpace(pathPrefix + cmd.Name())
	node := refCommand{
		Name:  name,
		Path:  path,
		Use:   cmd.Use,
		Short: cmd.Short,
		Flags: collectRefFlags(cmd),
	}
	if cmd.Annotations != nil {
		node.Write = cmd.Annotations["write"] == "true"
		node.RequiresConfirmation = cmd.Annotations["confirm"] == "true"
		node.RiskLevel = cmd.Annotations["riskLevel"]
		node.OutputType = cmd.Annotations["outputType"]
	}
	// Derive blast radius from write flag and risk level (SEC-SPEC §3).
	node.BlastRadius = blastRadius(node.Write, node.RiskLevel)
	node.PositionalArgs = parsePositionalArgs(cmd.Use)
	node.SupportsFields = cmdHasFieldsFlag(cmd)

	children := cmd.Commands()
	if len(children) > 0 {
		sort.Slice(children, func(i, j int) bool {
			return children[i].Name() < children[j].Name()
		})
		childPrefix := prefix + cmd.Name() + " "
		childPath := pathPrefix + cmd.Name() + " "
		for _, child := range children {
			if child.Hidden || !child.IsAvailableCommand() {
				continue
			}
			if sub := commandToRef(child, childPrefix, childPath); sub.Name != "" {
				node.Commands = append(node.Commands, sub)
			}
		}
	} else {
		if node.OutputType == "" {
			node.OutputType = "json"
		}
		node.Formats = commandOutputFormats(cmd)
	}
	return node
}

func parsePositionalArgs(use string) []string {
	parts := strings.Fields(use)
	if len(parts) <= 1 {
		return nil
	}
	var args []string
	for _, part := range parts[1:] {
		for _, m := range positionalArgRe.FindAllStringSubmatch(part, -1) {
			args = append(args, m[1])
		}
	}
	return args
}

func cmdHasFieldsFlag(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	found := false
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "fields" {
			found = true
		}
	})
	return found
}

func collectRefFlags(cmd *cobra.Command) []refFlag {
	local := cmd.LocalFlags()
	var flags []refFlag

	local.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		flags = append(flags, refFlagFromPflag(f))
	})
	return flags
}

func refFlagFromPflag(f *pflag.Flag) refFlag {
	def := f.DefValue
	if def == "" || def == "[]" {
		def = "-"
	}
	return refFlag{Name: f.Name, Type: f.Value.Type(), Default: def, Usage: f.Usage}
}

// blastRadius returns the worst-case impact scope for a command,
// based on whether it writes state and its declared risk level (SEC-SPEC §3).
//
//	read commands  -> low
//	write commands -> medium
//	execute/high   -> high
//	delete/critical -> critical
func blastRadius(write bool, riskLevel string) string {
	if !write {
		return "low"
	}
	switch riskLevel {
	case "critical":
		return "critical"
	case "high":
		return "high"
	default:
		return "medium"
	}
}

func printReference(cmd *cobra.Command, root *cobra.Command) {
	var lines []string
	lines = append(lines, "# archery-cli Command Reference")
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Version: %s", root.Version))
	lines = append(lines, "Security Tier: T1 (Medium)")
	lines = append(lines, "")
	lines = append(lines, "Legend: **write** = mutates Archery state; **confirm** = requires `--dry-run` then `--confirm <confirm_token>`; **output** = default stdout type.")
	lines = append(lines, "")

	walkCommands(root, &lines, "")

	out := cmd.OutOrStdout()
	for _, line := range lines {
		_, _ = fmt.Fprintln(out, line)
	}
}

func walkCommands(cmd *cobra.Command, lines *[]string, prefix string) {
	if cmd.Hidden {
		return
	}

	name := prefix + cmd.Use
	*lines = append(*lines, "## "+name)
	*lines = append(*lines, "")
	if cmd.Short != "" {
		*lines = append(*lines, cmd.Short)
		*lines = append(*lines, "")
	}

	if len(cmd.Commands()) == 0 {
		var meta []string
		if cmd.Annotations != nil {
			if cmd.Annotations["write"] == "true" {
				meta = append(meta, "write")
			}
			if cmd.Annotations["confirm"] == "true" {
				meta = append(meta, "confirm")
			}
			if rl := cmd.Annotations["riskLevel"]; rl != "" {
				meta = append(meta, "risk="+rl)
			}
			if br := blastRadius(cmd.Annotations["write"] == "true", cmd.Annotations["riskLevel"]); br != "" {
				meta = append(meta, "blast_radius="+br)
			}
			if ot := cmd.Annotations["outputType"]; ot != "" {
				meta = append(meta, "output="+ot)
			} else {
				meta = append(meta, "output=json")
			}
			if fmts := commandOutputFormats(cmd); len(fmts) > 0 {
				meta = append(meta, "formats="+strings.Join(fmts, ","))
			}
		}
		if args := parsePositionalArgs(cmd.Use); len(args) > 0 {
			meta = append(meta, "args="+strings.Join(args, ","))
		}
		if cmdHasFieldsFlag(cmd) {
			meta = append(meta, "supports --fields")
		}
		if len(meta) > 0 {
			*lines = append(*lines, strings.Join(meta, " · "))
			*lines = append(*lines, "")
		}
	}

	flags := collectRefFlags(cmd)
	if len(flags) > 0 {
		*lines = append(*lines, "### Flags")
		*lines = append(*lines, "")
		*lines = append(*lines, "| Flag | Type | Default | Description |")
		*lines = append(*lines, "|------|------|---------|-------------|")
		for _, f := range flags {
			*lines = append(*lines, fmt.Sprintf("| `--%s` | %s | %s | %s |", f.Name, f.Type, f.Default, f.Usage))
		}
		*lines = append(*lines, "")
	}

	children := cmd.Commands()
	if len(children) > 0 {
		sort.Slice(children, func(i, j int) bool {
			return children[i].Name() < children[j].Name()
		})
		for _, child := range children {
			if !child.Hidden && child.IsAvailableCommand() {
				walkCommands(child, lines, prefix+cmd.Name()+" ")
			}
		}
	}
}
