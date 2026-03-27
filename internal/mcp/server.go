package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server is the jorm MCP server that exposes run monitoring tools.
type Server struct {
	dbPath string
	logDir string
}

// NewServer creates a new MCP server with the given database and log paths.
func NewServer(dbPath, logDir string) *Server {
	return &Server{dbPath: dbPath, logDir: logDir}
}

// Run starts the MCP server on stdio, blocking until the client disconnects.
func (s *Server) Run() error {
	mcpServer := server.NewMCPServer("jorm", "1.0.0",
		server.WithToolCapabilities(false),
	)

	// jorm_list_runs
	mcpServer.AddTool(
		mcp.NewTool("jorm_list_runs",
			mcp.WithDescription("List all jorm runs with status, issue, elapsed time, and cost"),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			db, err := openReadOnlyDB(s.dbPath)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			defer db.Close()

			out, err := listRuns(db)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(out), nil
		},
	)

	// jorm_get_status
	mcpServer.AddTool(
		mcp.NewTool("jorm_get_status",
			mcp.WithDescription("Get current status of a jorm run including agent states, round, and cost"),
			mcp.WithString("run_id", mcp.Required(), mcp.Description("The run ID to get status for")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			runID := mcp.ParseString(req, "run_id", "")
			if runID == "" {
				return mcp.NewToolResultError("run_id is required"), nil
			}

			db, err := openReadOnlyDB(s.dbPath)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			defer db.Close()

			out, err := getStatus(db, runID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(out), nil
		},
	)

	// jorm_get_logs
	mcpServer.AddTool(
		mcp.NewTool("jorm_get_logs",
			mcp.WithDescription("Get recent log lines from a jorm run's log file. Returns JSON-lines slog output."),
			mcp.WithString("run_id", mcp.Required(), mcp.Description("The run ID to get logs for")),
			mcp.WithString("since", mcp.Description("Only return logs after this RFC3339 timestamp")),
			mcp.WithNumber("limit", mcp.Description("Maximum number of log lines to return (default: all)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			runID := mcp.ParseString(req, "run_id", "")
			if runID == "" {
				return mcp.NewToolResultError("run_id is required"), nil
			}

			var since time.Time
			sinceStr := mcp.ParseString(req, "since", "")
			if sinceStr != "" {
				var err error
				since, err = time.Parse(time.RFC3339, sinceStr)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("invalid since timestamp: %v", err)), nil
				}
			}

			limit := mcp.ParseInt(req, "limit", 0)

			out, err := getLogs(s.logDir, runID, since, limit)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(out), nil
		},
	)

	// jorm_get_messages
	mcpServer.AddTool(
		mcp.NewTool("jorm_get_messages",
			mcp.WithDescription("Query the SQLite message bus for a run's messages, with optional filters"),
			mcp.WithString("run_id", mcp.Required(), mcp.Description("The run ID to query messages for")),
			mcp.WithString("topic", mcp.Description("Filter messages by topic (e.g. PLAN_READY, VALIDATION_RESULT)")),
			mcp.WithString("sender", mcp.Description("Filter messages by sender (e.g. worker, validator-build)")),
			mcp.WithNumber("limit", mcp.Description("Maximum number of messages to return (default: all)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			runID := mcp.ParseString(req, "run_id", "")
			if runID == "" {
				return mcp.NewToolResultError("run_id is required"), nil
			}

			topic := mcp.ParseString(req, "topic", "")
			sender := mcp.ParseString(req, "sender", "")
			limit := mcp.ParseInt(req, "limit", 0)

			db, err := openReadOnlyDB(s.dbPath)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			defer db.Close()

			out, err := getMessages(db, runID, topic, sender, limit)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(out), nil
		},
	)

	// jorm_inspect
	mcpServer.AddTool(
		mcp.NewTool("jorm_inspect",
			mcp.WithDescription("Full message ledger as a compact timeline for a jorm run"),
			mcp.WithString("run_id", mcp.Required(), mcp.Description("The run ID to inspect")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			runID := mcp.ParseString(req, "run_id", "")
			if runID == "" {
				return mcp.NewToolResultError("run_id is required"), nil
			}

			db, err := openReadOnlyDB(s.dbPath)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			defer db.Close()

			out, err := inspect(db, runID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(out), nil
		},
	)

	return server.ServeStdio(mcpServer)
}
