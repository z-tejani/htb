package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type HTBClient struct {
	httpClient *http.Client
	config     Config
}

type Machine struct {
	ID            int
	Name          string
	IP            string
	OS            string
	Difficulty    string
	Points        int
	Stars         float64
	Active        bool
	Spawning      bool
	Retired       bool
	StartingPoint bool
	StartingTier  string
	UserOwned     bool
	RootOwned     bool
	Tier          int
	Release       string
}

type VPNServer struct {
	ID             int
	Name           string
	Location       string
	CurrentClients int
	Full           bool
	VIP            bool
	Assigned       bool
}

type apiError struct {
	Message string `json:"message"`
}

type stringOrNumber string

func (s *stringOrNumber) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}

	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = stringOrNumber(str)
		return nil
	}

	var num json.Number
	if err := json.Unmarshal(data, &num); err == nil {
		*s = stringOrNumber(num.String())
		return nil
	}

	return fmt.Errorf("unsupported stringOrNumber value: %s", string(data))
}

func NewHTBClient(config Config) (*HTBClient, error) {
	if config.Token == "" {
		return nil, fmt.Errorf("missing HTB_APP_TOKEN; add it to htbTUI/.env, HTB/config/replay.env, or your shell environment")
	}

	return &HTBClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		config:     config,
	}, nil
}

func (c *HTBClient) request(method, endpoint string, body any) ([]byte, error) {
	payload, _, err := c.requestWithHeaders(method, endpoint, body)
	return payload, err
}

func (c *HTBClient) requestWithHeaders(method, endpoint string, body any) ([]byte, http.Header, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, c.config.APIBase+endpoint, reader)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.config.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr apiError
		if err := json.Unmarshal(payload, &apiErr); err == nil && apiErr.Message != "" {
			return nil, nil, errors.New(apiErr.Message)
		}
		return nil, nil, fmt.Errorf("%s", resp.Status)
	}

	return payload, resp.Header.Clone(), nil
}

func normalizeMachine(data rawMachine) Machine {
	info := data.Info
	if info.ID == 0 && data.ID != 0 {
		info = data.machineInfo
	}

	return Machine{
		ID:            info.ID,
		Name:          info.Name,
		IP:            info.IP,
		OS:            info.OS,
		Difficulty:    firstNonEmpty(info.DifficultyText, string(info.Difficulty)),
		Points:        info.Points,
		Stars:         info.Stars,
		Active:        info.PlayInfo.IsActive || info.Active,
		Spawning:      info.IsSpawning,
		Retired:       info.Retired,
		StartingPoint: info.SPFlag == 1,
		UserOwned:     info.AuthUserInUserOwns,
		RootOwned:     info.AuthUserInRootOwns,
		Tier:          info.Tier,
		Release:       info.Release,
	}
}

func normalizeStartingPointMachine(tierName string, data rawStartingPointMachine) Machine {
	return Machine{
		ID:            data.ID,
		Name:          data.Name,
		OS:            data.OS,
		Difficulty:    data.DifficultyText,
		Points:        data.StaticPoints,
		StartingPoint: data.SPFlag == 1,
		StartingTier:  tierName,
		UserOwned:     data.UserOwn,
		RootOwned:     data.RootOwn,
	}
}

func (c *HTBClient) ActiveMachine() (*Machine, error) {
	payload, err := c.request(http.MethodGet, "/machine/active", nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Info *machineInfo `json:"info"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, err
	}
	if response.Info == nil || response.Info.Name == "" {
		return nil, nil
	}

	machine, err := c.ResolveMachine(response.Info.Name)
	if err == nil && machine != nil {
		return machine, nil
	}

	fallback := normalizeMachine(rawMachine{Info: *response.Info})
	return &fallback, nil
}

func (c *HTBClient) MachineProfile(name string) (*Machine, error) {
	payload, err := c.request(http.MethodGet, "/machine/profile/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, err
	}

	var response rawMachine
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, err
	}

	machine := normalizeMachine(response)
	if machine.ID == 0 {
		return nil, nil
	}

	return &machine, nil
}

func (c *HTBClient) ListMachines() ([]Machine, error) {
	payload, err := c.request(http.MethodGet, "/machine/paginated?per_page=100", nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Data []rawMachine `json:"data"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, err
	}

	machines := make([]Machine, 0, len(response.Data))
	for _, item := range response.Data {
		machine := normalizeMachine(item)
		if machine.ID != 0 {
			machines = append(machines, machine)
		}
	}

	sort.Slice(machines, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339Nano, machines[i].Release)
		right, _ := time.Parse(time.RFC3339Nano, machines[j].Release)
		return right.Before(left) == false
	})

	return machines, nil
}

