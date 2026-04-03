package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
type machineSection struct {
	title    string
	machines []Machine
}

func (m machineItem) Title() string {
	return fmt.Sprintf("%s (#%d)", m.machine.Name, m.machine.ID)
}

func (m machineItem) Description() string {
	parts := []string{m.machine.OS, m.machine.Difficulty}
	if m.machine.StartingPoint && m.machine.StartingTier != "" {
		parts = append(parts, m.machine.StartingTier)
	}
	if m.machine.IP != "" {
		parts = append(parts, "IP "+m.machine.IP)
	}
	if m.machine.Retired {
		parts = append(parts, "retired")
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

type loadMachineCatalogMsg struct {
	sections []machineSection
	err      error
}

type loadVPNMsg struct {
	servers []VPNServer
	err     error
}

type loadVPNRuntimeMsg struct {
	status VPNRuntimeStatus
	err    error
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

type vpnDownloadDoneMsg struct {
	server VPNServer
	path   string
	err    error
}

type vpnConnectDoneMsg struct {
	server VPNServer
	status VPNRuntimeStatus
	err    error
}

type vpnDisconnectDoneMsg struct {
	status VPNRuntimeStatus
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
	vpnMgr VPNManager
	config Config

	width  int
	height int

	machineList list.Model
	vpnList     list.Model
	spinner     spinner.Model

	pane        pane
	active      *Machine
	selected    *Machine
	status      string
	lastError   string
	loading     bool
	sections    []machineSection
	sectionIdx  int
	searchMode  bool
	searchQuery string
	vpnStatus   VPNRuntimeStatus
	formMode    formMode
	formInputs  []textinput.Model
	formIndex   int
	tokenValue  string
}

func newModel(client *HTBClient, vpnMgr VPNManager, config Config) model {
	delegate := list.NewDefaultDelegate()
	machines := list.New([]list.Item{}, delegate, 0, 0)
	machines.Title = "Machines"
	machines.SetShowHelp(false)
	machines.SetFilteringEnabled(false)

	vpns := list.New([]list.Item{}, delegate, 0, 0)
	vpns.Title = "VPN Servers"
	vpns.SetShowHelp(false)
	vpns.SetFilteringEnabled(true)

	spin := spinner.New()
	spin.Spinner = spinner.Dot

	m := model{
		client:      client,
		vpnMgr:      vpnMgr,
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
		return loadVPNRuntimeCmd(m.vpnMgr)
	}
	return tea.Batch(m.spinner.Tick, refreshAllCmd(m.client, m.vpnMgr))
}

func refreshAllCmd(client *HTBClient, vpnMgr VPNManager) tea.Cmd {
	return tea.Batch(loadMachineCatalogCmd(client), loadVPNCmd(client), loadActiveCmd(client), loadVPNRuntimeCmd(vpnMgr))
}

func loadMachineCatalogCmd(client *HTBClient) tea.Cmd {
	return func() tea.Msg {
		currentSeason, err := client.ListMachines()
		if err != nil {
			return loadMachineCatalogMsg{err: err}
		}

		retired, err := client.ListRetiredMachines()
		if err != nil {
			return loadMachineCatalogMsg{err: err}
		}

		startingPoint, err := client.ListStartingPointMachines()
		if err != nil {
			return loadMachineCatalogMsg{err: err}
		}

		allMachines := mergeMachines(currentSeason, retired, startingPoint)
		sections := []machineSection{
			{title: "Current Season", machines: currentSeason},
			{title: "Retired", machines: retired},
			{title: "Starting Point", machines: startingPoint},
			{title: "All Machines", machines: allMachines},
		}

		return loadMachineCatalogMsg{sections: sections}
	}
}

func loadVPNCmd(client *HTBClient) tea.Cmd {
	return func() tea.Msg {
		servers, err := client.ListVPNServers()
		return loadVPNMsg{servers: servers, err: err}
	}
}

func loadVPNRuntimeCmd(vpnMgr VPNManager) tea.Cmd {
	return func() tea.Msg {
		status, err := vpnMgr.Status()
		return loadVPNRuntimeMsg{status: status, err: err}
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

func downloadVPNCmd(client *HTBClient, vpnMgr VPNManager, server VPNServer) tea.Cmd {
	return func() tea.Msg {
		path, err := vpnMgr.DownloadConfig(client, server)
		return vpnDownloadDoneMsg{server: server, path: path, err: err}
	}
}

func connectVPNCmd(client *HTBClient, vpnMgr VPNManager, server VPNServer) tea.Cmd {
	return func() tea.Msg {
		status, err := vpnMgr.Connect(client, server)
		return vpnConnectDoneMsg{server: server, status: status, err: err}
	}
}

func disconnectVPNCmd(vpnMgr VPNManager) tea.Cmd {
	return func() tea.Msg {
		status, err := vpnMgr.Disconnect()
		return vpnDisconnectDoneMsg{status: status, err: err}
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
	case loadMachineCatalogMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.sections = msg.sections
		if m.sectionIdx >= len(m.sections) {
			m.sectionIdx = 0
		}
		m.syncMachineList()
		total := 0
		for _, section := range msg.sections {
			if section.title == "All Machines" {
				total = len(section.machines)
				break
			}
		}
		m.status = fmt.Sprintf("Loaded %d machines across %d categories.", total, len(msg.sections))
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
	case loadVPNRuntimeMsg:
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.vpnStatus = msg.status
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
		cmds = append(cmds, refreshAllCmd(m.client, m.vpnMgr))
	case switchVPNDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.status = fmt.Sprintf("Switched VPN to %s.", msg.server.Name)
		cmds = append(cmds, loadVPNCmd(m.client))
	case vpnDownloadDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.status = fmt.Sprintf("Downloaded %s VPN config to %s.", msg.server.Name, msg.path)
		cmds = append(cmds, loadVPNCmd(m.client), loadVPNRuntimeCmd(m.vpnMgr))
	case vpnConnectDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.vpnStatus = msg.status
		m.status = fmt.Sprintf("Connected local OpenVPN session to %s.", msg.server.Name)
		cmds = append(cmds, loadVPNCmd(m.client), loadVPNRuntimeCmd(m.vpnMgr))
	case vpnDisconnectDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.vpnStatus = msg.status
		m.status = "Disconnected local VPN session."
		cmds = append(cmds, loadVPNRuntimeCmd(m.vpnMgr))
	case submitDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			break
		}
		m.status = msg.message
		m.formMode = noForm
		m.formInputs = nil
		cmds = append(cmds, refreshAllCmd(m.client, m.vpnMgr))
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
		cmds = append(cmds, m.spinner.Tick, refreshAllCmd(m.client, m.vpnMgr))
	case tea.KeyMsg:
		if m.formMode != noForm {
			return m.updateForm(msg)
		}
		if m.searchMode {
			return m.updateSearch(msg)
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
			cmds = append(cmds, refreshAllCmd(m.client, m.vpnMgr))
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
		case "d":
			if m.client == nil {
				m.lastError = "Set your HTB token first with t."
				break
			}
			if m.pane == vpnPane {
				selected, ok := m.vpnList.SelectedItem().(vpnItem)
				if ok {
					m.loading = true
					m.lastError = ""
					m.status = fmt.Sprintf("Downloading VPN config for %s...", selected.server.Name)
					cmds = append(cmds, downloadVPNCmd(m.client, m.vpnMgr, selected.server))
				}
			}
		case "c":
			if m.client == nil {
				m.lastError = "Set your HTB token first with t."
				break
			}
			if m.pane == vpnPane {
				selected, ok := m.vpnList.SelectedItem().(vpnItem)
				if ok {
					m.loading = true
					m.lastError = ""
					m.status = fmt.Sprintf("Connecting to %s...", selected.server.Name)
					cmds = append(cmds, connectVPNCmd(m.client, m.vpnMgr, selected.server))
				}
			}
		case "x":
			if m.pane == vpnPane {
				m.loading = true
				m.lastError = ""
				m.status = "Disconnecting local VPN session..."
				cmds = append(cmds, disconnectVPNCmd(m.vpnMgr))
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
			} else if m.pane == vpnPane {
				selected, ok := m.vpnList.SelectedItem().(vpnItem)
				if ok {
					m.loading = true
					m.lastError = ""
					m.status = fmt.Sprintf("Connecting to %s...", selected.server.Name)
					cmds = append(cmds, connectVPNCmd(m.client, m.vpnMgr, selected.server))
				}
			}
		case "t":
			m.openTokenForm()
		case "[":
			if m.pane == machinesPane && len(m.sections) > 0 {
				m.sectionIdx--
				if m.sectionIdx < 0 {
					m.sectionIdx = len(m.sections) - 1
				}
				m.syncMachineList()
			}
		case "]":
			if m.pane == machinesPane && len(m.sections) > 0 {
				m.sectionIdx = (m.sectionIdx + 1) % len(m.sections)
				m.syncMachineList()
			}
		case "/":
			if m.pane == machinesPane {
				m.searchMode = true
				m.status = "Search mode: type a machine name, Enter to finish, Esc to cancel."
			}
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

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.searchMode = false
		m.status = "Search cancelled."
		return m, nil
	case tea.KeyEnter:
		m.searchMode = false
		m.syncMachineList()
		m.status = fmt.Sprintf("Search applied: %q.", strings.TrimSpace(m.searchQuery))
		if strings.TrimSpace(m.searchQuery) == "" {
			m.status = "Search cleared."
		}
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		runes := []rune(m.searchQuery)
		if len(runes) > 0 {
			m.searchQuery = string(runes[:len(runes)-1])
			m.syncMachineList()
		}
		return m, nil
	case tea.KeyCtrlU:
		m.searchQuery = ""
		m.syncMachineList()
		return m, nil
	case tea.KeyRunes:
		m.searchQuery += string(msg.Runes)
		m.syncMachineList()
		return m, nil
	case tea.KeySpace:
		m.searchQuery += " "
		m.syncMachineList()
		return m, nil
	default:
		return m, nil
	}
}

func (m *model) syncMachineList() {
	machines := m.filteredMachines()
	items := make([]list.Item, 0, len(machines))
	for _, machine := range machines {
		items = append(items, machineItem{machine: machine})
	}
	m.machineList.SetItems(items)
	if len(m.sections) > 0 {
		m.machineList.Title = m.sections[m.sectionIdx].title
	}
}

func (m model) filteredMachines() []Machine {
	if len(m.sections) == 0 {
		return nil
	}

	machines := m.sections[m.sectionIdx].machines
	query := strings.ToLower(strings.TrimSpace(m.searchQuery))
	if query == "" {
		return machines
	}

	filtered := make([]Machine, 0, len(machines))
	for _, machine := range machines {
		if strings.Contains(strings.ToLower(machine.Name), query) {
			filtered = append(filtered, machine)
		}
	}

	return filtered
}

func mergeMachines(groups ...[]Machine) []Machine {
	merged := make([]Machine, 0)
	seen := make(map[int]struct{})

	for _, group := range groups {
		for _, machine := range group {
			if _, ok := seen[machine.ID]; ok {
				continue
			}
			seen[machine.ID] = struct{}{}
			merged = append(merged, machine)
		}
	}

	sort.Slice(merged, func(i, j int) bool {
		return strings.ToLower(merged[i].Name) < strings.ToLower(merged[j].Name)
	})

	return merged
}

func formatVPNStatus(status VPNRuntimeStatus) string {
	if !status.Available {
		return "Local VPN: openvpn not installed."
	}
	if status.Connected {
		return fmt.Sprintf("Local VPN: connected to %s | pid %d", firstNonEmpty(status.ServerName, "unknown"), status.PID)
	}
	if status.ConfigPath != "" {
		return fmt.Sprintf("Local VPN: disconnected | last config %s", status.ConfigPath)
	}
	return "Local VPN: disconnected."
}

func formatVPNDetails(server *VPNServer, status VPNRuntimeStatus) string {
	lines := []string{formatVPNStatus(status)}
	if server == nil {
		lines = append(lines, "Select a VPN server to switch, download, or connect.")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, fmt.Sprintf("%s (#%d)", server.Name, server.ID))
	lines = append(lines, fmt.Sprintf("Location: %s", firstNonEmpty(server.Location, "Unknown")))
	lines = append(lines, fmt.Sprintf("Clients: %d", server.CurrentClients))
	lines = append(lines, fmt.Sprintf("Assigned in HTB: %t", server.Assigned))
	if status.ConfigPath != "" {
		lines = append(lines, "Config: "+status.ConfigPath)
	}
	if status.LogPath != "" {
		lines = append(lines, "Log: "+status.LogPath)
	}
	return strings.Join(lines, "\n")
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
	if m.pane == vpnPane {
		activeSummary = formatVPNStatus(m.vpnStatus)
	}

	details := "Select a machine and press enter for details."
	if m.selected != nil {
		details = fmt.Sprintf(
			"%s (#%d)\nOS: %s\nDifficulty: %s\nIP: %s\nOwned: user=%t root=%t",
			m.selected.Name, m.selected.ID, firstNonEmpty(m.selected.OS, "Unknown"), firstNonEmpty(m.selected.Difficulty, "Unknown"), firstNonEmpty(m.selected.IP, "pending"), m.selected.UserOwned, m.selected.RootOwned,
		)
	}
	if m.pane == vpnPane {
		selected, _ := m.vpnList.SelectedItem().(vpnItem)
		details = formatVPNDetails(&selected.server, m.vpnStatus)
		if selected.server.ID == 0 {
			details = formatVPNDetails(nil, m.vpnStatus)
		}
	}

	currentList := m.machineList.View()
	if m.pane == vpnPane {
		currentList = m.vpnList.View()
	}

	searchLine := mutedStyle.Render("Search: / to search by name")
	if m.searchMode {
		searchLine = activePaneStyle.Render("Search: " + m.searchQuery + "|")
	} else if strings.TrimSpace(m.searchQuery) != "" {
		searchLine = activePaneStyle.Render("Search: " + m.searchQuery)
	}
	if m.pane == vpnPane {
		searchLine = mutedStyle.Render("VPN Keys: enter/c connect | d download | x disconnect | w switch in HTB")
	}

	categoryLine := mutedStyle.Render("No categories loaded.")
	if len(m.sections) > 0 {
		labels := make([]string, 0, len(m.sections))
		for i, section := range m.sections {
			label := fmt.Sprintf("%s (%d)", section.title, len(section.machines))
			if i == m.sectionIdx {
				label = activePaneStyle.Render("[" + label + "]")
			} else {
				label = mutedStyle.Render(label)
			}
			labels = append(labels, label)
		}
		categoryLine = strings.Join(labels, " | ")
	}
	if m.pane == vpnPane {
		categoryLine = mutedStyle.Render("VPN Servers")
	}

	body := []string{
		header,
		strings.Join(panes, " | "),
		activeSummary,
		categoryLine,
		searchLine,
		boxStyle.Width(m.width - 4).Render(currentList),
		boxStyle.Width(m.width - 4).Render(details),
		mutedStyle.Render("Keys: tab switch pane | [ ] categories | / search | t edit token | r refresh | s spawn machine | w switch VPN | d download VPN | c connect | x disconnect | f submit flag | enter action | q quit"),
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
	vpnMgr := NewVPNManager(baseDir)
	var client *HTBClient
	if config.Token != "" {
		client, err = NewHTBClient(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	program := tea.NewProgram(newModel(client, vpnMgr, config), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
