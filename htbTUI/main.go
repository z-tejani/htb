package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type pane int

const (
	machinesPane pane = iota
	vpnPane
)

type machineItem struct{ machine Machine }
type vpnItem struct{ server VPNServer }

func (m machineItem) Title() string {
	return fmt.Sprintf("%s (#%d)", m.machine.Name, m.machine.ID)
}

func (m machineItem) Description() string {
	parts := []string{m.machine.OS, m.machine.Difficulty}
	if m.machine.IP != "" {
		parts = append(parts, "IP "+m.machine.IP)
	}
	if m.machine.UserOwned || m.machine.RootOwned {
		parts = append(parts, fmt.Sprintf("owned u:%t r:%t", m.machine.UserOwned, m.machine.RootOwned))
	}
	return strings.Join(parts, " | ")
}

func (m machineItem) FilterValue() string { return m.machine.Name }

func (v vpnItem) Title() string {
	return fmt.Sprintf("%s (#%d)", v.server.Name, v.server.ID)
}

func (v vpnItem) Description() string {
	suffix := ""
	if v.server.Assigned {
		suffix = " | current"
	}
	return fmt.Sprintf("%s | clients %d%s", v.server.Location, v.server.CurrentClients, suffix)
}

func (v vpnItem) FilterValue() string { return v.server.Name }

type loadMachinesMsg struct {
	machines []Machine
	err      error
}

type loadVPNMsg struct {
	servers []VPNServer
	err     error
}

type activeMachineMsg struct {
	machine *Machine
	err     error
}

type spawnDoneMsg struct {
	machine *Machine
	err     error
}

type switchVPNDoneMsg struct {
	server VPNServer
	err    error
}

type submitDoneMsg struct {
	message string
	err     error
}

type saveTokenDoneMsg struct {
	client *HTBClient
	config Config
	err    error
}

type detailsMsg struct {
	machine *Machine
	err     error
}

type formMode int

const (
	noForm formMode = iota
	flagForm
	tokenForm
)

type model struct {
	client *HTBClient
	config Config

	width  int
	height int

	machineList list.Model
	vpnList     list.Model
	spinner     spinner.Model

	pane       pane
	active     *Machine
	selected   *Machine
	status     string
	lastError  string
	loading    bool
	formMode   formMode
	formInputs []textinput.Model
	formIndex  int
	tokenValue string
}

func newModel(client *HTBClient, config Config) model {
	delegate := list.NewDefaultDelegate()
	machines := list.New([]list.Item{}, delegate, 0, 0)
	machines.Title = "Machines"
	machines.SetShowHelp(false)
	machines.SetFilteringEnabled(true)

	vpns := list.New([]list.Item{}, delegate, 0, 0)
	vpns.Title = "VPN Servers"
	vpns.SetShowHelp(false)
	vpns.SetFilteringEnabled(true)

	spin := spinner.New()
	spin.Spinner = spinner.Dot

	m := model{
		client:      client,
		config:      config,
		machineList: machines,
		vpnList:     vpns,
		spinner:     spin,
		pane:        machinesPane,
		status:      "Loading HTB data...",
	}

	if client == nil {
		m.status = "Enter your HTB app token to load data."
		m.openTokenForm()
	}

	return m
}

func (m model) Init() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return tea.Batch(m.spinner.Tick, refreshAllCmd(m.client))
}

func refreshAllCmd(client *HTBClient) tea.Cmd {
	return tea.Batch(loadMachinesCmd(client), loadVPNCmd(client), loadActiveCmd(client))
}

func loadMachinesCmd(client *HTBClient) tea.Cmd {
	return func() tea.Msg {
		machines, err := client.ListMachines()
		return loadMachinesMsg{machines: machines, err: err}
	}
}

func loadVPNCmd(client *HTBClient) tea.Cmd {
	return func() tea.Msg {
		servers, err := client.ListVPNServers()
		return loadVPNMsg{servers: servers, err: err}
	}
}

