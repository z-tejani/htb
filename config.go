package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	BaseDir               string
	EnvFile               string
	APIBase               string
	Token                 string
	WaitIntervalSeconds   int
	WaitAttempts          int
	PreferredVPNID        string
	PreferredVPNName      string
	DefaultFlagDifficulty int
}

var shellDefaultPattern = regexp.MustCompile(`^\$\{([A-Z0-9_]+):-([^}]*)\}$`)

func loadConfig(baseDir string) Config {
	envFiles := []string{
		filepath.Join(baseDir, "..", "HTB", "config", "replay.env"),
		filepath.Join(baseDir, ".env"),
	}

	merged := map[string]string{}

	for _, envFile := range envFiles {
		fileVars := parseEnvFile(envFile, merged)
		for key, value := range fileVars {
			merged[key] = value
		}
	}

	for _, pair := range os.Environ() {
		key, value, ok := strings.Cut(pair, "=")
		if ok {
			merged[key] = value
		}
	}

	return Config{
		BaseDir:               baseDir,
		EnvFile:               filepath.Join(baseDir, ".env"),
		APIBase:               firstNonEmpty(merged["HTB_API_BASE"], "https://labs.hackthebox.com/api/v4"),
		Token:                 merged["HTB_APP_TOKEN"],
		WaitIntervalSeconds:   parseIntDefault(merged["HTB_WAIT_INTERVAL"], 5),
		WaitAttempts:          parseIntDefault(merged["HTB_WAIT_ATTEMPTS"], 24),
		PreferredVPNID:        merged["HTB_VPN_SERVER_ID"],
		PreferredVPNName:      merged["HTB_VPN_SERVER_NAME"],
		DefaultFlagDifficulty: parseIntDefault(merged["HTB_DEFAULT_FLAG_DIFFICULTY"], 0),
	}
}

func parseEnvFile(path string, merged map[string]string) map[string]string {
	file, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer file.Close()

	parsed := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		value = strings.TrimSpace(stripQuotes(value))
		matches := shellDefaultPattern.FindStringSubmatch(value)
		if len(matches) == 3 {
			envKey := matches[1]
			fallback := matches[2]
			if current := firstNonEmpty(parsed[envKey], merged[envKey], os.Getenv(envKey)); current != "" {
				value = current
			} else {
				value = fallback
			}
		}

		parsed[key] = value
	}

	return parsed
}

func saveToken(baseDir, token string) (Config, error) {
	envFile := filepath.Join(baseDir, ".env")
	line := "HTB_APP_TOKEN=" + quoteEnvValue(token)

	content, err := os.ReadFile(envFile)
	if err != nil && !os.IsNotExist(err) {
		return Config{}, err
	}

	lines := []string{}
	replaced := false
	if len(content) > 0 {
		for _, existing := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
			trimmed := strings.TrimSpace(existing)
			if strings.HasPrefix(trimmed, "HTB_APP_TOKEN=") || strings.HasPrefix(trimmed, "export HTB_APP_TOKEN=") {
				if !replaced {
					lines = append(lines, line)
					replaced = true
				}
				continue
			}
			lines = append(lines, existing)
		}
	}

	if !replaced {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, line)
	}

	output := strings.Join(lines, "\n")
	if output == "" || !strings.HasSuffix(output, "\n") {
		output += "\n"
	}

	if err := os.WriteFile(envFile, []byte(output), 0o600); err != nil {
		return Config{}, err
	}

	return loadConfig(baseDir), nil
}

func stripQuotes(value string) string {
	if len(value) >= 2 {
		if value[0] == '"' && value[len(value)-1] == '"' {
			unquoted, err := strconv.Unquote(value)
			if err == nil {
				return unquoted
			}
			return value[1 : len(value)-1]
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
	}

	return value
}

func quoteEnvValue(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(value) + `"`
}

func parseIntDefault(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
