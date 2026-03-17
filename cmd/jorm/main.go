package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/jorm/internal/loop"
	"github.com/jorm/internal/store"
)

func main() {
	var (
		configPath string
		repoDir    string
		profile    string
	)

	root := &cobra.Command{
		Use:   "jorm",
		Short: "Autonomous dev loop harness powered by Claude Code",
	}

	root.PersistentFlags().StringVar(&configPath, "config", ".dev-loop.yaml", "path to config file")
	root.PersistentFlags().StringVar(&repoDir, "repo", ".", "path to git repository")
	root.PersistentFlags().StringVar(&profile, "profile", "", "validator profile to use")

	runCmd := &cobra.Command{
		Use:   "run <issue-id>",
		Short: "Run the dev loop for an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return loop.Run(context.Background(), loop.Options{
				ConfigPath: configPath,
				RepoDir:    repoDir,
				Profile:    profile,
				IssueID:    args[0],
			})
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