func loadActiveCmd(client *HTBClient) tea.Cmd {
	return func() tea.Msg {
		machine, err := client.ActiveMachine()
		return activeMachineMsg{machine: machine, err: err}
	}
}

func spawnCmd(client *HTBClient, machineName string) tea.Cmd {
	return func() tea.Msg {
		machine, err := client.SpawnMachine(machineName)
		if err != nil {
			return spawnDoneMsg{err: err}
		}
		ready, err := client.WaitForMachineIP(machine.Name)
		return spawnDoneMsg{machine: ready, err: err}
	}
}

func switchVPNCmd(client *HTBClient, server VPNServer) tea.Cmd {
	return func() tea.Msg {
		err := client.SwitchVPN(server.ID)
		return switchVPNDoneMsg{server: server, err: err}
	}
}

func loadDetailsCmd(client *HTBClient, name string) tea.Cmd {
	return func() tea.Msg {
		machine, err := client.ResolveMachine(name)
		return detailsMsg{machine: machine, err: err}
	}
}

func submitFlagCmd(client *HTBClient, machineName, flag string, difficulty int) tea.Cmd {
	return func() tea.Msg {
		machine, err := client.ResolveMachine(machineName)
		if err != nil {
			return submitDoneMsg{err: err}
		}
		if machine == nil {
			return submitDoneMsg{err: fmt.Errorf("could not resolve machine %q", machineName)}
		}
		err = client.SubmitFlag(machine.ID, flag, difficulty)
		if err != nil {
			return submitDoneMsg{err: err}
		}
		return submitDoneMsg{message: fmt.Sprintf("Flag submitted for %s.", machine.Name)}
	}
}

