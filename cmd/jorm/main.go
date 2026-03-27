package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"os/exec"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/jorm/internal/config"
	"github.com/jorm/internal/conductor"
	"github.com/jorm/internal/events"
	"github.com/jorm/internal/jormpath"
	"github.com/jorm/internal/loop"
	"github.com/jorm/internal/mcp"
	"github.com/jorm/internal/store"
	"github.com/jorm/internal/ui"
)

// Set via -ldflags at build time.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	var (
		configPath string
		repoDir    string
		profile    string
		noTUI      bool
	)

	root := &cobra.Command{
		Use:   "jorm",
		Short: "Autonomous dev loop harness powered by Claude Code",
	}

	root.PersistentFlags().StringVar(&configPath, "config", ".jorm/config.yaml", "path to config file")
	root.PersistentFlags().StringVar(&repoDir, "repo", ".", "path to git repository")
	root.PersistentFlags().StringVar(&profile, "profile", "", "validator profile to use")
	root.PersistentFlags().BoolVar(&noTUI, "no-tui", false, "disable TUI, use plain text output")

	// Run command
	var (
		worktreeFlag bool
		prFlag       bool
		shipFlag     bool
		debugFlag    bool
		modelFlag    string
	)

	runCmd := &cobra.Command{
		Use:   "run <issue-id|file|prompt>",
		Short: "Run the dev loop for an issue ID, markdown file, or freeform prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := args[0]
			opts := loop.Options{
				ConfigPath: configPath,
				RepoDir:    repoDir,
				Profile:    profile,
				Worktree:   worktreeFlag,
				PR:         prFlag,
				Ship:       shipFlag,
				Debug:      debugFlag,
				Model:      modelFlag,
			}

			// Detect input type: number = issue ID, .md file = file provider, else = freeform prompt
			if isIssueID(arg) {
				opts.IssueID = arg
			} else if isMarkdownFile(arg) {
				opts.IssueID = strings.TrimSuffix(filepath.Base(arg), filepath.Ext(arg))
				opts.Title = filepath.Base(arg)
				// Body will be loaded by the loop from the file
				data, err := os.ReadFile(arg)
				if err != nil {
					return fmt.Errorf("reading file %s: %w", arg, err)
				}
				opts.Body = string(data)
			} else {
				// Freeform prompt
				opts.IssueID = fmt.Sprintf("prompt-%d", time.Now().Unix())
				opts.Title = arg
				opts.Body = arg
			}

			if noTUI {
				return loop.Run(context.Background(), opts)
			}
			// Use new zeroshot-style scrolling UI
			opts.SinkFactory = func(runID string, agentCount int) events.Sink {
				return ui.New(runID, agentCount)
			}
			return loop.Run(context.Background(), opts)
		},
	}
	runCmd.Flags().BoolVar(&worktreeFlag, "worktree", false, "create git worktree for isolation")
	runCmd.Flags().BoolVar(&prFlag, "pr", false, "create PR on completion (implies --worktree)")
	runCmd.Flags().BoolVar(&shipFlag, "ship", false, "create PR and auto-merge (implies --pr)")
	runCmd.Flags().BoolVar(&debugFlag, "debug", false, "enable debug logging")
	runCmd.Flags().StringVar(&modelFlag, "model", "", "model override (e.g. sonnet, opus, haiku)")

	// Resume command
	resumeCmd := &cobra.Command{
		Use:   "resume <issue-id>",
		Short: "Resume a previous run for an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return loop.Run(context.Background(), loop.Options{
				ConfigPath: configPath,
				RepoDir:    repoDir,
				Profile:    profile,
				IssueID:    args[0],
				Resume:     true,
			})
		},
	}

	// List command
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all jorm runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := store.New()
			if err != nil {
				return err
			}
			defer st.Close()

			runs, err := st.List()
			if err != nil {
				return err
			}

			if len(runs) == 0 {
				fmt.Println("No runs found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tISSUE\tBRANCH\tSTATUS\tATTEMPTS\tUPDATED")
			for _, r := range runs {
				status := r.Status
				switch status {
				case "accepted":
					status = color.GreenString(status)
				case "rejected", "failed":
					status = color.RedString(status)
				case "running":
					status = color.YellowString(status)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
					r.ID, r.IssueID, r.Branch, status, r.Attempt, r.UpdatedAt.Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}

	// Status command
	statusCmd := &cobra.Command{
		Use:   "status [run-id]",
		Short: "Show status of a run or all runs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := store.New()
			if err != nil {
				return err
			}
			defer st.Close()

			if len(args) == 1 {
				run, err := st.Load(args[0])
				if err != nil {
					return fmt.Errorf("loading run: %w", err)
				}
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintf(w, "ID:\t%s\n", run.ID)
				fmt.Fprintf(w, "Issue:\t%s\n", run.IssueID)
				fmt.Fprintf(w, "Branch:\t%s\n", run.Branch)
				fmt.Fprintf(w, "Status:\t%s\n", run.Status)
				fmt.Fprintf(w, "Attempts:\t%d\n", run.Attempt)
				fmt.Fprintf(w, "Worktree:\t%s\n", run.WorktreeDir)
				fmt.Fprintf(w, "Created:\t%s\n", run.CreatedAt.Format(time.RFC3339))
				fmt.Fprintf(w, "Updated:\t%s\n", run.UpdatedAt.Format(time.RFC3339))
				if run.Findings != "" {
					fmt.Fprintf(w, "Findings:\t%s\n", run.Findings)
				}
				return w.Flush()
			}

			runs, err := st.List()
			if err != nil {
				return err
			}
			if len(runs) == 0 {
				fmt.Println("No runs found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tISSUE\tSTATUS\tATTEMPTS\tUPDATED")
			for _, r := range runs {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
					r.ID, r.IssueID, r.Status, r.Attempt, r.UpdatedAt.Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}

	// Logs command
	var followFlag bool
	logsCmd := &cobra.Command{
		Use:   "logs <run-id>",
		Short: "View logs for a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projDir, err := jormpath.ProjectDir()
			if err != nil {
				return err
			}
			logPath := filepath.Join(projDir, "logs", args[0]+".log")

			if followFlag {
				c := exec.Command("tail", "-f", logPath)
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				return c.Run()
			}

			data, err := os.ReadFile(logPath)
			if err != nil {
				return fmt.Errorf("reading log file: %w", err)
			}
			fmt.Print(string(data))
			return nil
		},
	}
	logsCmd.Flags().BoolVarP(&followFlag, "follow", "f", false, "tail the log file")

	// Stop command
	stopCmd := &cobra.Command{
		Use:   "stop <run-id>",
		Short: "Signal a running jorm process to stop",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			storeDir, err := jormpath.StoreDir()
			if err != nil {
				return err
			}
			signalPath := filepath.Join(storeDir, fmt.Sprintf("stop-%s", args[0]))
			if err := os.WriteFile(signalPath, []byte("stop"), 0o644); err != nil {
				return fmt.Errorf("writing stop signal: %w", err)
			}
			fmt.Printf("Stop signal written for run %s\n", args[0])
			return nil
		},
	}

	// Clean command
	var cleanAll bool
	cleanCmd := &cobra.Command{
		Use:   "clean [run-id]",
		Short: "Clean up worktrees and run data",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := store.New()
			if err != nil {
				return err
			}
			defer st.Close()

			if cleanAll {
				runs, err := st.List()
				if err != nil {
					return err
				}
				var errs []string
				for _, r := range runs {
					if err := cleanRun(st, r); err != nil {
						errs = append(errs, err.Error())
					}
				}
				if len(errs) > 0 {
					return fmt.Errorf("cleaning runs: %s", strings.Join(errs, "; "))
				}
				fmt.Printf("Cleaned %d runs\n", len(runs))
				return nil
			}

			if len(args) == 0 {
				return fmt.Errorf("specify a run-id or use --all")
			}

			run, err := st.Load(args[0])
			if err != nil {
				return fmt.Errorf("loading run: %w", err)
			}
			if err := cleanRun(st, run); err != nil {
				return err
			}
			fmt.Printf("Cleaned run %s\n", run.ID)
			return nil
		},
	}
	cleanCmd.Flags().BoolVar(&cleanAll, "all", false, "clean all runs")

	// Inspect command
	var timelineFlag bool
	inspectCmd := &cobra.Command{
		Use:   "inspect <run-id>",
		Short: "Inspect the message bus ledger for a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := store.New()
			if err != nil {
				return err
			}
			defer st.Close()

			msgs, err := st.QueryMessages(args[0], "")
			if err != nil {
				return fmt.Errorf("querying messages: %w", err)
			}

			if len(msgs) == 0 {
				fmt.Println("No messages found for this run.")
				return nil
			}

			if timelineFlag {
				for _, m := range msgs {
					fmt.Printf("%s  %-25s  %-15s  %s\n",
						m.Timestamp.Format("15:04:05.000"),
						m.Topic,
						m.Sender,
						truncate(m.Content, 80))
				}
				return nil
			}

			for i, m := range msgs {
				if i > 0 {
					fmt.Println(strings.Repeat("─", 80))
				}
				fmt.Printf("Topic:   %s\n", m.Topic)
				fmt.Printf("Sender:  %s\n", m.Sender)
				fmt.Printf("Time:    %s\n", m.Timestamp.Format(time.RFC3339))
				if m.Content != "" {
					fmt.Printf("Content:\n%s\n", m.Content)
				}
			}
			return nil
		},
	}
	inspectCmd.Flags().BoolVar(&timelineFlag, "timeline", false, "compact one-line-per-message view")

	// Config command
	var (
		validateConfig bool
		workflowName   string
	)
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Show resolved configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				if validateConfig {
					fmt.Printf("Config validation failed: %s\n", err)
					return err
				}
				return err
			}

			if validateConfig {
				// Verify profiles reference valid validators
				for name, ids := range cfg.Profiles {
					if _, err := cfg.ValidatorsForProfile(name); err != nil {
						fmt.Printf("Profile %q: %s\n", name, err)
						return err
					}
					fmt.Printf("Profile %q: %d validators OK\n", name, len(ids))
				}
				fmt.Println("Config is valid.")
				return nil
			}

			if workflowName != "" {
				templates := conductor.BuiltinStagedTemplates("")
				tmpl, ok := templates[workflowName]
				if !ok {
					return fmt.Errorf("unknown workflow: %s\nAvailable: %s", workflowName, availableWorkflows(templates))
				}
				fmt.Printf("  Worker:  model=%-8s  maxIter=%d\n", tmpl.WorkerConfig.Model, tmpl.WorkerConfig.MaxIterations)
				fmt.Printf("  Tester:  %s (%s)\n", tmpl.TesterConfig.Name, tmpl.TesterConfig.Command)
				fmt.Printf("  Stages:  %d\n", len(tmpl.Stages))
				for _, s := range tmpl.Stages {
					fmt.Printf("    - %s (%s)\n", s.Name, s.Kind)
				}
				return nil
			}

			// Print resolved config
			fmt.Printf("Config: %s\n", configPath)
			fmt.Printf("Model: %s\n", cfg.Model)
			fmt.Printf("Profile: %s\n", cfg.Profile)
			fmt.Printf("Issue Provider: %s\n", cfg.IssueProvider)
			fmt.Printf("Conductor: classify_model=%s\n", cfg.Conductor.ClassifyModel)
			if len(cfg.Validators) > 0 {
				fmt.Printf("Validators: %d\n", len(cfg.Validators))
				for _, v := range cfg.Validators {
					fmt.Printf("  - %s (%s/%s) on_fail=%s run_on=%s\n", v.Name, v.Type, v.Mode, v.OnFail, v.RunOn)
				}
			}
			if len(cfg.Profiles) > 0 {
				fmt.Printf("Profiles:\n")
				for name, ids := range cfg.Profiles {
					fmt.Printf("  %s: %s\n", name, strings.Join(ids, ", "))
				}
			}
			return nil
		},
	}
	configCmd.Flags().BoolVar(&validateConfig, "validate", false, "validate config file")
	configCmd.Flags().StringVar(&workflowName, "workflow", "", "show agents for a workflow template")

	// Version command
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("jorm %s (commit: %s, built: %s)\n", version, commit, buildDate)
		},
	}

	// Init command (stub)
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Generate .jorm/ configuration (LLM-assisted)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("jorm init: coming soon")
			fmt.Println("For now, create .jorm/config.yaml manually.")
			return nil
		},
	}

	// Hide commands that assume daemon/background mode or aren't implemented yet.
	// They still work if typed directly — just hidden from --help.
	resumeCmd.Hidden = true
	statusCmd.Hidden = true
	stopCmd.Hidden = true
	cleanCmd.Hidden = true
	inspectCmd.Hidden = true
	initCmd.Hidden = true
	demoCmd := newDemoCmd()
	demoCmd.Hidden = true

	root.AddCommand(runCmd, resumeCmd, listCmd, statusCmd, logsCmd, stopCmd, cleanCmd, inspectCmd, configCmd, initCmd, versionCmd, demoCmd, newMCPCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

var jiraKeyPattern = regexp.MustCompile(`^[A-Z]+-[0-9]+$`)

func isIssueID(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil || jiraKeyPattern.MatchString(s)
}

func isMarkdownFile(s string) bool {
	ext := strings.ToLower(filepath.Ext(s))
	return ext == ".md" || ext == ".markdown"
}

func truncate(s string, max int) string {
	// Take first line only
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max-3]) + "..."
	}
	return s
}

