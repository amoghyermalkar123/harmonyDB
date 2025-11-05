package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"harmonydb/raft/proto"
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type model struct {
	table       table.Model
	client      proto.RaftClient
	conn        *grpc.ClientConn
	lastUpdate  time.Time
	nodeInfo    string
	errorMsg    string
	nodePort    int
	logs        []*proto.Log
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m = m.refreshData()
		}

	case tickMsg:
		m = m.refreshData()
		return m, tick()

	case tea.WindowSizeMsg:
		// Adjust table size
		m.table.SetWidth(msg.Width - 4)
		m.table.SetHeight(msg.Height - 8)
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) refreshData() model {
	if m.client == nil {
		m.errorMsg = "Not connected to Raft node"
		return m
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Get node status via RPC
	statusResp, err := m.client.GetStatus(ctx, &proto.GetStatusRequest{})
	if err != nil {
		m.errorMsg = fmt.Sprintf("Failed to get status: %v", err)
		return m
	}

	// Get logs via RPC
	logsResp, err := m.client.GetLogs(ctx, &proto.GetLogsRequest{})
	if err != nil {
		m.errorMsg = fmt.Sprintf("Failed to get logs: %v", err)
		return m
	}

	logs := logsResp.Logs
	m.logs = logs

	// Update node info
	m.nodeInfo = fmt.Sprintf("Node ID: %d | Port: %d | State: %s | Term: %d | Applied: %d | Committed: %d | Total Logs: %d",
		statusResp.NodeId, m.nodePort, statusResp.NodeState, statusResp.CurrentTerm,
		statusResp.LastApplied, statusResp.LastCommitted, len(logs))

	// Convert logs to table rows
	rows := make([]table.Row, 0, len(logs))
	for i, log := range logs {
		status := "Pending"
		if log.Id <= statusResp.LastCommitted {
			status = "Committed"
		}
		if log.Id <= statusResp.LastApplied {
			status = "Applied"
		}

		if log.Data != nil {
			rows = append(rows, table.Row{
				fmt.Sprintf("%d", log.Id),
				fmt.Sprintf("%d", log.Term),
				log.Data.Op,
				log.Data.Key,
				truncateString(log.Data.Value, 30),
				status,
				fmt.Sprintf("#%d", i+1),
			})
		}
	}

	m.table.SetRows(rows)
	m.lastUpdate = time.Now()
	m.errorMsg = ""

	return m
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func (m model) View() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Render("🔍 HarmonyDB Raft Log Monitor")

	info := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(fmt.Sprintf("Last updated: %s | Press 'r' to refresh manually | Press 'q' to quit",
			m.lastUpdate.Format("15:04:05")))

	nodeStatus := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Render(m.nodeInfo)

	var content string
	if m.errorMsg != "" {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)
		content = errorStyle.Render("Error: " + m.errorMsg)
	} else {
		content = baseStyle.Render(m.table.View())
	}

	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Navigate: ↑/↓ arrows | Refresh: r | Quit: q")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		nodeStatus,
		"",
		info,
		"",
		content,
		"",
		footer,
	)
}

func connectToRaftNode(port int) (proto.RaftClient, *grpc.ClientConn, error) {
	address := fmt.Sprintf("localhost:%d", port)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to Raft node at %s: %v", address, err)
	}

	client := proto.NewRaftClient(conn)

	// Test the connection
	_, err = client.GetStatus(ctx, &proto.GetStatusRequest{})
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to get status from Raft node: %v", err)
	}

	return client, conn, nil
}

func main() {
	var nodePort = flag.Int("port", 8080, "Port of the Raft node to monitor")
	flag.Parse()

	fmt.Printf("🔍 Connecting to Raft node on port %d...\n", *nodePort)

	// Connect to the specified Raft node
	client, conn, err := connectToRaftNode(*nodePort)
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		fmt.Println("💡 Make sure a Raft node is running on the specified port")
		fmt.Println("   You can start one with: go run ./raft/cmd/main.go")
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Printf("✅ Successfully connected to Raft node on port %d\n", *nodePort)

	columns := []table.Column{
		{Title: "ID", Width: 8},
		{Title: "Term", Width: 8},
		{Title: "Op", Width: 6},
		{Title: "Key", Width: 20},
		{Title: "Value", Width: 30},
		{Title: "Status", Width: 12},
		{Title: "Index", Width: 8},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	m := model{
		table:      t,
		client:     client,
		conn:       conn,
		lastUpdate: time.Now(),
		nodeInfo:   "Initializing Raft monitor...",
		nodePort:   *nodePort,
	}

	// Refresh data initially
	m = m.refreshData()

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}