func saveTokenCmd(baseDir, token string) tea.Cmd {
	return func() tea.Msg {
		config, err := saveToken(baseDir, token)
		if err != nil {
			return saveTokenDoneMsg{err: err}
		}

		client, err := NewHTBClient(config)
		if err != nil {
			return saveTokenDoneMsg{err: err}
		}

		return saveTokenDoneMsg{client: client, config: config}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	cmds := []tea.Cmd{cmd}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.machineList.SetSize(msg.Width-4, max(8, msg.Height-12))
		m.vpnList.SetSize(msg.Width-4, max(8, msg.Height-12))
	case loadMachinesMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		items := make([]list.Item, 0, len(msg.machines))
		for _, machine := range msg.machines {
			items = append(items, machineItem{machine: machine})
		}
		m.machineList.SetItems(items)
		m.status = fmt.Sprintf("Loaded %d machines.", len(msg.machines))
	case loadVPNMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		items := make([]list.Item, 0, len(msg.servers))
		for _, server := range msg.servers {
			items = append(items, vpnItem{server: server})
		}
		m.vpnList.SetItems(items)
	case activeMachineMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.active = msg.machine
	case detailsMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.selected = msg.machine
		if msg.machine != nil {
			m.status = fmt.Sprintf("Loaded details for %s.", msg.machine.Name)
		}
	case spawnDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.active = msg.machine
		m.selected = msg.machine
		m.status = fmt.Sprintf("%s is ready at %s.", msg.machine.Name, msg.machine.IP)
		cmds = append(cmds, refreshAllCmd(m.client))
	case switchVPNDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.status = fmt.Sprintf("Switched VPN to %s.", msg.server.Name)
		cmds = append(cmds, loadVPNCmd(m.client))
	case submitDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.status = msg.message
		m.formMode = noForm
		m.formInputs = nil
		cmds = append(cmds, refreshAllCmd(m.client))
	case saveTokenDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.client = msg.client
		m.config = msg.config
		m.formMode = noForm
		m.formInputs = nil
		m.status = "Token saved. Loading HTB data..."
		m.lastError = ""
		m.loading = true
		cmds = append(cmds, m.spinner.Tick, refreshAllCmd(m.client))
	case tea.KeyMsg:
		if m.formMode != noForm {
			return m.updateForm(msg)
		}

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			if m.pane == machinesPane {
				m.pane = vpnPane
			} else {
				m.pane = machinesPane
			}
		case "r":
			if m.client == nil {
				m.lastError = "Set your HTB token first with t."
				break
			}
			m.loading = true
			m.lastError = ""
			m.status = "Refreshing..."
			cmds = append(cmds, refreshAllCmd(m.client))
		case "a":
			if m.client == nil {
				m.lastError = "Set your HTB token first with t."
				break
			}
			m.loading = true
			m.lastError = ""
			cmds = append(cmds, loadActiveCmd(m.client))
		case "f":
			if m.client == nil {
				m.openTokenForm()
				m.lastError = "Set your HTB token first."
				break
			}
			m.openFlagForm()
		case "s":
			if m.client == nil {
				m.lastError = "Set your HTB token first with t."
				break
			}
			if m.pane == machinesPane {
				selected, ok := m.machineList.SelectedItem().(machineItem)
				if ok {
					m.loading = true
					m.lastError = ""
					m.status = fmt.Sprintf("Spawning %s...", selected.machine.Name)
					cmds = append(cmds, spawnCmd(m.client, selected.machine.Name))
				}
			}
		case "w":
			if m.client == nil {
				m.lastError = "Set your HTB token first with t."
				break
			}
			if m.pane == vpnPane {
				selected, ok := m.vpnList.SelectedItem().(vpnItem)
				if ok {
					m.loading = true
					m.lastError = ""
					m.status = fmt.Sprintf("Switching VPN to %s...", selected.server.Name)
					cmds = append(cmds, switchVPNCmd(m.client, selected.server))
				}
			}
		case "enter":
			if m.client == nil {
				m.openTokenForm()
				m.lastError = "Set your HTB token first."
				break
			}
			if m.pane == machinesPane {
				selected, ok := m.machineList.SelectedItem().(machineItem)
				if ok {
					m.loading = true
					m.lastError = ""
					cmds = append(cmds, loadDetailsCmd(m.client, selected.machine.Name))
				}
			}
		case "t":
			m.openTokenForm()
		}
	}

	if m.formMode == noForm {
		if m.pane == machinesPane {
			m.machineList, cmd = m.machineList.Update(msg)
			cmds = append(cmds, cmd)
		} else {
			m.vpnList, cmd = m.vpnList.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *model) openFlagForm() {
	m.formMode = flagForm
	m.formInputs = make([]textinput.Model, 3)
	placeholders := []string{"active", "HTB{flag}", strconv.Itoa(m.config.DefaultFlagDifficulty)}
	prompts := []string{"Machine: ", "Flag: ", "Difficulty: "}

	initialMachine := "active"
	if selected, ok := m.machineList.SelectedItem().(machineItem); ok {
		initialMachine = selected.machine.Name
	}

	values := []string{initialMachine, "", strconv.Itoa(m.config.DefaultFlagDifficulty)}

	for i := range m.formInputs {
		input := textinput.New()
		input.Prompt = prompts[i]
		input.Placeholder = placeholders[i]
		input.SetValue(values[i])
		if i == 1 {
			input.EchoMode = textinput.EchoPassword
			input.EchoCharacter = '*'
		}
		if i == 0 {
			input.Focus()
		}
		m.formInputs[i] = input
	}
	m.formIndex = 0
}

func (m *model) openTokenForm() {
	m.formMode = tokenForm
	m.formInputs = nil
	m.formIndex = 0
	m.tokenValue = ""
	m.status = "Token entry is open. Paste your token, then press Enter to save."
}

func (m model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.formMode == tokenForm {
		switch msg.Type {
		case tea.KeyEsc:
			if m.client == nil {
				m.lastError = "An HTB token is required before the TUI can load data."
				return m, nil
			}
			m.formMode = noForm
			m.tokenValue = ""
			return m, nil
		case tea.KeyEnter:
			token := strings.TrimSpace(m.tokenValue)
			if token == "" {
				m.lastError = "Token cannot be empty."
				return m, nil
			}
			m.loading = true
			m.lastError = ""
			m.status = "Saving token..."
			return m, saveTokenCmd(m.config.BaseDir, token)
		case tea.KeyBackspace, tea.KeyCtrlH:
			runes := []rune(m.tokenValue)
			if len(runes) > 0 {
				m.tokenValue = string(runes[:len(runes)-1])
			}
			return m, nil
		case tea.KeyCtrlU:
			m.tokenValue = ""
			return m, nil
		case tea.KeyRunes:
			m.tokenValue += string(msg.Runes)
			return m, nil
		case tea.KeySpace:
			m.tokenValue += " "
			return m, nil
		default:
			return m, nil
		}
	}

	switch msg.String() {
	case "esc":
		m.formMode = noForm
		m.formInputs = nil
		return m, nil
	case "tab", "shift+tab", "enter", "up", "down":
		key := msg.String()
		if key == "enter" && m.formIndex == len(m.formInputs)-1 {
			machine := strings.TrimSpace(m.formInputs[0].Value())
			flag := strings.TrimSpace(m.formInputs[1].Value())
			difficulty, err := strconv.Atoi(strings.TrimSpace(m.formInputs[2].Value()))
			if err != nil {
				m.lastError = "Difficulty must be a number."
				return m, nil
			}
			m.loading = true
			m.lastError = ""
			m.status = fmt.Sprintf("Submitting flag for %s...", machine)
			return m, submitFlagCmd(m.client, machine, flag, difficulty)
		}

		if key == "up" || key == "shift+tab" {
			m.formIndex--
		} else {
			m.formIndex++
		}
		if m.formIndex > len(m.formInputs)-1 {
			m.formIndex = 0
		}
		if m.formIndex < 0 {
			m.formIndex = len(m.formInputs) - 1
		}
		for i := range m.formInputs {
			if i == m.formIndex {
				m.formInputs[i].Focus()
			} else {
				m.formInputs[i].Blur()
			}
		}
		return m, nil
	}

	cmds := make([]tea.Cmd, len(m.formInputs))
	for i := range m.formInputs {
		m.formInputs[i], cmds[i] = m.formInputs[i].Update(msg)
	}

	teaCmds := make([]tea.Cmd, 0, len(cmds))
	for _, c := range cmds {
		teaCmds = append(teaCmds, c)
	}
	return m, tea.Batch(teaCmds...)
}

func (m model) View() string {
	if m.width == 0 {
		return "\n  Loading..."
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	activePaneStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	boxStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	noticeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	tokenBoxStyle := lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("42")).Padding(1, 2)

	header := titleStyle.Render("HTB TUI")
	if m.loading {
		header += " " + m.spinner.View()
	}

	if m.client == nil {
		count := len([]rune(m.tokenValue))
		tokenStatus := mutedStyle.Render("No characters entered yet.")
		if count > 0 {
			tokenStatus = noticeStyle.Render(fmt.Sprintf("Input received: %d characters", count))
		}
		tokenField := "HTB Token: |"
		if count > 0 {
			tokenField = "HTB Token: " + strings.Repeat("*", count) + "|"
		}
		formLines := []string{
			noticeStyle.Render("HTB token required"),
			"",
			"This TUI needs your Hack The Box app token before it can load machines or VPN servers.",
			"Paste the token below and press Enter to save it to htbTUI/.env.",
			"",
			tokenField,
			"",
			tokenStatus,
			mutedStyle.Render("Backspace deletes. Ctrl+U clears the whole token."),
			mutedStyle.Render("Find it in Hack The Box account settings or copy an existing HTB_APP_TOKEN value."),
			mutedStyle.Render("Press q to quit."),
		}

		body := []string{
			header,
			tokenBoxStyle.Width(min(90, max(40, m.width-4))).Render(strings.Join(formLines, "\n")),
		}
		if m.status != "" {
			body = append(body, "Status: "+m.status)
		}
		if m.lastError != "" {
			body = append(body, errorStyle.Render("Error: "+m.lastError))
		}
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(body, "\n\n"))
	}

	panes := []string{"Machines", "VPN"}
	if m.pane == machinesPane {
		panes[0] = activePaneStyle.Render("[Machines]")
		panes[1] = mutedStyle.Render("VPN")
	} else {
		panes[0] = mutedStyle.Render("Machines")
		panes[1] = activePaneStyle.Render("[VPN]")
	}

	activeSummary := "No active machine."
	if m.active != nil {
		activeSummary = fmt.Sprintf("Active: %s (#%d) | %s | IP %s", m.active.Name, m.active.ID, m.active.Difficulty, firstNonEmpty(m.active.IP, "pending"))
	} else if m.client == nil {
		activeSummary = "No token configured yet."
	}

	details := "Select a machine and press enter for details."
	if m.selected != nil {
		details = fmt.Sprintf(
			"%s (#%d)\nOS: %s\nDifficulty: %s\nIP: %s\nOwned: user=%t root=%t",
			m.selected.Name, m.selected.ID, firstNonEmpty(m.selected.OS, "Unknown"), firstNonEmpty(m.selected.Difficulty, "Unknown"), firstNonEmpty(m.selected.IP, "pending"), m.selected.UserOwned, m.selected.RootOwned,
		)
	}

	currentList := m.machineList.View()
	if m.pane == vpnPane {
		currentList = m.vpnList.View()
	}

	body := []string{
		header,
		strings.Join(panes, " | "),
		activeSummary,
		boxStyle.Width(m.width - 4).Render(currentList),
		boxStyle.Width(m.width - 4).Render(details),
		mutedStyle.Render("Keys: tab switch pane | t edit token | r refresh | s spawn machine | w switch VPN | f submit flag | enter details | q quit"),
	}

	if m.status != "" {
		body = append(body, "Status: "+m.status)
	}
	if m.lastError != "" {
		body = append(body, errorStyle.Render("Error: "+m.lastError))
	}
	if m.formMode == flagForm {
		formLines := []string{"Submit Flag"}
		for _, input := range m.formInputs {
			formLines = append(formLines, input.View())
		}
		formLines = append(formLines, mutedStyle.Render("Enter submits on the last field. Esc cancels."))
		body = append(body, boxStyle.Width(min(80, m.width-4)).Render(strings.Join(formLines, "\n")))
	} else if m.formMode == tokenForm {
		count := len([]rune(m.tokenValue))
		tokenField := "HTB Token: |"
		if count > 0 {
			tokenField = "HTB Token: " + strings.Repeat("*", count) + "|"
		}
		formLines := []string{"Set HTB Token", tokenField}
		if count == 0 {
			formLines = append(formLines, mutedStyle.Render("No characters entered yet."))
		} else {
			formLines = append(formLines, noticeStyle.Render(fmt.Sprintf("Input received: %d characters", count)))
		}
		formLines = append(formLines, mutedStyle.Render("Backspace deletes. Ctrl+U clears the whole token."))
		formLines = append(formLines, mutedStyle.Render("Paste the token and press Enter to save it to htbTUI/.env."))
		if m.client != nil {
			formLines = append(formLines, mutedStyle.Render("Esc cancels."))
		}
		body = append(body, tokenBoxStyle.Width(min(80, m.width-4)).Render(strings.Join(formLines, "\n")))
	}

	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(body, "\n"))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	baseDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	config := loadConfig(baseDir)
	var client *HTBClient
	if config.Token != "" {
		client, err = NewHTBClient(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	program := tea.NewProgram(newModel(client, config), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