func cleanRun(st *store.Store, r *store.RunState) error {
	if r.WorktreeDir != "" {
		// Check for uncommitted changes before destroying worktree
		cmd := exec.Command("git", "status", "--porcelain")
		cmd.Dir = r.WorktreeDir
		if out, err := cmd.Output(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
			fmt.Fprintf(os.Stderr, "Warning: worktree %s has uncommitted changes, removing anyway\n", r.WorktreeDir)
		}
		os.RemoveAll(r.WorktreeDir)
	}
	if err := st.Delete(r.ID); err != nil {
		return fmt.Errorf("deleting run %s: %w", r.ID, err)
	}
	return nil
}

func newMCPCmd() *cobra.Command {
	var (
		dbFlag   string
		logsFlag string
	)
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server on stdio for run monitoring and log analysis",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dbFlag == "" || logsFlag == "" {
				storeDir, err := jormpath.StoreDir()
				if err != nil {
					return err
				}
				if dbFlag == "" {
					dbFlag = filepath.Join(storeDir, "jorm.db")
				}
				if logsFlag == "" {
					projDir, err := jormpath.ProjectDir()
					if err != nil {
						return err
					}
					logsFlag = filepath.Join(projDir, "logs")
				}
			}
			return mcp.NewServer(dbFlag, logsFlag).Run()
		},
	}
	cmd.Flags().StringVar(&dbFlag, "db", "", "path to jorm SQLite database (default ~/.jorm/jorm.db)")
	cmd.Flags().StringVar(&logsFlag, "logs", "", "path to log directory (default ~/.jorm/logs)")
	return cmd
}

func availableWorkflows(templates map[string]conductor.StagedTemplate) string {
	names := make([]string, 0, len(templates))
	for name := range templates {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