func (c *HTBClient) ListRetiredMachines() ([]Machine, error) {
	type retiredResponse struct {
		Data []rawMachine `json:"data"`
		Meta struct {
			LastPage int `json:"last_page"`
		} `json:"meta"`
	}

	page := 1
	perPage := 100
	machines := []Machine{}

	for {
		payload, err := c.request(http.MethodGet, fmt.Sprintf("/machine/list/retired/paginated?page=%d&per_page=%d", page, perPage), nil)
		if err != nil {
			return nil, err
		}

		var response retiredResponse
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, err
		}

		for _, item := range response.Data {
			machine := normalizeMachine(item)
			if machine.ID == 0 {
				continue
			}
			machine.Retired = true
			machines = append(machines, machine)
		}

		if page >= response.Meta.LastPage || response.Meta.LastPage == 0 {
			break
		}
		page++
	}

	sort.Slice(machines, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339Nano, machines[i].Release)
		right, _ := time.Parse(time.RFC3339Nano, machines[j].Release)
		return right.Before(left) == false
	})

	return machines, nil
}

func (c *HTBClient) ListStartingPointMachines() ([]Machine, error) {
	type tierResponse struct {
		Data struct {
			Name     string                    `json:"name"`
			Machines []rawStartingPointMachine `json:"machines"`
		} `json:"data"`
	}

	tiers := []int{1, 2, 3}
	machines := []Machine{}

	for _, tier := range tiers {
		payload, err := c.request(http.MethodGet, fmt.Sprintf("/sp/tier/%d", tier), nil)
		if err != nil {
			return nil, err
		}

		var response tierResponse
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, err
		}

		for _, item := range response.Data.Machines {
			machine := normalizeStartingPointMachine(response.Data.Name, item)
			if machine.ID != 0 {
				machines = append(machines, machine)
			}
		}
	}

	return machines, nil
}

func (c *HTBClient) ResolveMachine(query string) (*Machine, error) {
	query = strings.TrimSpace(query)
	if query == "" || strings.EqualFold(query, "active") {
		return c.ActiveMachine()
	}

	if id, err := strconv.Atoi(query); err == nil {
		machines, err := c.ListMachines()
		if err != nil {
			return nil, err
		}
		for _, machine := range machines {
			if machine.ID == id {
				copy := machine
				return &copy, nil
			}
		}
	}

	profile, err := c.MachineProfile(query)
	if err == nil && profile != nil {
		return profile, nil
	}

	machines, listErr := c.ListMachines()
	if listErr != nil {
		if err != nil {
			return nil, err
		}
		return nil, listErr
	}

	lowered := strings.ToLower(query)
	for _, machine := range machines {
		if strings.EqualFold(machine.Name, query) {
			copy := machine
			return &copy, nil
		}
	}
	for _, machine := range machines {
		if strings.Contains(strings.ToLower(machine.Name), lowered) {
			copy := machine
			return &copy, nil
		}
	}

	return nil, nil
}

