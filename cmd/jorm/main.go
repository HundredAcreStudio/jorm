package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/jorm/internal/loop"
	"github.com/jorm/internal/store"
	"github.com/jorm/internal/tui"
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

	root.PersistentFlags().StringVar(&configPath, "config", ".dev-loop.yaml", "path to config file")
	root.PersistentFlags().StringVar(&repoDir, "repo", ".", "path to git repository")
	root.PersistentFlags().StringVar(&profile, "profile", "", "validator profile to use")
	root.PersistentFlags().BoolVar(&noTUI, "no-tui", false, "disable TUI, use plain text output")

	runCmd := &cobra.Command{
		Use:   "run <issue-id or prompt>",
		Short: "Run the dev loop for an issue ID or freeform prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := args[0]
			opts := loop.Options{
				ConfigPath: configPath,
				RepoDir:    repoDir,
				Profile:    profile,
			}

			// If the arg looks like a number, treat it as an issue ID
			if isIssueID(arg) {
				opts.IssueID = arg
			} else {
				// Freeform prompt
				opts.IssueID = fmt.Sprintf("prompt-%d", time.Now().Unix())
				opts.Title = arg
				opts.Body = arg
			}

			if noTUI {
				return loop.Run(context.Background(), opts)
			}
			return tui.Run(context.Background(), opts)
		},
	}

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

	root.AddCommand(runCmd, resumeCmd, listCmd)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func isIssueID(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}