func (c *HTBClient) ListVPNServers() ([]VPNServer, error) {
	payload, err := c.request(http.MethodGet, "/connections/servers?product=labs", nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Data struct {
			Assigned rawVPNServer `json:"assigned"`
			Options  map[string]map[string]struct {
				Servers map[string]rawVPNServer `json:"servers"`
			} `json:"options"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, err
	}

	servers := []VPNServer{}
	for _, region := range response.Data.Options {
		for _, group := range region {
			for _, server := range group.Servers {
				servers = append(servers, VPNServer{
					ID:             server.ID,
					Name:           firstNonEmpty(server.FriendlyName, server.Name),
					Location:       server.Location,
					CurrentClients: server.CurrentClients,
					Full:           server.Full,
					VIP:            strings.Contains(strings.ToLower(firstNonEmpty(server.FriendlyName, server.Name)), "vip"),
					Assigned:       server.ID == response.Data.Assigned.ID,
				})
			}
		}
	}

	sort.Slice(servers, func(i, j int) bool {
		if servers[i].Assigned != servers[j].Assigned {
			return servers[i].Assigned
		}
		if servers[i].Location != servers[j].Location {
			return servers[i].Location < servers[j].Location
		}
		return servers[i].Name < servers[j].Name
	})

	return servers, nil
}

func (c *HTBClient) ResolveVPNServer(query string) (*VPNServer, error) {
	servers, err := c.ListVPNServers()
	if err != nil {
		return nil, err
	}

	if id, err := strconv.Atoi(strings.TrimSpace(query)); err == nil {
		for _, server := range servers {
			if server.ID == id {
				copy := server
				return &copy, nil
			}
		}
	}

	lowered := strings.ToLower(strings.TrimSpace(query))
	for _, server := range servers {
		if strings.EqualFold(server.Name, query) {
			copy := server
			return &copy, nil
		}
	}
	for _, server := range servers {
		if strings.Contains(strings.ToLower(server.Name), lowered) {
			copy := server
			return &copy, nil
		}
	}

	return nil, nil
}

func (c *HTBClient) SwitchVPN(id int) error {
	_, err := c.request(http.MethodPost, fmt.Sprintf("/connections/servers/switch/%d?product=labs", id), nil)
	return err
}

func (c *HTBClient) DownloadVPNProfile(serverID int) ([]byte, string, error) {
	payload, headers, err := c.requestWithHeaders(http.MethodGet, fmt.Sprintf("/access/ovpnfile/%d/0", serverID), nil)
	if err != nil {
		return nil, "", err
	}

	filename := fmt.Sprintf("htb-vpn-%d.ovpn", serverID)
	if disposition := headers.Get("Content-Disposition"); disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil && params["filename"] != "" {
			filename = params["filename"]
		}
	}

	return payload, filename, nil
}

func (c *HTBClient) SwitchPreferredVPN() error {
	query := firstNonEmpty(c.config.PreferredVPNID, c.config.PreferredVPNName)
	if query == "" {
		return nil
	}

	server, err := c.ResolveVPNServer(query)
	if err != nil {
		return err
	}
	if server == nil {
		return fmt.Errorf("preferred VPN server %q could not be resolved", query)
	}

	return c.SwitchVPN(server.ID)
}

func (c *HTBClient) SpawnMachine(query string) (*Machine, error) {
	machine, err := c.ResolveMachine(query)
	if err != nil {
		return nil, err
	}
	if machine == nil {
		return nil, fmt.Errorf("unable to resolve a machine from %q", query)
	}

	if err := c.SwitchPreferredVPN(); err != nil {
		return nil, err
	}

	if machine.StartingPoint {
		_, err = c.request(http.MethodPost, "/vm/spawn", map[string]int{"machine_id": machine.ID})
	} else {
		_, err = c.request(http.MethodPost, fmt.Sprintf("/machine/play/%d", machine.ID), nil)
	}
	if err != nil {
		return nil, err
	}

	return machine, nil
}

func (c *HTBClient) WaitForMachineIP(query string) (*Machine, error) {
	for attempt := 0; attempt < c.config.WaitAttempts; attempt++ {
		machine, err := c.ResolveMachine(query)
		if err != nil {
			return nil, err
		}
		if machine != nil && machine.IP != "" {
			return machine, nil
		}
		time.Sleep(time.Duration(c.config.WaitIntervalSeconds) * time.Second)
	}

	return nil, fmt.Errorf("timed out waiting for %q to receive an IP", query)
}

func (c *HTBClient) SubmitFlag(machineID int, flag string, difficulty int) error {
	_, err := c.request(http.MethodPost, "/machine/own", map[string]any{
		"id":         machineID,
		"flag":       flag,
		"difficulty": difficulty,
	})
	return err
}

type rawMachine struct {
	Info machineInfo `json:"info"`
	machineInfo
}

type machineInfo struct {
	ID                 int            `json:"id"`
	Name               string         `json:"name"`
	IP                 string         `json:"ip"`
	OS                 string         `json:"os"`
	DifficultyText     string         `json:"difficultyText"`
	Difficulty         stringOrNumber `json:"difficulty"`
	Points             int            `json:"points"`
	Stars              float64        `json:"stars"`
	Active             bool           `json:"active"`
	IsSpawning         bool           `json:"isSpawning"`
	Retired            bool           `json:"retired"`
	SPFlag             int            `json:"sp_flag"`
	AuthUserInUserOwns bool           `json:"authUserInUserOwns"`
	AuthUserInRootOwns bool           `json:"authUserInRootOwns"`
	Tier               int            `json:"tier"`
	Release            string         `json:"release"`
	PlayInfo           struct {
		IsActive bool `json:"isActive"`
	} `json:"playInfo"`
}

type rawVPNServer struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	FriendlyName   string `json:"friendly_name"`
	Location       string `json:"location"`
	CurrentClients int    `json:"current_clients"`
	Full           bool   `json:"full"`
}

type rawStartingPointMachine struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	OS             string `json:"os"`
	DifficultyText string `json:"difficultyText"`
	StaticPoints   int    `json:"static_points"`
	SPFlag         int    `json:"sp_flag"`
	UserOwn        bool   `json:"userOwn"`
	RootOwn        bool   `json:"rootOwn"`
}
